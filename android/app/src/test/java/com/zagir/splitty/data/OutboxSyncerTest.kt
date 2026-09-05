package com.zagir.splitty.data

import com.zagir.splitty.IO_WAIT_MS
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import java.io.File
import java.nio.file.Files
import java.time.Instant
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
import kotlinx.coroutines.withTimeout
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Ядро офлайн-досылки (Task 14): судьба записи по ответу сервера. Цена ошибки
 * здесь — молча потерянный расход пользователя или вечно висящая очередь,
 * поэтому каждая ветка контракта проверяется отдельно:
 * успех → удалить из очереди; 4xx → failed и идём дальше; 5xx/сеть → остаётся
 * pending и синк прерывается; 401 → синк прерывается, запись не трогаем.
 *
 * Сеть — настоящая (MockWebServer через SplittyRepository), «онлайн» —
 * подставной StateFlow вместо Android-зависимого NetworkMonitor.
 */
class OutboxSyncerTest {

    /** Keystore в JVM недоступен — шифрование токена подменяем (см. SessionStoreTest). */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() = Unit
    }

    private val server = MockWebServer()
    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>
    private lateinit var outbox: OutboxStore
    private lateinit var session: SessionStore

    private val online = MutableStateFlow(false)

    @BeforeTest
    fun setUp(): Unit = runBlocking {
        dir = Files.createTempDirectory("syncer-test").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        outbox = OutboxStore(File(dir, "outbox.json"), SplittyJson)
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        session.signIn("jwt", Me(id = 1L, username = "zagir", displayName = "Загир"))
        // Токен читается из DataStore асинхронно; синк без него уходит вхолостую.
        session.state.filterNotNull().first { it.token != null }
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun syncer(): OutboxSyncer {
        server.start()
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
        return OutboxSyncer(outbox, repository, session, online, scope)
    }

    private fun entry(localId: String, description: String = "Такси"): OutboxEntry = OutboxEntry(
        localId = localId,
        roomId = "room1",
        payload = OutboxPayload(
            description = description,
            sum = 100,
            donorId = 1L,
            recipientIds = listOf(1L, 2L),
        ),
        createdAt = Instant.parse("2026-07-18T12:00:00Z"),
    )

    private fun okOperation(id: String, clientOpId: String) = MockResponse().setBody(
        """
        {"id":"$id","description":"Такси","sum":100,
         "donor":{"id":1,"displayName":"Загир"},
         "recipients":[{"user":{"id":2,"displayName":"Оля"},"sum":50}],
         "createdAt":"2026-07-18T12:00:00Z","clientOpId":"$clientOpId"}
        """.trimIndent()
    )

    private fun errorResponse(status: Int, code: String, message: String) = MockResponse()
        .setResponseCode(status)
        .setBody("""{"error":{"code":"$code","message":"$message"}}""")

    /** Ждёт условия на очереди: синк идёт в фоновой корутине scope. */
    private suspend fun awaitQueue(predicate: (List<OutboxEntry>) -> Boolean): List<OutboxEntry> =
        withTimeout(IO_WAIT_MS) { outbox.entries.first(predicate) }

    @Test
    fun `success removes entry and bumps data version`(): Unit = runBlocking {
        outbox.add(entry("a"))
        val syncer = syncer()
        server.enqueue(okOperation("op1", "a"))
        val versionBefore = session.dataVersion.value

        online.value = true
        syncer.syncNow()

        assertTrue(awaitQueue { it.isEmpty() }.isEmpty())
        // Экраны должны перечитать данные — иначе расход «пропадёт» до рестарта.
        assertTrue(session.dataVersion.first { it > versionBefore } > versionBefore)

        // Идемпотентность: clientOpId == localId записи.
        val body = server.takeRequest().body.readUtf8()
        assertTrue(body.contains("\"clientOpId\":\"a\""), body)
    }

    @Test
    fun `4xx marks entry failed and queue continues`(): Unit = runBlocking {
        outbox.add(entry("a", "Плохая"))
        outbox.add(entry("b", "Хорошая"))
        val syncer = syncer()
        server.enqueue(errorResponse(400, "bad_request", "Сумма должна быть больше нуля"))
        server.enqueue(okOperation("op2", "b"))

        online.value = true
        syncer.syncNow()

        // «a» осталась с текстом сервера, «b» ушла — очередь не встаёт из-за
        // одной битой записи.
        val queue = awaitQueue { q -> q.size == 1 && q.single().isFailed }
        assertEquals("a", queue.single().localId)
        assertEquals("Сумма должна быть больше нуля", queue.single().errorMessage)
    }

    @Test
    fun `5xx keeps entry pending and stops the sync`(): Unit = runBlocking {
        outbox.add(entry("a"))
        outbox.add(entry("b"))
        val syncer = syncer()
        server.enqueue(errorResponse(503, "unavailable", "Сервер недоступен"))

        online.value = true
        syncer.syncNow()

        // Обе записи целы и pending: 5xx — не вина записи, повторим на следующем
        // триггере. И следующая запись даже не отправлялась.
        awaitQueue { q -> q.size == 2 }
        Thread.sleep(200)
        assertEquals(listOf("a", "b"), outbox.entries.value.map { it.localId })
        assertTrue(outbox.entries.value.all { it.status == OutboxStatus.PENDING })
        assertEquals(1, server.requestCount)
    }

    @Test
    fun `transport error keeps entry pending`(): Unit = runBlocking {
        outbox.add(entry("a"))
        val syncer = syncer()
        // Обрыв соединения = нет сети: запись обязана уцелеть.
        server.enqueue(MockResponse().setSocketPolicy(okhttp3.mockwebserver.SocketPolicy.DISCONNECT_AT_START))

        online.value = true
        syncer.syncNow()

        Thread.sleep(500)
        assertEquals(1, outbox.entries.value.size)
        assertEquals(OutboxStatus.PENDING, outbox.entries.value.single().status)
    }

    @Test
    fun `401 stops the sync without touching entries`(): Unit = runBlocking {
        outbox.add(entry("a"))
        outbox.add(entry("b"))
        val syncer = syncer()
        server.enqueue(errorResponse(401, "unauthorized", "Сессия истекла"))

        online.value = true
        syncer.syncNow()

        // Разлогин делает AuthInterceptor; синкер лишь прерывается, ничего не
        // помечая failed — после нового входа очередь уйдёт как есть.
        Thread.sleep(500)
        assertEquals(2, outbox.entries.value.size)
        assertTrue(outbox.entries.value.all { it.status == OutboxStatus.PENDING })
        assertEquals(1, server.requestCount)
    }

    @Test
    fun `offline sync does not touch the network`(): Unit = runBlocking {
        outbox.add(entry("a"))
        val syncer = syncer()

        online.value = false
        syncer.syncNow()

        Thread.sleep(300)
        assertEquals(0, server.requestCount)
        assertEquals(1, outbox.entries.value.size)
    }

    @Test
    fun `going online triggers the sync automatically`(): Unit = runBlocking {
        outbox.add(entry("a"))
        syncer() // подписка на isOnline живёт в init
        server.enqueue(okOperation("op1", "a"))

        online.value = true

        assertTrue(awaitQueue { it.isEmpty() }.isEmpty())
    }

    @Test
    fun `failed entries are not retried automatically`(): Unit = runBlocking {
        outbox.add(entry("a"))
        outbox.markFailed("a", "Сумма должна быть больше нуля")
        val syncer = syncer()

        online.value = true
        syncer.syncNow()

        // Повтор — только по явному действию пользователя, иначе бесконечный
        // цикл запросов с гарантированной ошибкой.
        Thread.sleep(300)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun `sync without token does nothing`(): Unit = runBlocking {
        outbox.add(entry("a"))
        val syncer = syncer()
        session.logout()
        session.state.filterNotNull().first { it.token == null }

        online.value = true
        syncer.syncNow()

        Thread.sleep(300)
        assertEquals(0, server.requestCount)
        assertNull(outbox.entries.value.firstOrNull()?.errorMessage)
    }

    @Test
    fun `entry deleted while sync is in flight is not sent`(): Unit = runBlocking {
        outbox.add(entry("a"))
        outbox.add(entry("b"))
        val syncer = syncer()
        // Первый ответ намеренно медленный: успеваем удалить «b» из очереди,
        // пока летит «a». Синк снимает список один раз — без пере-чтения он
        // отправит удалённую запись, и трата появится в комнате вопреки удалению.
        server.enqueue(okOperation("op1", "a").setBodyDelay(600, java.util.concurrent.TimeUnit.MILLISECONDS))
        server.enqueue(okOperation("op2", "b"))

        online.value = true
        syncer.syncNow()

        Thread.sleep(150)
        outbox.remove("b")

        awaitQueue { it.isEmpty() }
        Thread.sleep(400)
        assertEquals(1, server.requestCount)
    }

    @Test
    fun `entry edited while sync is in flight is sent with the new payload`(): Unit = runBlocking {
        outbox.add(entry("a"))
        outbox.add(entry("b", "Старая"))
        val syncer = syncer()
        server.enqueue(okOperation("op1", "a").setBodyDelay(600, java.util.concurrent.TimeUnit.MILLISECONDS))
        server.enqueue(okOperation("op2", "b"))

        online.value = true
        syncer.syncNow()

        Thread.sleep(150)
        outbox.update("b", entry("b", "Исправленная").payload)

        awaitQueue { it.isEmpty() }
        server.takeRequest() // «a»
        val body = server.takeRequest().body.readUtf8()
        // Без пере-чтения записи ушёл бы устаревший payload, а исправленную
        // запись синк тут же удалял — правка не доезжала до сервера вообще.
        assertTrue(body.contains("Исправленная"), body)
    }
}
