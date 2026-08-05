package com.zagir.splitty.ui.auth

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.SplittyRepository
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
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
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/** Валидация формы «email + пароль» (порт iOS EmailLoginForm). */
class EmailLoginFormTest {

    @Test
    fun `email is trimmed and lowercased`() {
        assertEquals("olga@example.com", EmailLoginForm.normalizeEmail("  Olga@Example.COM \n"))
    }

    @Test
    fun `email shape is checked`() {
        assertTrue(EmailLoginForm.isValidEmail("olga@example.com"))
        assertTrue(EmailLoginForm.isValidEmail(" Olga@Example.com "))
        assertFalse(EmailLoginForm.isValidEmail("olga"))
        assertFalse(EmailLoginForm.isValidEmail("@example.com"))
        assertFalse(EmailLoginForm.isValidEmail("olga@example"))
        assertFalse(EmailLoginForm.isValidEmail("olga@@example.com"))
        assertFalse(EmailLoginForm.isValidEmail("olga@.com"))
        assertFalse(EmailLoginForm.isValidEmail("olga@example."))
    }

    @Test
    fun `password length is bounded on both ends`() {
        assertFalse(EmailLoginForm.isValidPassword("short12"))
        assertTrue(EmailLoginForm.isValidPassword("secret123"))
        // bcrypt молча отбрасывает всё после 72 байт — такие пароли совпадали
        // бы по общему префиксу
        assertFalse(EmailLoginForm.isValidPassword("a".repeat(73)))
        assertFalse(EmailLoginForm.isValidPassword("я".repeat(40)))
    }

    @Test
    fun `login does not require password length but registration does`() {
        val login = LoginUiState(email = "olga@example.com", password = "x")
        assertTrue(login.isEmailFormValid)

        val registration = login.copy(isRegistering = true, registerName = "Оля")
        assertFalse(registration.isEmailFormValid)
        assertTrue(registration.copy(password = "secret123").isEmailFormValid)
        // Имя обязательно: на пустое сервер отвечает 400 validation
        assertFalse(registration.copy(password = "secret123", registerName = " ").isEmailFormValid)
    }
}

/**
 * Вход и регистрация по email через ViewModel: тело запроса, сохранение сессии
 * и текст ошибки. Сеть — MockWebServer, как в [LoginGoogleTest].
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class EmailPasswordLoginTest {

    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    /** Credential Manager сюда не приходит — вход по паролю его не трогает. */
    private object UnusedProvider : GoogleIdTokenProvider {
        override suspend fun idToken(activityContext: android.content.Context): String? =
            error("не должен вызываться")
    }

    private val server = MockWebServer()
    private lateinit var cacheDir: File
    private lateinit var sessionDir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        cacheDir = Files.createTempDirectory("pwd-login-cache").toFile()
        sessionDir = Files.createTempDirectory("pwd-login-session").toFile()
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

    private fun viewModel(): LoginViewModel {
        val json = SplittyJson
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
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
        return LoginViewModel(repository, session, UnusedProvider)
    }

    private suspend fun LoginViewModel.awaitIdle(): LoginUiState =
        withTimeout(5_000) { state.first { !it.isLoggingIn } }

    @Test
    fun `login sends normalized email and stores session`() = runBlocking {
        server.enqueue(MockResponse().setBody(AUTH_JSON))
        val vm = viewModel()
        vm.onEmailChange("  Olga@Example.COM ")
        vm.onPasswordChange("secret123")

        vm.submitEmailForm()
        val state = vm.awaitIdle()

        assertNull(state.errorMessage)
        val request = server.takeRequest()
        assertEquals("/api/v1/auth/login", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"email\":\"olga@example.com\""))
        assertTrue(body.contains("\"password\":\"secret123\""))
        // Пароль не остаётся в состоянии экрана после успешного входа
        assertEquals("", state.password)
        val stored = withTimeout(5_000) { session.state.first { it?.token != null } }
        assertEquals("jwt-token", stored?.token)
        assertEquals("olga@example.com", stored?.me?.loginEmail)
    }

    @Test
    fun `registration sends trimmed name`() = runBlocking {
        server.enqueue(MockResponse().setBody(AUTH_JSON))
        val vm = viewModel()
        vm.toggleRegistering()
        vm.onEmailChange("olga@example.com")
        vm.onPasswordChange("secret123")
        vm.onRegisterNameChange("  Оля  ")

        vm.submitEmailForm()
        vm.awaitIdle()

        val request = server.takeRequest()
        assertEquals("/api/v1/auth/register", request.path)
        assertTrue(request.body.readUtf8().contains("\"displayName\":\"Оля\""))
    }

    @Test
    fun `server message is shown as is`() = runBlocking {
        server.enqueue(
            MockResponse().setResponseCode(409)
                .setBody("""{"error":{"code":"email_taken","message":"этот email уже зарегистрирован"}}"""),
        )
        val vm = viewModel()
        vm.toggleRegistering()
        vm.onEmailChange("taken@example.com")
        vm.onPasswordChange("secret123")
        vm.onRegisterNameChange("Оля")

        vm.submitEmailForm()
        val state = vm.awaitIdle()

        assertEquals("этот email уже зарегистрирован", state.errorMessage)
        assertNull(withTimeout(5_000) { session.state.first { it != null } }?.token)
    }

    @Test
    fun `invalid form does not hit the network`() = runBlocking {
        val vm = viewModel()
        vm.onEmailChange("olga")
        vm.onPasswordChange("secret123")

        vm.submitEmailForm()

        assertEquals(0, server.requestCount)
    }

    private companion object {
        val AUTH_JSON = """
            {"token":"jwt-token","user":{"id":1000000000003,"displayName":"Оля",
             "linkedProviders":["password"],"loginEmail":"olga@example.com"}}
        """.trimIndent()
    }
}
