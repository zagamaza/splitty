package com.zagir.splitty.core.analytics

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.zagir.splitty.IO_WAIT_MS
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.AuthInterceptor
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Хвост очереди на выходе и очередь при протухшей сессии.
 *
 * Обе проверки написаны по следу из живой базы: у трёх новых аккаунтов
 * единственным событием оказался `logout`. Причина была в том, что отправка
 * случалась ТОЛЬКО по накоплению [Analytics.BATCH_SIZE], а сразу после выхода
 * `OfflineDataCleaner` объявлял очередь чужой и стирал её. Всё, что человек
 * успел сделать за короткий сеанс, не уезжало никогда — и выглядело это как
 * работающая инструментовка.
 */
class AnalyticsBacklogTest {

    /** Фейк-шифр: настоящий Keystore в JVM недоступен. */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    private val server = MockWebServer()
    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>
    private lateinit var session: SessionStore
    private lateinit var api: SplittyApi
    private lateinit var queue: AnalyticsQueue

    @BeforeTest
    fun setUp() {
        server.start()
        dir = Files.createTempDirectory("analytics-backlog").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        val client = OkHttpClient.Builder().addInterceptor(AuthInterceptor(session)).build()
        api = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .client(client)
            .addConverterFactory(
                SplittyJson.asConverterFactory("application/json; charset=utf-8".toMediaType()),
            )
            .build()
            .create(SplittyApi::class.java)
        queue = AnalyticsQueue(File(dir, "analytics.json"), SplittyJson)
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun analytics() =
        Analytics(queue, api, session, scope, DeviceIdSource { "test-device" })

    private fun ok() = MockResponse().setBody("""{"accepted":1,"duplicates":0,"rejected":0}""")

    /** Ждёт, пока очередь на диске догонит ожидаемый размер. */
    private suspend fun awaitQueueSize(expected: Int) {
        withTimeout(IO_WAIT_MS) {
            while (queue.snapshot().size != expected) delay(10)
        }
    }

    /**
     * Меньше порога — и всё равно уезжает, потому что человек выходит.
     *
     * Здесь событий втрое меньше [Analytics.BATCH_SIZE]: по старому коду не
     * ушло бы ни одного, а очередь через мгновение стёрлась бы как чужая.
     */
    @Test
    fun backlogIsSentBeforeLogout() = runBlocking {
        server.enqueue(ok())
        server.enqueue(ok())
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }
        val analytics = analytics()
        analytics.onOwnerChanged(1)

        analytics.track(AnalyticsEvent.ScreenView("groups"))
        analytics.track(AnalyticsEvent.ScreenView("add_expense"))
        analytics.track(AnalyticsEvent.ScreenView("activity"))
        awaitQueueSize(3)

        // Именно ожидающий вариант: на него опирается ProfileViewModel.logout(),
        // и после его возврата чистить очередь уже безопасно.
        analytics.trackTerminalAndAwait(AnalyticsEvent.Logout)

        // Без опроса: раз ожидание вернуло управление, отправка ЗАВЕРШЕНА.
        // Опрос здесь прятал бы ровно ту гонку, ради которой всё затевалось.
        assertTrue(
            queue.snapshot().isEmpty(),
            "ожидание вернулось раньше, чем очередь уехала — чистка снова успеет первой",
        )

        // Первый запрос — накопленное, вторым уходит сам выход.
        val backlog = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }!!
        val names = SplittyJson.parseToJsonElement(backlog.body.readUtf8())
            .jsonObject["events"]!!.jsonArray
            .map { it.jsonObject["name"]!!.jsonPrimitive.content }
        assertEquals(
            listOf("screen_view", "screen_view", "screen_view"),
            names,
            "хвост очереди не уехал перед выходом — в базе останется один logout",
        )
        assertEquals("Bearer token-A", backlog.getHeader("Authorization"))

