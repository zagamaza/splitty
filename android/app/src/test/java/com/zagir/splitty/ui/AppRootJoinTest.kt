package com.zagir.splitty.ui

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.AvatarStore
import com.zagir.splitty.data.OfflineDataCleaner
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.SplittyRepository
import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Отложенное вступление в группу по ссылке-приглашению — исполнение
 * (`AppRootViewModel`).
 *
 * Тут ломается тише всего: диплинк открывают ОДИН раз, ссылку из мессенджера
 * второй раз взять неоткуда, и любая ошибка исполнения выглядит как «нажал на
 * приглашение, попал в пустой список групп». Поэтому проверяется не «запрос
 * ушёл», а судьба самого НАМЕРЕНИЯ: когда оно стирается, когда переживает
 * ошибку и когда вступление вообще не начинается.
 *
 * MockWebServer + настоящие SessionStore/PendingJoinStore поверх временного
 * DataStore — как в ProfileAccountTest: фейки здесь заменили бы собой ровно ту
 * логику (гонку сессии и намерения), из-за которой тесты и пишутся.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class AppRootJoinTest {

    /** Фейк-шифр: реальный Keystore в JVM недоступен. */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    private val server = MockWebServer()
    private lateinit var cacheDir: File
    private lateinit var sessionDir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>
    private lateinit var session: SessionStore
    private lateinit var pendingJoin: PendingJoinStore
    private lateinit var repository: SplittyRepository

    /** Ответы по «МЕТОД /путь» — как в ProfileAccountTest. */
    private val responses = mutableMapOf<String, MutableList<MockResponse>>()

    private fun respond(key: String, response: MockResponse) {
        responses.getOrPut(key) { mutableListOf() }.add(response)
    }

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val key = "${request.method} ${request.path.orEmpty()}"
                val queued = synchronized(responses) { responses[key]?.removeFirstOrNull() }
                return queued ?: MockResponse().setResponseCode(404).setBody(NOT_FOUND_JSON)
            }
        }
        server.start()
        cacheDir = Files.createTempDirectory("approot-join-cache").toFile()
        sessionDir = Files.createTempDirectory("approot-join-session").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(sessionDir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        pendingJoin = PendingJoinStore(dataStore)
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(
                SplittyJson.asConverterFactory("application/json; charset=utf-8".toMediaType()),
            )
            .build()
        repository = SplittyRepository(
            retrofit.create(SplittyApi::class.java),
            retrofit.create(ParseApi::class.java),
            SplittyJson,
            ApiCache(cacheDir, SplittyJson),
        )
    }

    @AfterTest
    fun tearDown() {
        Dispatchers.resetMain()
        server.shutdown()
        scope.cancel()
        cacheDir.deleteRecursively()
        sessionDir.deleteRecursively()
    }

    private fun viewModel() = AppRootViewModel(session, pendingJoin, repository)

    /** Вход: без токена вступление не начинается вовсе. */
    private suspend fun signIn(token: String = "jwt-token", me: Me = ME) {
        session.signIn(token, me)
        withTimeout(5_000) { session.state.first { it?.token == token } }
    }

    /** Чистильщик офлайн-данных поверх тех же хранилищ, что и у VM. */
    private fun cleaner() = OfflineDataCleaner(
        session,
        ApiCache(cacheDir, SplittyJson),
        OutboxStore(File(cacheDir, "outbox.json"), SplittyJson),
        AvatarStore(repository, scope),
        pendingJoin,
        scope,
    )

    @Test
    fun `authenticated pending join consumes the intent and opens the room`() = runBlocking {
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertNull(vm.joinError.value)
        // Намерение стирается ТОЛЬКО после успеха — иначе повторный запуск
        // приложения вступал бы в ту же группу второй раз.
        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
    }

    @Test
    fun `guest keeps the intent until sign in`() = runBlocking {
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        pendingJoin.set(ROOM_ID)
        // Токена нет: запрос ушёл бы анонимно, получил 401 и сжёг приглашение.
        withTimeout(5_000) { session.state.first { it != null } }

        val vm = viewModel()

        // Ни запроса, ни открытой комнаты — намерение просто ждёт.
        assertNull(server.takeRequest(500, TimeUnit.MILLISECONDS))
        assertNull(vm.openRoomId.value)
        assertEquals(ROOM_ID, pendingJoin.pending.first()?.roomId)

        // Вошли — вступление доезжает само, без повторного открытия ссылки.
        signIn()
        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
    }

    @Test
    fun `transient failure keeps the invite for the next attempt`() = runBlocking {
        // 503: сервер прилёг (или у человека метро). Приглашение обязано
        // выжить — второй раз ссылку из мессенджера взять неоткуда.
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setResponseCode(503).setBody(UNAVAILABLE_JSON),
        )
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertNotNull(withTimeout(5_000) { vm.joinError.first { it != null } })
        assertNull(vm.openRoomId.value)
        assertEquals(ROOM_ID, pendingJoin.pending.first()?.roomId)

        vm.dismissJoinError()
        assertNull(vm.joinError.value)
    }

    @Test
    fun `not found clears the invite`() = runBlocking {
        // 404 повторять бессмысленно: группы нет. Оставленное намерение
        // всплывало бы алертом на каждом старте приложения.
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setResponseCode(404).setBody(NOT_FOUND_JSON),
        )
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertEquals(
            "Группа не найдена. Возможно, её удалили или ссылка-приглашение устарела",
            withTimeout(5_000) { vm.joinError.first { it != null } },
        )
        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
    }

    @Test
    fun `forbidden clears the invite`() = runBlocking {
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setResponseCode(403).setBody(FORBIDDEN_JSON),
        )
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertEquals(
            "Нет доступа к этой группе. Попросите участника прислать новое приглашение",
            withTimeout(5_000) { vm.joinError.first { it != null } },
        )
        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
    }

    @Test
    fun `unauthorized keeps the invite and stops retrying on the dead token`() = runBlocking {
        // Один-единственный 401 в очереди: если бы защита unauthorizedToken не
        // работала, второй запрос пришёл бы на незаказанный ответ (404) и тест
        // увидел бы чужой текст ошибки вместо тишины.
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setResponseCode(401).setBody(UNAUTHORIZED_JSON),
        )
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
        // Намерение на месте: после переавторизации вступление доедет само.
        assertEquals(ROOM_ID, pendingJoin.pending.first()?.roomId)
        // Алерта нет: человека и так выбрасывает на экран входа, «Требуется
        // вход» поверх него сказало бы очевидное.
        assertNull(vm.joinError.value)
        // Повторов на мёртвом токене нет.
        assertNull(server.takeRequest(500, TimeUnit.MILLISECONDS))
    }

    @Test
    fun `expired session keeps the invite for the next sign in`() = runBlocking {
        // Ради этого сценария намерение и не забирается take()-ом до запроса:
        // токен протух ровно на вступлении, человек входит заново — и
        // приглашение обязано доехать без повторного открытия ссылки.
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setResponseCode(401).setBody(UNAUTHORIZED_JSON),
        )
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        signIn(token = "dead-token")
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()

        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
        assertEquals(ROOM_ID, pendingJoin.pending.first()?.roomId)

        // Переавторизация: НОВЫЙ токен снимает блокировку unauthorizedToken.
        signIn(token = "fresh-token")

        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
    }

    @Test
    fun `room opened resets the navigation intent`() = runBlocking {
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()
        withTimeout(5_000) { vm.openRoomId.first { it != null } }

        // Гасим сразу: иначе комната откроется второй раз при пересоздании корня.
        vm.onRoomOpened()
        assertNull(vm.openRoomId.value)
    }

    @Test
    fun `session emission during the request does not send a second join`() = runBlocking {
        // Ответ с задержкой: пока join в полёте, сессия эмитит ЕЩЁ раз (новый
        // токен). Пара «токен + намерение» при этом меняется, фильтр
        // distinctUntilChanged её пропускает — и без защиты (isJoining в полёте,
        // joinedRoomId после успеха) в ту же группу ушёл бы второй запрос.
        respond(
            "POST /api/v1/rooms/$ROOM_ID/join",
            MockResponse().setBody(ROOM_JSON).setBodyDelay(700, TimeUnit.MILLISECONDS),
        )
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()
        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
        signIn(token = "second-token")

        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertNull(server.takeRequest(700, TimeUnit.MILLISECONDS))
    }

    @Test
    fun `reopening the same link opens the room without a second join`() = runBlocking {
        // Повторный тап по той же ссылке. Запрос не нужен — в группе уже
        // состоим, — но раньше не происходило ВООБЩЕ ничего: ни навигации, ни
        // очистки, и забытое намерение проваливало в эту группу на каждом
        // следующем холодном старте.
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        signIn()
        pendingJoin.set(ROOM_ID)

        val vm = viewModel()
        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertEquals("/api/v1/rooms/$ROOM_ID/join", server.takeRequest(5, TimeUnit.SECONDS)?.path)
        vm.onRoomOpened()
        withTimeout(5_000) { pendingJoin.pending.first { it == null } }

        pendingJoin.set(ROOM_ID)

        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
        // Второго запроса на вступление нет.
        assertNull(server.takeRequest(500, TimeUnit.MILLISECONDS))
    }

    @Test
    fun `invite of a previous account is dropped when someone else signs in`() = runBlocking {
        // Утечка между аккаунтами. Сессия A протухла (её чистка приглашение
        // намеренно СОХРАНЯЕТ), процесс умер — вход уводит в системный лист, —
        // а на устройстве вошёл уже B. Единственное, что отличает «вернулся
        // тот же» от «пришёл другой» после смерти процесса, — записанный на
        // диск владелец намерения: без него B молча вступал бы в приватную
        // группу A, и его туда ещё и уносило бы навигацией.
        pendingJoin.set(ROOM_ID, ownerId = ME.id)

        // Новый процесс: чистильщик и корень создаются с нуля, память пуста.
        assertNotNull(cleaner())
        val vm = viewModel()
        signIn(token = "other-token", me = OTHER)

        // Ни одного запроса на вступление.
        assertNull(server.takeRequest(1, TimeUnit.SECONDS))
        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
        assertNull(vm.openRoomId.value)
    }

    @Test
    fun `guest invite is adopted by the account that signs in`() = runBlocking {
        // Обратная сторона проверки владельца: ссылку открыл гость (владельца
        // нет), и вступление обязано доехать сразу после входа.
        respond("POST /api/v1/rooms/$ROOM_ID/join", MockResponse().setBody(ROOM_JSON))
        pendingJoin.set(ROOM_ID)
        assertNotNull(cleaner())

        val vm = viewModel()
        signIn()

        assertEquals(ROOM_ID, withTimeout(5_000) { vm.openRoomId.first { it != null } })
    }

    @Test
    fun `cleaner keeps the invite on expiry and drops it on explicit logout`() = runBlocking {
        // Сквозной сценарий A3/B5. Раньше 401 прямо во время вступления
        // гарантированно убивал приглашение: AppRoot возвращал намерение, а
        // OfflineDataCleaner тут же стирал его вместе с остальными данными.
        assertNotNull(cleaner())
        signIn()
        pendingJoin.set(ROOM_ID)
        withTimeout(5_000) { session.state.first { it?.token != null } }

        // Протухшая сессия (401 → AuthInterceptor.notifyUnauthorized).
        session.notifyUnauthorized()
        withTimeout(5_000) { session.state.first { it?.hasStoredToken == false } }
        // Даём чистке отработать: она асинхронная, и «не стёрла» без паузы
        // означало бы лишь «не успела».
        delay(500)
        assertEquals(ROOM_ID, pendingJoin.pending.first()?.roomId)

        // Явный выход — данные предыдущего владельца устройства стираются все.
        signIn()
        withTimeout(5_000) { session.state.first { it?.token != null } }
        session.logout()

        assertNull(withTimeout(5_000) { pendingJoin.pending.first { it == null } })
    }

    // MARK: - joinLinkErrorText / isTerminalJoinError

    @Test
    fun `join link errors are explained in human words`() {
        // Человек по приглашению не нажимал «Присоединиться» и не вводил код —
        // сырое «Не найдено» от сервера ему ничего не объясняет.
        assertEquals(
            "Группа не найдена. Возможно, её удалили или ссылка-приглашение устарела",
            joinLinkErrorText(ApiException(404, "not_found", "Не найдено")),
        )
        // Код без статуса (и наоборот) распознаётся так же.
        assertEquals(
            "Группа не найдена. Возможно, её удалили или ссылка-приглашение устарела",
            joinLinkErrorText(ApiException(null, "not_found", "Не найдено")),
        )
        assertEquals(
            "Нет доступа к этой группе. Попросите участника прислать новое приглашение",
            joinLinkErrorText(ApiException(403, "forbidden", "Нет доступа")),
        )
        // Остальное — серверный message как есть: он уже человеческий.
        assertEquals(
            "Нет соединения с сервером",
            joinLinkErrorText(
                ApiException(null, ApiException.CODE_TRANSPORT, "Нет соединения с сервером"),
            ),
        )
    }

    @Test
    fun `only 404 and 403 are terminal`() {
        assertTrue(isTerminalJoinError(ApiException(404, "not_found", "нет")))
        assertTrue(isTerminalJoinError(ApiException(403, "forbidden", "нельзя")))
        // Сеть и 5xx — временные: приглашение обязано дожить до второй попытки.
        assertTrue(
            !isTerminalJoinError(
                ApiException(null, ApiException.CODE_TRANSPORT, "Нет соединения с сервером"),
            ),
        )
        assertTrue(!isTerminalJoinError(ApiException(503, "unavailable", "сервер занят")))
        assertTrue(!isTerminalJoinError(ApiException(401, "unauthorized", "нужен вход")))
    }

    private companion object {
        const val ROOM_ID = "507f1f77bcf86cd799439011"
        val ME = Me(id = 1, username = "zagir", displayName = "Загир")

        /** Другой человек на том же устройстве. */
        val OTHER = Me(id = 2, username = "other", displayName = "Другой")

        val ROOM_JSON = """
            {"id":"$ROOM_ID","name":"Стамбул","createdAt":"2026-01-01T00:00:00Z",
            "currency":"RUB","totalSpent":0,"mySpent":0,"myBalance":0}
        """.trimIndent().replace("\n", "")
        val NOT_FOUND_JSON = """{"error":{"code":"not_found","message":"Не найдено"}}"""
        val FORBIDDEN_JSON = """{"error":{"code":"forbidden","message":"Нет доступа"}}"""
        val UNAUTHORIZED_JSON = """{"error":{"code":"unauthorized","message":"Требуется вход"}}"""
        val UNAVAILABLE_JSON = """{"error":{"code":"unavailable","message":"Сервер занят"}}"""
    }
}
