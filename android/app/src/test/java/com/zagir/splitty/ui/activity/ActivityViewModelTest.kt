package com.zagir.splitty.ui.activity

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.InviteStatus
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
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
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * VM раздела «Уведомления»: лента и приглашения приезжают одним ответом
 * `GET /notifications`, счётчик непрочитанного публикуется В СЕССИЮ (бейдж на
 * табе живёт вне экрана), отметка прочитанного шлёт `seenThrough` ИЗ ОТВЕТА,
 * а принятие/отклонение приглашения убирает карточку и перезагружает ленту.
 *
 * Сеть — MockWebServer, Main — реальный диспетчер (viewModelScope живой),
 * состояние ждём через потоки с таймаутом.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class ActivityViewModelTest {

    /** Фейк-шифр для SessionStore: реальный Keystore в JVM недоступен. */
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
        dir = Files.createTempDirectory("activity-vm").toFile()
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

    private fun viewModel(): ActivityViewModel {
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
        val syncer = OutboxSyncer(
            OutboxStore(File(dir, "outbox.json"), json),
            repository,
            session,
            MutableStateFlow(true),
            scope,
        )
        return ActivityViewModel(repository, session, syncer)
    }

    /** Ждёт запрос с нужным путём, пропуская посторонние (синк outbox и т.п.). */
    private fun awaitRequest(path: String): RecordedRequest {
        repeat(10) {
            val request = server.takeRequest(5, java.util.concurrent.TimeUnit.SECONDS)
                ?: error("нет запроса $path")
            if (request.path.orEmpty().startsWith(path)) return request
        }
        error("не дождались запроса $path")
    }

    @Test
    fun `feed loads items, invites and publishes unread count to session`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        val vm = viewModel()

        val items = withTimeout(5_000) {
            vm.state.first { it is UiState.Content } as UiState.Content
        }.value

        assertEquals(1, items.size)
        assertEquals("Ужин", items.first().operation.description)

        val invites = withTimeout(5_000) { vm.invites.first { it.isNotEmpty() } }
        assertEquals(1, invites.size)
        assertEquals("65b0", invites.first().roomId)
        assertEquals(InviteStatus.ADDED, invites.first().status)

        // Счётчик уезжает В СЕССИЮ, а не остаётся в VM: бейдж на табе рисуется
        // вне этого экрана, иначе он появлялся бы ровно когда его гасят.
        assertEquals(3, withTimeout(5_000) { session.unreadNotifications.first { it != 0 } })
    }

    @Test
    fun `markSeen sends seenThrough from the response, not local time`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        server.enqueue(MockResponse().setResponseCode(204))
        val vm = viewModel()
        withTimeout(5_000) { vm.state.first { it is UiState.Content } }

        vm.markSeen()

        awaitRequest("/api/v1/notifications")
        val seen = awaitRequest("/api/v1/me/notifications-seen")
        // Именно время ответа: возьми мы локальное «сейчас», события,
        // пришедшие между ответом и тапом, погасли бы непоказанными.
        assertTrue(seen.body.readUtf8().contains(SEEN_THROUGH), "seenThrough из ответа")

        assertEquals(0, withTimeout(5_000) { session.unreadNotifications.first { it == 0 } })
    }

    @Test
    fun `accepting an invite drops the card and reloads the feed`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        server.enqueue(MockResponse().setResponseCode(204)) // accept
        server.enqueue(MockResponse().setBody(FEED_WITHOUT_INVITE_JSON))
        val vm = viewModel()

        val card = withTimeout(5_000) { vm.invites.first { it.isNotEmpty() } }.first()
        vm.acceptInvite(card)

        awaitRequest("/api/v1/notifications")
        val accept = awaitRequest("/api/v1/invites/65b0/accept")
        assertEquals("POST", accept.method)

        assertTrue(withTimeout(5_000) { vm.invites.first { it.isEmpty() } }.isEmpty())
        // Лента перезагружена — комната, в которую вступили, уже видна.
        val items = withTimeout(5_000) {
            vm.state.first { it is UiState.Content && it.value.size == 2 }
        } as UiState.Content
        assertEquals(setOf("65af", "65b0"), items.value.map { it.roomId }.toSet())
    }

    @Test
    fun `declining an invite drops the card`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        server.enqueue(MockResponse().setResponseCode(204)) // decline
        server.enqueue(MockResponse().setBody(FEED_WITHOUT_INVITE_JSON))
        val vm = viewModel()

        val card = withTimeout(5_000) { vm.invites.first { it.isNotEmpty() } }.first()
        vm.declineInvite(card)

        awaitRequest("/api/v1/notifications")
        assertEquals("POST", awaitRequest("/api/v1/invites/65b0/decline").method)
        assertTrue(withTimeout(5_000) { vm.invites.first { it.isEmpty() } }.isEmpty())
    }

    @Test
    fun `leaving from the card calls leave, not decline`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        server.enqueue(MockResponse().setResponseCode(204)) // leave
        server.enqueue(MockResponse().setBody(FEED_WITHOUT_INVITE_JSON))
        val vm = viewModel()

        val card = withTimeout(5_000) { vm.invites.first { it.isNotEmpty() } }.first()
        vm.leaveFromCard(card)

        awaitRequest("/api/v1/notifications")
        // Человека уже ДОБАВИЛИ: отказ — это выход из группы, а не decline
        // (тот работает только для pending и ответил бы 409 not_pending).
        val leave = awaitRequest("/api/v1/rooms/65b0/members/me")
        assertEquals("DELETE", leave.method)
    }

    @Test
    fun `failed invite action surfaces an error and keeps the card`() = runBlocking {
        server.enqueue(MockResponse().setBody(FEED_JSON))
        server.enqueue(
            MockResponse().setResponseCode(409)
                .setBody("""{"error":{"code":"has_operations","message":""}}"""),
        )
        val vm = viewModel()

        val card = withTimeout(5_000) { vm.invites.first { it.isNotEmpty() } }.first()
        vm.leaveFromCard(card)

        val error = withTimeout(5_000) { vm.errorMessage.filterNotNull().first() }
        assertTrue(error != null)
        // Карточка на месте: действие не состоялось, убирать её нечестно.
        assertEquals(1, vm.invites.value.size)
    }

    private companion object {
        const val SEEN_THROUGH = "2026-07-05T12:30:00"

        val FEED_JSON = """
            {
              "invites": [
                {
                  "roomId": "65b0", "roomName": "Байкал",
                  "inviterName": "Аня", "status": "added",
                  "createdAt": "2026-07-05T12:20:00Z"
                }
              ],
              "items": [
                {
                  "roomId": "65af", "roomName": "Ужин", "roomCurrency": "RUB",
                  "operation": {
                    "id": "op1", "description": "Ужин", "sum": 1320,
                    "isDebtRepayment": false,
                    "donor": {"id": 1, "displayName": "Аня"},
                    "recipients": [{"user": {"id": 1, "displayName": "Аня"}, "sum": 1320}],
                    "createdAt": "2026-07-05T12:00:00Z"
                  }
                }
              ],
              "unreadCount": 3,
              "seenThrough": "${SEEN_THROUGH}Z"
            }
        """.trimIndent()

        val FEED_WITHOUT_INVITE_JSON = """
            {
              "invites": [],
              "items": [
                {
                  "roomId": "65af", "roomName": "Ужин", "roomCurrency": "RUB",
                  "operation": {
                    "id": "op1", "description": "Ужин", "sum": 1320,
                    "isDebtRepayment": false,
                    "donor": {"id": 1, "displayName": "Аня"},
                    "recipients": [{"user": {"id": 1, "displayName": "Аня"}, "sum": 1320}],
                    "createdAt": "2026-07-05T12:00:00Z"
                  }
                },
                {
                  "roomId": "65b0", "roomName": "Байкал", "roomCurrency": "RUB",
                  "operation": {
                    "id": "op2", "description": "Билеты", "sum": 5000,
                    "isDebtRepayment": false,
                    "donor": {"id": 2, "displayName": "Боря"},
                    "recipients": [{"user": {"id": 2, "displayName": "Боря"}, "sum": 5000}],
                    "createdAt": "2026-07-05T12:25:00Z"
                  }
                }
              ],
              "unreadCount": 0,
              "seenThrough": "2026-07-05T12:31:00Z"
            }
        """.trimIndent()
    }
}
