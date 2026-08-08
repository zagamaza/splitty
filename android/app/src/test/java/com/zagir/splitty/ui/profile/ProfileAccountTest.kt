package com.zagir.splitty.ui.profile

import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
import android.content.Context
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.model.LoginProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.AuthInterceptor
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PushTokenRegistrar
import com.zagir.splitty.ui.components.identityErrorText
import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
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
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Экран «Профиль», Task 21: секция «Способы входа» и удаление аккаунта.
 *
 * Проверяется то, из-за чего человек теряет аккаунт или данные: разлогин
 * СТРОГО после успешного `DELETE /me` (и не при сетевой ошибке), блокировка
 * отвязки последнего способа входа ДО запроса, показ предупреждения об
 * отвязке Telegram и обратная совместимость профиля без `linkedProviders`.
 */
// Robolectric — ради живого android.content.Context в сигнатуре
// GoogleIdTokenProvider (тот же приём, что в LoginGoogleTest).
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ProfileAccountTest {

    /** Фейк-шифр: реальный Keystore в JVM недоступен. */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    /** Подменный Credential Manager: отдаёт токен либо отменяет (null). */
    private class FakeProvider(private val token: String? = "google-id-token") :
        GoogleIdTokenProvider {
        @Volatile
        var calls = 0

        override suspend fun idToken(activityContext: Context): String? {
            calls++
            return token
        }
    }

    /**
     * Подменная отвязка push-токена: считает вызовы вместо похода в Firebase.
     * Настоящий регистратор в JVM-тесте падает на `FirebaseMessaging` и не
     * шлёт ничего — без дублёра «повтор не отвязывал токен» было бы неотличимо
     * от «отвязывал, но запрос всё равно не ушёл», и тест проходил бы на
     * сломанном коде.
     */
    private class SpyRegistrar(
        repository: SplittyRepository,
        session: SessionStore,
        scope: CoroutineScope,
    ) : PushTokenRegistrar(repository, session, scope) {
        @Volatile
        var unregisterCalls = 0

        override fun unregisterCurrent() {
            unregisterCalls++
        }
    }

    private val server = MockWebServer()
    private lateinit var cacheDir: File
    private lateinit var sessionDir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore
    private lateinit var registrar: SpyRegistrar

    /**
     * Все запросы, реально дошедшие до сервера: «МЕТОД /путь» и заголовок
     * Authorization. Проверять надо и отсутствие лишнего запроса, и то, что
     * повтор удаления ушёл ИМЕННО с токеном.
     */
    private val seenRequests = mutableListOf<Pair<String, String?>>()

    /**
     * Ответы по ПУТИ, а не общей очередью: `init` ViewModel уходит в GET /me
     * параллельно действию теста, и очередь отдавала бы профиль запросу
     * привязки — тест падал бы на разборе ответа, а не по существу.
     */
    private val responses = mutableMapOf<String, MutableList<MockResponse>>()

    /** [key] — «МЕТОД /путь»: DELETE /me и GET /me различаются только методом. */
    private fun respond(key: String, response: MockResponse) {
        responses.getOrPut(key) { mutableListOf() }.add(response)
    }

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path.orEmpty()
                val key = "${request.method} $path"
                synchronized(seenRequests) {
                    seenRequests.add(key to request.getHeader("Authorization"))
                }
                val queued = synchronized(responses) { responses[key]?.removeFirstOrNull() }
                // Незаказанное падает 404: «ответ не тому запросу» обязан быть
                // виден как ошибка теста, а не тихо разойтись по assert'ам.
                return queued ?: MockResponse().setResponseCode(404).setBody(NOT_FOUND_JSON)
            }
        }
        server.start()
        cacheDir = Files.createTempDirectory("profile-account-cache").toFile()
        sessionDir = Files.createTempDirectory("profile-account-session").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
    }

    @AfterTest
    fun tearDown() {
        Dispatchers.resetMain()
        server.shutdown()
        scope.cancel()
        cacheDir.deleteRecursively()
        sessionDir.deleteRecursively()
    }

    /**
     * VM с живой сессией: токен уже сохранён, иначе «разлогинило/не
     * разлогинило» нечем отличить.
     *
     * GET /me из `init` намеренно НЕ обслуживается (404): его ответ прилетает
     * параллельно действию теста и перезаписывал бы `linkedProviders` уже
     * после привязки. Сбой обновления профиля init глотает по своему контракту
     * («кэш профиля уже показан»), а проверяем мы не его.
     */
    private fun viewModel(
        provider: GoogleIdTokenProvider = FakeProvider(),
        me: Me = ME,
    ): ProfileViewModel = runBlocking {
        val json = SplittyJson
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(sessionDir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        session.signIn("jwt-token", me)
        withTimeout(5_000) { session.state.first { it?.token != null } }
        // НАСТОЯЩИЙ AuthInterceptor, а не голый Retrofit: именно он вешает
        // Authorization и делает глобальный разлогин на 401 — обе половины
        // сценария удаления аккаунта живут в нём, и подменять его фейком
        // значило бы выбросить из теста ровно то, что ломалось.
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .client(OkHttpClient.Builder().addInterceptor(AuthInterceptor(session)).build())
            .addConverterFactory(
                json.asConverterFactory("application/json; charset=utf-8".toMediaType()),
            )
            .build()
        val repository = SplittyRepository(
            retrofit.create(SplittyApi::class.java),
            retrofit.create(ParseApi::class.java),
            json,
            ApiCache(cacheDir, json),
        )
        registrar = SpyRegistrar(repository, session, scope)
        ProfileViewModel(
            repository = repository,
            sessionStore = session,
            pushTokenRegistrar = registrar,
            googleIdTokenProvider = provider,
            outboxStore = OutboxStore(File(cacheDir, "outbox.json"), json),
        )
    }

    /** Профиль в сессии после обновления способов входа. */
    private suspend fun awaitProviders(expected: List<String>): Me? =
        withTimeout(5_000) { session.state.first { it?.me?.linkedProviders == expected } }?.me

    @Test
    fun `successful delete logs the user out`() = runBlocking {
        respond("DELETE /api/v1/me", MockResponse().setResponseCode(204))
        val vm = viewModel()

        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        assertNull(vm.errorMessage.value)
        // Токен и профиль стёрты — AppRoot покажет экран входа, а
        // OfflineDataCleaner по пропаже токена вычистит кеш и outbox.
        val stored = withTimeout(5_000) { session.state.first { it?.token == null } }
        assertNull(stored?.token)
        assertNull(stored?.me)
        assertFalse(stored?.hasStoredToken ?: true)
        assertTrue(server.requestCount >= 2)
    }

    @Test
    fun `network failure on delete keeps the session alive`() = runBlocking<Unit> {
        respond("DELETE /api/v1/me", MockResponse().setResponseCode(500).setBody(SERVER_ERROR_JSON))
        val vm = viewModel()

        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        // Аккаунт жив: выбросить человека на экран входа значило бы соврать,
        // что удаление прошло.
        assertNotNull(vm.errorMessage.value)
        assertEquals("jwt-token", session.state.value?.token)
        assertNotNull(session.state.value?.me)
        // И флага повтора тут быть не должно: tombstone не поставлен, повторять
        // нечего, а обычное протухание сессии обязано разлогинивать как раньше.
        assertFalse(session.isPurgePending())
    }

    /**
     * Повтор удаления после `purge_incomplete` больше не уничтожает свой
     * собственный токен.
     *
     * Сбой ПОСЛЕ tombstone оставляет аккаунт удалённым, а его PII — в базе.
     * Доделать чистку может только повторный `DELETE /me` тем же токеном
     * (маршрут на `authDeleted`), войти заново нельзя — личности вычищены.
     * Но экран безусловно звал отвязку push-токена ПЕРЕД удалением: на повторе
     * `DELETE /me/devices` (маршрут на `s.auth`) получал на tombstone 401,
     * [AuthInterceptor] звал разлогин, и токен пропадал ДО того, как уходил сам
     * повтор. Тот летел без Authorization, получал 401 — и единственный путь
     * доделать удаление закрывался навсегда (5.1.1(v)/GDPR).
     */
    @Test
    fun `retry after purge incomplete keeps the token and skips device unregister`() = runBlocking {
        respond(
            "DELETE /api/v1/me",
            MockResponse().setResponseCode(500).setBody(PURGE_INCOMPLETE_JSON),
        )
        val vm = viewModel()

        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        assertNotNull(vm.errorMessage.value)
        // Аккаунт был ЖИВ до этого нажатия — отвязка токена устройства законна.
        assertEquals(1, registrar.unregisterCalls)
        val pending = withTimeout(5_000) { session.state.first { it?.purgePending == true } }
        assertEquals("jwt-token", pending?.token)

        // Второе нажатие «Удалить аккаунт» — тот самый повтор. Алерт «повторите»
        // человек перед этим закрывает, как и на живом экране.
        vm.dismissError()
        synchronized(seenRequests) { seenRequests.clear() }
        respond("DELETE /api/v1/me", MockResponse().setResponseCode(204))
        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        val requests = synchronized(seenRequests) { seenRequests.toList() }
        // Главное: повтор НЕ ходил в /me/devices. Именно этот запрос и сносил
        // токен, которым повтор только и мог быть выполнен.
        assertEquals(1, registrar.unregisterCalls, "повтор не смеет отвязывать push-токен")
        assertTrue(
            requests.none { it.first.contains("/me/devices") },
            "повтор ушёл в /me/devices: $requests",
        )
        // И ушёл ИМЕННО с токеном — без него сервер ответил бы 401.
        assertTrue(
            requests.any { it.first == "DELETE /api/v1/me" && it.second == "Bearer jwt-token" },
            "повтор ушёл без Authorization: $requests",
        )
        // Дошёл до 204: сессия закрыта по-настоящему, флаг снят.
        val ended = withTimeout(5_000) { session.state.first { it?.token == null } }
        assertNull(ended?.me)
        assertFalse(ended?.purgePending ?: true)
        assertNull(vm.errorMessage.value)
    }

    /**
     * Пока чистка не доделана, 401 от ЛЮБОГО другого маршрута не смеет стирать
     * токен. Это вторая половина той же дыры: чтобы её открыть, повтор нажимать
     * не обязательно — аккаунт уже tombstone, и 401 отвечает каждый маршрут на
     * `s.auth` (обновление профиля, открытие группы, отвязка push-токена).
     */
    @Test
    fun `unauthorized while purge pending keeps the token`() = runBlocking {
        respond(
            "DELETE /api/v1/me",
            MockResponse().setResponseCode(500).setBody(PURGE_INCOMPLETE_JSON),
        )
        val vm = viewModel()
        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }
        withTimeout(5_000) { session.state.first { it?.purgePending == true } }

        // Ровно то, что делает AuthInterceptor на 401 любого запроса.
        session.notifyUnauthorized()
        delay(300)

        assertEquals("jwt-token", session.state.value?.token, "401 стёр токен повтора")
        assertTrue(session.isPurgePending())
    }

    /**
     * Обратная сторона: БЕЗ флага незавершённой чистки протухшая сессия обязана
     * разлогинивать, как и раньше. Иначе «защита токена» превратилась бы в
     * «приложение с мёртвым токеном никогда не показывает экран входа».
     */
    @Test
    fun `unauthorized without purge pending still ends the session`() = runBlocking {
        val vm = viewModel()
        assertFalse(session.isPurgePending())
        assertNotNull(vm)

        session.notifyUnauthorized()

        val ended = withTimeout(5_000) { session.state.first { it?.token == null } }
        assertNull(ended?.token)
    }

    @Test
    fun `demo account gets 403 and stays signed in`() = runBlocking {
        respond("DELETE /api/v1/me", MockResponse().setResponseCode(403).setBody(FORBIDDEN_JSON))
        val vm = viewModel()

        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        assertEquals(UiText.Raw("Демонстрационный аккаунт удалить нельзя"), vm.errorMessage.value)
        assertEquals("jwt-token", session.state.value?.token)
    }

    @Test
    fun `unlinking the last login method is blocked in the ui`() {
        val single = ME.copy(linkedProviders = listOf("telegram"))

        // Кнопка «Отвязать» гаснет ДО запроса: сервер ответил бы 409
        // last_identity, но узнавать о запрете из алерта после действия — плохо.
        assertFalse(single.canUnlink(LoginProvider.TELEGRAM))
        assertTrue(single.isLinked(LoginProvider.TELEGRAM))
        assertFalse(single.isLinked(LoginProvider.GOOGLE))

        val both = ME.copy(linkedProviders = listOf("telegram", "google"))
        assertTrue(both.canUnlink(LoginProvider.TELEGRAM))
        assertTrue(both.canUnlink(LoginProvider.GOOGLE))
        // Непривязанное не отвязывается, сколько бы способов ни было.
        assertFalse(both.canUnlink(LoginProvider.APPLE))
    }

    @Test
    fun `linking google updates linked providers from the server response`() = runBlocking {
        respond("POST /api/v1/me/link/google", MockResponse().setBody(LINKED_BOTH_JSON))
        val provider = FakeProvider(token = "google-id-token")
        val vm = viewModel(provider)

        vm.linkGoogle(context)
        withTimeout(5_000) { vm.isIdentityBusy.first { !it } }

        assertNull(vm.errorMessage.value)
        assertEquals(1, provider.calls)
        // Список приезжает ОТВЕТОМ сервера, а не досочиняется на клиенте.
        val me = awaitProviders(listOf("telegram", "google"))
        assertEquals(listOf("telegram", "google"), me?.linkedProviders)
        val paths = List(server.requestCount) { server.takeRequest().path }
        assertTrue("/api/v1/me/link/google" in paths, "запрос привязки: $paths")
    }

    @Test
    fun `cancelled google link shows nothing and sends no request`() = runBlocking {
        val provider = FakeProvider(token = null)
        val vm = viewModel(provider)

        vm.linkGoogle(context)
        withTimeout(5_000) { vm.isIdentityBusy.first { !it } }

        assertNull(vm.errorMessage.value)
        assertEquals(1, provider.calls)
        // Главное в этом тесте — что запроса привязки НЕ БЫЛО: без проверки
        // он проходил бы и при отправке пустого id-токена на сервер. Считаем
        // не requestCount (в него попадает GET /me из init VM), а сами пути.
        assertEquals("/api/v1/me", server.takeRequest(5, TimeUnit.SECONDS)?.path)
        assertNull(server.takeRequest(500, TimeUnit.MILLISECONDS))
    }

    @Test
    fun `identity taken conflict is explained without the error code`() = runBlocking {
        respond(
            "POST /api/v1/me/link/google",
            MockResponse().setResponseCode(409).setBody(IDENTITY_TAKEN_JSON),
        )
        val vm = viewModel()

        vm.linkGoogle(context)
        withTimeout(5_000) { vm.isIdentityBusy.first { !it } }

        assertEquals(
            UiText.res(R.string.error_identity_taken),
            vm.errorMessage.value,
        )
        // Привязки не случилось — список прежний.
        assertEquals(listOf("telegram"), session.state.value?.me?.linkedProviders)
    }

    @Test
    fun `telegram unlink warning goes to its own dialog`() = runBlocking {
        respond("DELETE /api/v1/me/link/telegram", MockResponse().setBody(UNLINK_TELEGRAM_JSON))
        val vm = viewModel(me = ME.copy(linkedProviders = listOf("telegram", "google")))

        vm.unlink(LoginProvider.TELEGRAM)
        withTimeout(5_000) { vm.isIdentityBusy.first { !it } }

        // Предупреждение — НЕ ошибка: свой диалог, пустой errorMessage.
        assertNull(vm.errorMessage.value)
        assertEquals(TELEGRAM_WARNING, vm.noticeMessage.value)
        assertEquals(listOf("google"), awaitProviders(listOf("google"))?.linkedProviders)

        vm.dismissNotice()
        assertNull(vm.noticeMessage.value)
    }

    @Test
    fun `last identity conflict from the server is human readable`() {
        val error = ApiException(409, "last_identity", "нельзя отвязать последний способ входа")
        assertEquals(
            UiText.res(R.string.error_last_identity),
            identityErrorText(error),
        )
        // 401 здесь — отказ провайдера, а не протухшая сессия.
        assertEquals(
            UiText.res(R.string.error_provider_rejected),
            identityErrorText(ApiException(401, "unauthorized", "unauthorized")),
        )
        // Прочее падает в общий humanErrorText — свой текст не выдумываем.
        assertEquals(
            UiText.res(R.string.error_no_internet),
            identityErrorText(
                ApiException(null, ApiException.CODE_TRANSPORT, "Нет соединения с сервером"),
            ),
        )
    }

    @Test
    fun `profile cached before linked providers existed still decodes`() {
        // В офлайн-кеше и DataStore лежат профили, записанные ДО появления
        // ключа: строгий разбор ронял бы их на первом холодном старте.
        val old = """{"id":1,"displayName":"Загир","lang":"ru","notificationOn":true}"""
        val me = SplittyJson.decodeFromString(Me.serializer(), old)

        assertEquals(emptyList(), me.linkedProviders)
        // И без единого способа отвязывать нечего — кнопок в секции не будет.
        assertFalse(me.canUnlink(LoginProvider.GOOGLE))
    }

    /** Контекст фейку не нужен — он лишь пробрасывается через сигнатуру. */
    private val context: Context get() = RuntimeEnvironment.getApplication()

    private companion object {
        val ME = Me(
            id = 1,
            username = "zagir",
            displayName = "Загир",
            linkedProviders = listOf("telegram"),
        )
        val LINKED_BOTH_JSON = """
            {"user":{"id":1,"displayName":"Загир","username":"zagir","lang":"ru",
            "notificationOn":true,"linkedProviders":["telegram","google"]}}
        """.trimIndent().replace("\n", "")
        const val TELEGRAM_WARNING = "Telegram отвязан. Если вы снова напишете боту, " +
            "он заведёт отдельный новый профиль без ваших групп."
        val UNLINK_TELEGRAM_JSON = """
            {"user":{"id":1,"displayName":"Загир","username":"zagir","lang":"ru",
            "notificationOn":true,"linkedProviders":["google"]},"warning":"$TELEGRAM_WARNING"}
        """.trimIndent().replace("\n", "")
        val IDENTITY_TAKEN_JSON = """
            {"error":{"code":"identity_taken","message":"Этот аккаунт уже связан"}}
        """.trimIndent()
        val FORBIDDEN_JSON = """
            {"error":{"code":"forbidden","message":"Демонстрационный аккаунт удалить нельзя"}}
        """.trimIndent()
        val SERVER_ERROR_JSON = """
            {"error":{"code":"internal","message":"не удалось удалить аккаунт"}}
        """.trimIndent()
        val PURGE_INCOMPLETE_JSON = """
            {"error":{"code":"purge_incomplete","message":"аккаунт удалён, но очистка данных не завершена: повторите запрос"}}
        """.trimIndent()
        val NOT_FOUND_JSON = """
            {"error":{"code":"not_found","message":"нет ответа для этого пути в тесте"}}
        """.trimIndent()
    }
}
