package com.zagir.splitty.ui.groups

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Состав группы в VM детали: позвать друга, выйти, убрать участника.
 *
 * Проверяется то, чего не видно из UI: какие запросы реально уходят, что
 * `onDone` вызывается ТОЛЬКО после успеха (иначе экран закрывался бы поверх
 * неслучившегося выхода) и что 409 `has_operations` доезжает до алерта
 * сообщением сервера — оно объясняет путь наружу, «конфликт» не объясняет
 * ничего.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34]) // NetworkMonitor тянет ConnectivityManager — нужен Context
class GroupMembersViewModelTest {

    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    private val server = MockWebServer()
    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore

    @BeforeTest
    fun setUp(): Unit = runBlocking {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        dir = Files.createTempDirectory("group-members-vm").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        session.signIn("jwt", Me(id = 1L, username = "zagir", displayName = "Загир"))
        session.state.filterNotNull().first { it.token != null }
    }

    @AfterTest
    fun tearDown() {
        Dispatchers.resetMain()
        server.shutdown()
        scope.cancel()
        dir.deleteRecursively()
    }

    private fun viewModel(): GroupDetailViewModel {
        val json = SplittyJson
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
            .build()
        val repository = SplittyRepository(
            retrofit.create(SplittyApi::class.java),
            retrofit.create(ParseApi::class.java),
            json,
            ApiCache(File(dir, "cache-api"), json),
        )
        val outbox = OutboxStore(File(dir, "outbox.json"), json)
        val online = MutableStateFlow(true)
        val syncer = OutboxSyncer(outbox, repository, session, online, scope)
        return GroupDetailViewModel(
            repository,
            session,
            syncer,
            outbox,
            NetworkMonitor(ApplicationProvider.getApplicationContext()),
        )
    }

    private fun awaitRequest(path: String): RecordedRequest {
        repeat(10) {
            val request = server.takeRequest(5, TimeUnit.SECONDS) ?: error("нет запроса $path")
            if (request.path.orEmpty().startsWith(path)) return request
        }
        error("не дождались запроса $path")
    }

    private suspend fun loadedViewModel(): GroupDetailViewModel {
        server.enqueue(MockResponse().setBody(ROOM_JSON))
        val vm = viewModel()
        vm.start("65af")
        withTimeout(5_000) { vm.room.first { it is UiState.Content } }
        awaitRequest("/api/v1/rooms/65af")
        return vm
    }

    @Test
    fun `inviting friends posts one member request per id`() = runBlocking {
        val vm = loadedViewModel()
        server.enqueue(MockResponse().setBody("""{"status":"added"}"""))
        server.enqueue(MockResponse().setBody(ROOM_JSON)) // refresh после приглашения

        var done = false
        vm.inviteFriends(setOf(7L)) { done = true }

        val request = awaitRequest("/api/v1/rooms/65af/members")
        assertEquals("POST", request.method)
        assertTrue(request.body.readUtf8().contains("\"userId\":7"))
        withTimeout(5_000) {
            while (!done) kotlinx.coroutines.delay(20)
        }
    }

    @Test
    fun `leave calls delete members me and only then closes the screen`() = runBlocking {
        val vm = loadedViewModel()
        server.enqueue(MockResponse().setResponseCode(204))

        var closed = false
        vm.leaveRoom { closed = true }

        val request = awaitRequest("/api/v1/rooms/65af/members/me")
        assertEquals("DELETE", request.method)
        withTimeout(5_000) {
            while (!closed) kotlinx.coroutines.delay(20)
        }
    }

    @Test
    fun `leave with operations keeps the screen and shows the server explanation`() = runBlocking {
        val vm = loadedViewModel()
        server.enqueue(
            MockResponse().setResponseCode(409).setBody(
                """{"error":{"code":"has_operations","message":"$SERVER_MESSAGE"}}""",
            ),
        )

        var closed = false
        vm.leaveRoom { closed = true }

        awaitRequest("/api/v1/rooms/65af/members/me")
        val alert = withTimeout(5_000) { vm.alertMessage.filterNotNull().first() }
        // Именно текст сервера: он объясняет, как выйти (убрать себя из
        // расходов), а свой «конфликт» не объяснил бы ничего.
        assertEquals(SERVER_MESSAGE, (alert as UiText.Raw).value)
        assertFalse(closed, "экран не закрывается, выход не состоялся")
    }

    @Test
    fun `removing a member deletes by id and refreshes the room`() = runBlocking {
        val vm = loadedViewModel()
        server.enqueue(MockResponse().setResponseCode(204))
        server.enqueue(MockResponse().setBody(ROOM_JSON)) // refresh

        vm.removeMember(2L)

        val request = awaitRequest("/api/v1/rooms/65af/members/2")
        assertEquals("DELETE", request.method)
        // Комната перечитывается — список участников на экране обязан сойтись
        // с сервером, а не «потерять» строку локально.
        assertEquals("/api/v1/rooms/65af", awaitRequest("/api/v1/rooms/65af").path)
    }

    private companion object {
        const val SERVER_MESSAGE = "Сначала уберите себя из расходов группы"

        val ROOM_JSON = """
            {
              "id": "65af", "name": "Ужин", "createdAt": "2026-07-05T12:00:00Z",
              "isArchived": false,
              "currency": "RUB",
              "members": [
                {"id": 1, "displayName": "Загир"},
                {"id": 2, "displayName": "Боря"}
              ],
              "totalSpent": 0, "mySpent": 0, "myBalance": 0,
              "debts": [],
              "operations": []
            }
        """.trimIndent()
    }
}
