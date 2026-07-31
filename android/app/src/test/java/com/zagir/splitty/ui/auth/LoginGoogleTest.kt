package com.zagir.splitty.ui.auth

import android.content.Context
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.auth.GoogleSignInException
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
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlinx.coroutines.CompletableDeferred
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
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Вход через Google (Task 18): id-токен из Credential Manager обменивается на
 * сессию через POST /auth/google. Сам Credential Manager в JVM недоступен
 * (нужны Play Services и системный UI), поэтому подменяется [FakeProvider] —
 * проверяется логика ViewModel: успех кладёт сессию, ошибка API кладёт алерт,
 * отмена не показывает НИЧЕГО.
 */
// Robolectric — только ради живого android.content.Context: подменный провайдер
// его не трогает, но тип параметра настоящий (реальному Credential Manager
// нужна активити). SDK 34 — тот же кэш, что у остальных Robolectric-тестов.
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LoginGoogleTest {

    /** Фейк-шифр: реальный Keystore в JVM недоступен. */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    /**
     * Подменный Credential Manager: отдаёт токен, отменяет (null) или падает.
     * [gate] удерживает вызов открытым — так тест «второго тапа» не зависит от
     * того, успел ли первый вход завершиться на соседнем потоке.
     */
    private class FakeProvider(
        private val token: String? = "google-id-token",
        private val failure: Throwable? = null,
        private val gate: CompletableDeferred<Unit>? = null,
    ) : GoogleIdTokenProvider {
        @Volatile
        var calls = 0
        override suspend fun idToken(activityContext: Context): String? {
            calls++
            gate?.await()
            failure?.let { throw it }
            return token
        }
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
        cacheDir = Files.createTempDirectory("google-login-cache").toFile()
        sessionDir = Files.createTempDirectory("google-login-session").toFile()
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

    private fun viewModel(provider: GoogleIdTokenProvider): LoginViewModel {
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
        return LoginViewModel(repository, session, provider)
    }

    /** Ждём завершения входа: флаг ставится синхронно, снимается в finally. */
    private suspend fun LoginViewModel.awaitIdle(): LoginUiState =
        withTimeout(5_000) { state.first { !it.isLoggingIn } }

    @Test
    fun `successful google login stores session`() = runBlocking {
        server.enqueue(MockResponse().setBody(AUTH_JSON))
        val vm = viewModel(FakeProvider(token = "google-id-token"))

        vm.loginWithGoogle(context)
        val state = vm.awaitIdle()

        assertNull(state.errorMessage)
        // Токен уехал именно на /auth/google и именно в поле idToken.
        val request = server.takeRequest()
        assertEquals("/api/v1/auth/google", request.path)
        assertTrue(request.body.readUtf8().contains("\"idToken\":\"google-id-token\""))
        // Сессия сохранена — экран входа сменится табами.
        val stored = withTimeout(5_000) { session.state.first { it?.token != null } }
        assertEquals("jwt-token", stored?.token)
        assertEquals("Загир", stored?.me?.displayName)
    }

    @Test
    fun `api error shows message and keeps user signed out`() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":"invalid_token"}"""))
        val vm = viewModel(FakeProvider(token = "google-id-token"))

        vm.loginWithGoogle(context)
        val state = vm.awaitIdle()

        assertNotNull(state.errorMessage)
        assertEquals("Не удалось войти через Google", state.errorMessage)
        assertNull(withTimeout(5_000) { session.state.first { it != null } }?.token)
    }

    @Test
    fun `cancellation is silent`() = runBlocking {
        val provider = FakeProvider(token = null)
        val vm = viewModel(provider)

        vm.loginWithGoogle(context)
        val state = vm.awaitIdle()

        // Ни алерта, ни зависшего спиннера, ни запроса на сервер.
        assertNull(state.errorMessage)
        assertEquals(1, provider.calls)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun `credential manager failure shows its message`() = runBlocking {
        val vm = viewModel(
            FakeProvider(failure = GoogleSignInException("Добавьте Google-аккаунт в настройках устройства")),
        )

        vm.loginWithGoogle(context)
        val state = vm.awaitIdle()

        assertEquals("Добавьте Google-аккаунт в настройках устройства", state.errorMessage)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun `second tap while signing in is ignored`() = runBlocking {
        server.enqueue(MockResponse().setBody(AUTH_JSON))
        val gate = CompletableDeferred<Unit>()
        val provider = FakeProvider(token = "google-id-token", gate = gate)
        val vm = viewModel(provider)

        vm.loginWithGoogle(context)
        // Флаг isLoggingIn ставится синхронно — второй тап обязан отвалиться
        // до похода в Credential Manager, иначе поверх листа откроется второй.
        vm.loginWithGoogle(context)
        gate.complete(Unit)
        vm.awaitIdle()

        assertEquals(1, provider.calls)
    }

    /**
     * Контекст фейку не нужен — он лишь пробрасывается через сигнатуру.
     * Реальный Credential Manager требует активити (см. [GoogleIdTokenProvider]).
     */
    private val context: Context get() = RuntimeEnvironment.getApplication()

    private companion object {
        val AUTH_JSON = """
            {"token":"jwt-token","user":{"id":1,"displayName":"Загир","username":"zagir"}}
        """.trimIndent()
    }
}
