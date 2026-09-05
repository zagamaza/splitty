package com.zagir.splitty.ui.groups

import com.zagir.splitty.IO_WAIT_MS
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.analytics.testAnalytics
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.AvatarStore
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.ui.main.badgeLabel
import java.io.File
import java.nio.file.Files
import java.time.Instant
import java.util.concurrent.TimeUnit
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
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
 * Счётчик непрочитанного на карточке группы: разбор `unreadCount`, текст бейджа
 * и отметка группы прочитанной при открытии.
 *
 * Бейдж вкладки сообщает, ЧТО что-то случилось, счётчик карточки — ГДЕ; гаснет
 * он открытием ИМЕННО этой группы (раздел «Уведомления» его не трогает, иначе
 * счётчиков почти никто не увидел бы — туда человека и ведёт бейдж).
 */
class GroupUnreadModelTest {

    @Test
    fun `room summary decodes unread count`() {
        val room = SplittyJson.decodeFromString(
            RoomSummary.serializer(),
            """
            {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
             "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,"myBalance":0,
             "unreadCount":3}
            """.trimIndent(),
        )
        assertEquals(3, room.unreadCount)
    }

    /** Ключа нет у прочитанной группы (omitempty) — разбор всего списка не должен падать. */
    @Test
    fun `room summary without unread count defaults to zero`() {
        val room = SplittyJson.decodeFromString(
            RoomSummary.serializer(),
            """
            {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
             "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,"myBalance":0}
            """.trimIndent(),
        )
        assertEquals(0, room.unreadCount)
    }

    /** Комната из кеша прошлой версии приходит без `seenThrough` — отмечать нечем. */
    @Test
    fun `room detail seen through is optional`() {
        val room = SplittyJson.decodeFromString(
            RoomDetail.serializer(),
            """
            {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
             "members":[],"currency":"RUB","totalSpent":0,"mySpent":0,"myBalance":0,
             "debts":[],"operations":[]}
            """.trimIndent(),
        )
        assertNull(room.seenThrough)

        val fresh = SplittyJson.decodeFromString(RoomDetail.serializer(), ROOM_JSON)
        assertEquals(Instant.parse("2026-07-30T12:00:00Z"), fresh.seenThrough)
    }

    /** Правило «99+» одно на бейдж вкладки и на счётчик карточки. */
    @Test
    fun `badge label rule is shared with the tab badge`() {
        assertNull(badgeLabel(0, "99+"))
        assertEquals("7", badgeLabel(7, "99+"))
        assertEquals("99", badgeLabel(99, "99+"))
        assertEquals("99+", badgeLabel(100, "99+"))
    }
}

/** Открытие группы отмечает её прочитанной — временем ИЗ ОТВЕТА сервера. */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34]) // NetworkMonitor тянет ConnectivityManager — нужен Context
class GroupSeenMarkTest {

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
        dir = Files.createTempDirectory("group-unread-vm").toFile()
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
        val syncer = OutboxSyncer(outbox, repository, session, MutableStateFlow(true), scope)
        return GroupDetailViewModel(
            repository,
            session,
            syncer,
            AvatarStore(repository, scope),
            outbox,
            testAnalytics(dir, SplittyJson, session, scope),
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

    @Test
    fun `opening a room marks it seen with the server time from the response`() = runBlocking {
        server.enqueue(MockResponse().setBody(ROOM_JSON))
        server.enqueue(MockResponse().setResponseCode(204))
        val vm = viewModel()
        vm.start("65af")
        withTimeout(IO_WAIT_MS) { vm.room.first { it is UiState.Content } }

        val seen = awaitRequest("/api/v1/rooms/65af/notifications-seen")
        assertEquals("POST", seen.method)
        // Именно серверное время ответа: своё «сейчас» погасило бы и расход,
        // пришедший между ответом и отметкой, — человек его не увидел бы.
        assertTrue(
            seen.body.readUtf8().contains("2026-07-30T12:00:00Z"),
            "отметка обязана нести seenThrough из ответа",
        )
    }
}

private const val ROOM_JSON = """
{
  "id": "65af", "name": "Ужин", "createdAt": "2026-07-05T12:00:00Z",
  "isArchived": false, "currency": "RUB",
  "members": [{"id": 1, "displayName": "Загир"}],
  "totalSpent": 0, "mySpent": 0, "myBalance": 0,
  "debts": [], "operations": [],
  "seenThrough": "2026-07-30T12:00:00Z"
}
"""
