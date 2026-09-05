package com.zagir.splitty.ui.main

import com.zagir.splitty.IO_WAIT_MS
import android.content.Context
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PushEventBus
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
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
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Бейдж непрочитанного, когда пуш пришёл в ОТКРЫТОЕ приложение.
 *
 * `ON_START` в этот момент уже позади и до следующего сворачивания не
 * повторится: без реакции на приход человек смотрит на баннер о новом расходе,
 * а на колоколе висит вчерашнее число. iOS делает это через
 * `.splittyPushReceived`, здесь — через [PushEventBus].
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class MainScaffoldBadgeTest {

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
    private val bus = PushEventBus()

    @BeforeTest
    fun setUp(): Unit = runBlocking {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        dir = Files.createTempDirectory("push-badge").toFile()
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

    private fun viewModel(): MainScaffoldViewModel {
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
        return MainScaffoldViewModel(
            NetworkMonitor(ApplicationProvider.getApplicationContext<Context>()),
            syncer,
            bus,
            repository,
            session,
        )
    }

    @Test
    fun `push arriving in the foreground refreshes the badge`() = runBlocking {
        repeat(5) { server.enqueue(MockResponse().setBody(FEED_JSON)) }
        viewModel()

        val unread = withTimeout(IO_WAIT_MS) {
            // Подписка VM поднимается в своей корутине, а событие прихода
            // ничего не буферизует (в жизни VM существует задолго до пуша) —
            // повторяем, пока не доедет.
            launch {
                while (isActive) {
                    bus.noteReceived()
                    delay(50)
                }
            }.let { pump ->
                session.unreadNotifications.first { it != 0 }.also { pump.cancel() }
            }
        }

        assertEquals(7, unread)
    }

    private companion object {
        /** Ответ `GET /notifications`: важен только `unreadCount`. */
        val FEED_JSON = """
            {
              "invites": [],
              "items": [],
              "unreadCount": 7,
              "seenThrough": "2026-07-05T12:00:00Z"
            }
        """.trimIndent()
    }
}
