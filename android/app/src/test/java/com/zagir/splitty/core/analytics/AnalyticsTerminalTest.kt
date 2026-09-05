package com.zagir.splitty.core.analytics

import com.zagir.splitty.IO_WAIT_MS
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.AuthInterceptor
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import java.io.File
import java.lang.reflect.Proxy
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Терминальное событие — то, после которого сессии уже нет: нажатие «Выйти».
 *
 * Оно уходит МИМО очереди: очередь чистится как чужая через миллисекунды после
 * выхода, и положенная туда запись не уехала бы никогда. Раз ретраить негде,
 * единственная попытка обязана быть правильной — и по адресату, и по времени
 * жизни. Проверяется здесь худшее, а не удобное: заголовок снимается ДО чистки,
 * а уходит запрос уже после неё, иногда после входа СЛЕДУЮЩЕГО человека.
 *
 * Настоящие SessionStore, AuthInterceptor и MockWebServer: подмени любого из
 * трёх — и проверять будет нечего, гонка живёт ровно между ними.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class AnalyticsTerminalTest {

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

    /**
     * Ворота перед AuthInterceptor: держат запрос в цепочке, пока тест не
     * доведёт сессию до нужного состояния. Без них порядок «сняли заголовок →
     * вошёл другой → запрос прошёл перехватчик» не воспроизвести.
     */
    private val gate = CountDownLatch(1)

    @BeforeTest
    fun setUp() {
        server.enqueue(MockResponse().setBody("""{"accepted":1,"duplicates":0,"rejected":0}"""))
        server.start()
        dir = Files.createTempDirectory("analytics-terminal").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        val client = OkHttpClient.Builder()
            .addInterceptor(
                Interceptor { chain ->
                    if (chain.request().url.encodedPath.endsWith("/api/v1/events")) {
                        gate.await(5, TimeUnit.SECONDS)
                    }
                    chain.proceed(chain.request())
                },
            )
            .addInterceptor(AuthInterceptor(session))
            .build()
        api = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .client(client)
            .addConverterFactory(
                SplittyJson.asConverterFactory("application/json; charset=utf-8".toMediaType()),
            )
            .build()
            .create(SplittyApi::class.java)
    }

    @AfterTest
    fun tearDown() {
        gate.countDown()
        scope.cancel()
        server.shutdown()
        dir.deleteRecursively()
    }

    /**
     * Ждёт, пока сессия ДОЙДЁТ до состояния: `state` наполняется collect'ом
     * DataStore, а не возвратом из [SessionStore.signIn]. Без ожидания тест
     * проверял бы гонку самого себя, а не поведение отправки.
     */
    private suspend fun awaitToken(token: String?) {
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == token } }
    }

    private fun analytics(withApi: SplittyApi = api, into: CoroutineScope = scope) =
        Analytics(AnalyticsQueue(File(dir, "analytics.json"), SplittyJson), withApi, session, into)

    /**
     * Выход A, вошедший до отправки B — событие всё равно уходит под токеном A.
     *
     * Сервер берёт номер человека ИЗ ТОКЕНА и тело не читает, поэтому подмена
     * заголовка записала бы выход A на B: и неверная аналитика, и чужой след в
     * данных другого человека.
     */
    @Test
    fun terminalEventKeepsSenderToken() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        awaitToken("token-A")
        val analytics = analytics()
        analytics.onOwnerChanged(1)

        analytics.trackTerminal(AnalyticsEvent.Logout)

        // Пока запрос стоит в воротах, сессия успевает смениться целиком.
        session.logout()
        session.signIn("token-B", Me(id = 2, displayName = "Б"))
        awaitToken("token-B")
        gate.countDown()

        val request = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }
        assertEquals("Bearer token-A", request?.getHeader("Authorization"))
    }

    /** Без нового входа заголовок тоже переживает чистку сессии. */
    @Test
    fun terminalEventSurvivesLogoutWithoutNewLogin() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        awaitToken("token-A")
        val analytics = analytics()
        analytics.onOwnerChanged(1)

        analytics.trackTerminal(AnalyticsEvent.Logout)
        session.logout()
        awaitToken(null)
        gate.countDown()

        val request = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }
        assertEquals("Bearer token-A", request?.getHeader("Authorization"))
    }

/**
     * В теле уезжают ТОЛЬКО поля конверта из контракта.
     *
     * Конверт не сверяет ни один контракт-тест: они проверяют имена событий и
     * параметры, а состав тела — нет. Именно так `AnalyticsRecord`, уходивший
     * на провод целиком, какое-то время слал серверу внутренний `ownerUserId`:
     * сервер его игнорирует, в контракте его нет, и не заметил никто.
     */
    @Test
    fun wireBodyCarriesOnlyContractFields() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        awaitToken("token-A")
        val analytics = analytics()
        analytics.onOwnerChanged(1)

        analytics.trackTerminal(AnalyticsEvent.Logout)
        gate.countDown()

        val request = withTimeout(IO_WAIT_MS) { server.takeRequest(5, TimeUnit.SECONDS) }
        val body = SplittyJson.parseToJsonElement(request!!.body.readUtf8()).jsonObject
        val event = body["events"]!!.jsonArray.first().jsonObject

        // Точное равенство, а не «лишнего нет»: у EventBody.params нет значения
        // по умолчанию, поэтому kotlinx пишет его и пустым. Набор тот же, что
        // проверяет iOS — расхождение здесь означало бы реальный дрейф провода,
        // а не особенность платформы.
        assertEquals(
            setOf("id", "name", "at", "session", "platform", "appVersion", "locale", "params"),
            event.keys,
            "состав тела разошёлся с контрактом (docs/analytics-events.md)",
        )
        assertFalse(
            "ownerUserId" in event.keys,
            "владелец записи — внутреннее поле очереди, серверу его слать нечего",
        )
    }

    /**
     * Отмена не проглатывается.
     *
     * Проглоченная CancellationException — это корутина, завершившаяся как
     * успешная там, где на самом деле её остановили. В остальном коде отмена
     * пробрасывается (см. flush), и терминальная отправка не имеет права
     * отличаться.
     */
    @Test
    fun cancellationIsNotSwallowed() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        awaitToken("token-A")
        // Ворота внутри вызова: без них отправка успевает упасть и исчезнуть
        // из детей scope раньше, чем тест до неё дотянется.
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val throwing = Proxy.newProxyInstance(
            SplittyApi::class.java.classLoader,
            arrayOf(SplittyApi::class.java),
        ) { _, _, _ ->
            entered.countDown()
            release.await(5, TimeUnit.SECONDS)
            throw CancellationException("отмена изнутри вызова")
        } as SplittyApi

        // Отдельный scope: в общем живут DataStore и SessionStore, и «первый
        // ребёнок» оказался бы их, а не отправкой.
        val sendScope = CoroutineScope(Job() + Dispatchers.IO)
        try {
            analytics(throwing, sendScope).trackTerminal(AnalyticsEvent.Logout)

            assertTrue(entered.await(5, TimeUnit.SECONDS), "отправка не дошла до вызова")
            val child = sendScope.coroutineContext.job.children.first()
            release.countDown()
            withTimeout(IO_WAIT_MS) { child.join() }
            assertTrue(child.isCancelled, "отмена проглочена: корутина завершилась как успешная")
            assertFalse(
                sendScope.coroutineContext.job.isCancelled,
                "отмена одной отправки не смеет валить общий scope",
            )
        } finally {
            sendScope.cancel()
        }
    }
}
