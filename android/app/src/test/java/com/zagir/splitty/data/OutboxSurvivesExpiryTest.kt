package com.zagir.splitty.data

import com.zagir.splitty.IO_WAIT_MS
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.analytics.AnalyticsQueue
import com.zagir.splitty.core.analytics.testAnalytics
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
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
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.MediaType.Companion.toMediaType
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Очередь неотправленных расходов и протухание сессии.
 *
 * Человек добавляет расходы офлайн, 90-дневный токен истекает, он входит
 * заново — очередь обязана дожить до синка. Раньше её стирало вместе со всем
 * остальным, и расходы пропадали молча, так и не доехав до сервера.
 */
class OutboxSurvivesExpiryTest {

    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore
    private lateinit var outbox: OutboxStore
    private lateinit var retrofit: retrofit2.Retrofit

    @BeforeTest
    fun setUp() {
        dir = Files.createTempDirectory("outbox-expiry").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        outbox = OutboxStore(File(dir, "outbox.json"), SplittyJson)
        retrofit = retrofit2.Retrofit.Builder()
            .baseUrl("http://localhost:1/")
            .addConverterFactory(
                SplittyJson.asConverterFactory(
                    "application/json; charset=utf-8".toMediaType()
                )
            )
            .build()
        OfflineDataCleaner(
            sessionStore = session,
            cache = ApiCache(File(dir, "cache"), SplittyJson),
            outbox = outbox,
            avatars = AvatarStore(
                SplittyRepository(
                    retrofit.create(com.zagir.splitty.core.network.SplittyApi::class.java),
                    retrofit.create(com.zagir.splitty.core.network.ParseApi::class.java),
                    SplittyJson,
                    ApiCache(File(dir, "cache-avatars"), SplittyJson),
                ),
                scope,
            ),
            analyticsQueue = AnalyticsQueue(File(dir, "analytics.json"), SplittyJson),
            analytics = testAnalytics(dir, SplittyJson, session, scope),
            pendingJoin = PendingJoinStore(dataStore),
            scope = scope,
        )
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        dir.deleteRecursively()
    }

    private suspend fun signInWithQueuedExpense() {
        session.signIn("jwt", Me(id = 1L, username = "zagir", displayName = "Загир"))
        session.state.filterNotNull().first { it.token != null }
        outbox.add(
            OutboxEntry(
                localId = "local-1",
                roomId = "room1",
                payload = OutboxPayload(
                    description = "Такси",
                    sum = 100,
                    donorId = 1L,
                    recipientIds = listOf(1L, 2L),
                ),
                createdAt = java.time.Instant.parse("2026-08-12T10:00:00Z"),
            )
        )
        assertEquals(1, outbox.entries.first().size)
    }

    @Test
    fun `expired session keeps the unsent queue`() = runBlocking {
        signInWithQueuedExpense()

        session.notifyUnauthorized()

        // Ждём, пока чистка отработает переход «токен был → токена нет».
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == null } }
        kotlinx.coroutines.delay(500)

        assertEquals(
            1,
            outbox.entries.first().size,
            "очередь стёрлась при протухании токена — расход пропал, не доехав до сервера",
        )
    }

    @Test
    fun `explicit logout clears the queue`() = runBlocking {
        signInWithQueuedExpense()

        session.logout()

        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == null } }
        withTimeout(IO_WAIT_MS) { outbox.entries.first { it.isEmpty() } }

        assertTrue(
            outbox.entries.first().isEmpty(),
            "при явном выходе очередь принадлежит уходящему и обязана быть стёрта",
        )
    }
}