        val terminal = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }!!
        val terminalNames = SplittyJson.parseToJsonElement(terminal.body.readUtf8())
            .jsonObject["events"]!!.jsonArray
            .map { it.jsonObject["name"]!!.jsonPrimitive.content }
        assertEquals(listOf("logout"), terminalNames)
    }

    /**
     * Протухшая сессия — не выход: очередь обязана дожить до переавторизации.
     *
     * Соседняя очередь неотправленных расходов этот случай переживает
     * намеренно (см. OfflineDataCleaner), а очередь событий стиралась. Одна
     * ситуация, две противоположные политики — это и была дыра.
     */
    @Test
    fun expiredSessionKeepsQueue() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }
        val analytics = analytics()
        analytics.onOwnerChanged(1)
        analytics.track(AnalyticsEvent.ScreenView("groups"))
        awaitQueueSize(1)

        analytics.onOwnerChanged(userId = null, keepQueue = true)

        // Немного времени на случай, если чистка всё-таки запущена.
        delay(100)
        assertEquals(1, queue.snapshot().size, "протухание сессии стёрло очередь событий")

        // Тот же человек вернулся — записи по-прежнему его и остаются.
        analytics.onOwnerChanged(1)
        delay(100)
        assertEquals(1, queue.snapshot().size, "после переавторизации записи пропали")
    }

    /** Явный выход — очередь действительно чужая, и её чистят. */
    @Test
    fun explicitLogoutClearsQueue() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }
        val analytics = analytics()
        analytics.onOwnerChanged(1)
        analytics.track(AnalyticsEvent.ScreenView("groups"))
        awaitQueueSize(1)

        analytics.onOwnerChanged(userId = null)

        withTimeout(IO_WAIT_MS) {
            while (queue.snapshot().isNotEmpty()) delay(10)
        }
        assertTrue(queue.snapshot().isEmpty())
    }

    /**
     * Чистка не может опередить досыл — та самая производственная гонка.
     *
     * `logout()` раньше запускал отправку и, НЕ дожидаясь её, ронял сессию;
     * `OfflineDataCleaner` реагировал на это вызовом keepOwned(null) и стирал
     * очередь до того, как отправка доходила до первой записи. Здесь чистка
     * зовётся ровно тем же вызовом и ровно в тот момент, когда отправка уже
     * началась, но ещё висит на сети: захваченное должно уехать целиком.
     */
    @Test
    fun clearCannotOutrunDrain() = runBlocking {
        // Ответ приходит с задержкой: пока он в пути, дёргаем чистку.
        server.enqueue(ok().setBodyDelay(300, TimeUnit.MILLISECONDS))
        server.enqueue(ok())
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }
        val analytics = analytics()
        analytics.onOwnerChanged(1)
        analytics.track(AnalyticsEvent.ScreenView("groups"))
        analytics.track(AnalyticsEvent.ScreenView("add_expense"))
        awaitQueueSize(2)

        val done = scope.launch { analytics.trackTerminalAndAwait(AnalyticsEvent.Logout) }

        // Отправка уже началась: первый запрос стоит у сервера.
        val backlog = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }!!
        // Ровно то, что делает cleaner сразу за sessionStore.logout().
        analytics.onOwnerChanged(userId = null)

        withTimeout(IO_WAIT_MS) { done.join() }

        val names = SplittyJson.parseToJsonElement(backlog.body.readUtf8())
            .jsonObject["events"]!!.jsonArray
            .map { it.jsonObject["name"]!!.jsonPrimitive.content }
        assertEquals(
            listOf("screen_view", "screen_view"),
            names,
            "чистка опередила досыл — хвост очереди снова потерян",
        )
        val terminal = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }
        assertTrue(terminal != null, "сам выход не уехал")
    }

    /**
     * Ожидание на выходе ограничено: молчащая сеть не держит человека.
     *
     * Здесь сервер не отвечает вовсе, и trackTerminalAndAwait обязан вернуть
     * управление сам — иначе кнопка «Выйти» превращается в зависание.
     */
    @Test
    fun terminalWaitIsBounded() = runBlocking {
        // Чуть дольше таймаута — этого хватает, чтобы доказать границу, и
        // не оставляет висеть запрос дольше, чем живёт сам тест.
        server.enqueue(
            ok().setBodyDelay(Analytics.TERMINAL_WAIT_MS + 500, TimeUnit.MILLISECONDS),
        )
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }
        val analytics = analytics()
        analytics.onOwnerChanged(1)
        analytics.track(AnalyticsEvent.ScreenView("groups"))
        awaitQueueSize(1)

        val started = System.currentTimeMillis()
        analytics.trackTerminalAndAwait(AnalyticsEvent.Logout)
        val waited = System.currentTimeMillis() - started

        assertTrue(
            waited < Analytics.TERMINAL_WAIT_MS * 2,
            "выход ждал $waited мс — ожидание не ограничено",
        )
        // Отправка ещё висит на медленном ответе: даём ей завершиться, иначе
        // MockWebServer не закроется и тест упадёт на разборке, а не на сути.
        delay(1_000)
    }
}
