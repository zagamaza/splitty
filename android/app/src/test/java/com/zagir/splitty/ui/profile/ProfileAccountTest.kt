package com.zagir.splitty.ui.profile

import android.content.Context
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.model.LoginProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ApiException
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

    private val server = MockWebServer()
    private lateinit var cacheDir: File
    private lateinit var sessionDir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore

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
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
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
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(sessionDir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        session.signIn("jwt-token", me)
        withTimeout(5_000) { session.state.first { it?.token != null } }
        ProfileViewModel(
            repository = repository,
            sessionStore = session,
            pushTokenRegistrar = PushTokenRegistrar(repository, session, scope),
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
    }

    @Test
    fun `demo account gets 403 and stays signed in`() = runBlocking {
        respond("DELETE /api/v1/me", MockResponse().setResponseCode(403).setBody(FORBIDDEN_JSON))
        val vm = viewModel()

        vm.deleteAccount()
        withTimeout(5_000) { vm.isDeleting.first { !it } }

        assertEquals("Демонстрационный аккаунт удалить нельзя", vm.errorMessage.value)
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
            "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него",
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
            "Нельзя отвязать единственный способ входа. Сначала привяжите другой",
            identityErrorText(error),
        )
        // 401 здесь — отказ провайдера, а не протухшая сессия.
        assertEquals(
            "Не удалось подтвердить аккаунт. Попробуйте ещё раз",
            identityErrorText(ApiException(401, "unauthorized", "unauthorized")),
        )
        // Прочее падает в общий humanErrorText — свой текст не выдумываем.
        assertEquals(
            "Нет соединения с интернетом. Проверьте сеть и попробуйте ещё раз",
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
        val NOT_FOUND_JSON = """
            {"error":{"code":"not_found","message":"нет ответа для этого пути в тесте"}}
        """.trimIndent()
    }
}
