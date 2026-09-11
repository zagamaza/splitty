package com.zagir.splitty.ui

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.ViewModelStore
import com.zagir.splitty.IO_WAIT_MS
import com.zagir.splitty.core.analytics.testAnalytics
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PushEventBus
import com.zagir.splitty.ui.onboarding.IntroSeenSource
import java.io.File
import java.nio.file.Files
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Приветствие в корне: кому оно показывается и кому — уже нет.
 *
 * Отдельно от `WelcomeGateTest`: там проверяется чистая функция, здесь —
 * проводка вокруг неё, то есть миграция отметки и три источника состояния.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class AppRootIntroTest {

    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    /** Отметка в памяти: настоящая живёт в SharedPreferences, недоступном JVM. */
    private class FakeIntroSeen(private var seen: Boolean = false) : IntroSeenSource {
        override fun hasSeen() = seen
        override fun markSeen() { seen = true }
    }

    private val server = MockWebServer()
    private val vmStore = ViewModelStore()
    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>
    private lateinit var session: SessionStore
    private lateinit var pendingJoin: PendingJoinStore
    private lateinit var repository: SplittyRepository

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        dir = Files.createTempDirectory("approot-intro").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
        pendingJoin = PendingJoinStore(dataStore)
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(
                SplittyJson.asConverterFactory("application/json; charset=utf-8".toMediaType()),
            )
            .build()
        repository = SplittyRepository(
            retrofit.create(SplittyApi::class.java),
            retrofit.create(ParseApi::class.java),
            SplittyJson,
            ApiCache(dir, SplittyJson),
        )
    }

    @AfterTest
    fun tearDown() {
        vmStore.clear()
        Dispatchers.resetMain()
        server.shutdown()
        scope.cancel()
        dir.deleteRecursively()
    }

    private fun viewModel(introSeen: IntroSeenSource): AppRootViewModel {
        val factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>): T =
                AppRootViewModel(
                    session,
                    pendingJoin,
                    repository,
                    PushEventBus(),
                    testAnalytics(dir, SplittyJson, session, scope),
                    introSeen,
                ) as T
        }
        return ViewModelProvider(vmStore, factory)[AppRootViewModel::class.java]
    }

    private suspend fun awaitIntro(vm: AppRootViewModel): Boolean =
        withTimeout(IO_WAIT_MS) { vm.showIntro.first { it != null } }!!

    /** Чистая установка: человек ещё ничего про продукт не знает. */
    @Test
    fun `fresh install sees the intro`() = runBlocking {
        assertTrue(awaitIntro(viewModel(FakeIntroSeen())))
    }

    /** Уже видел — второй раз не показываем. */
    @Test
    fun `seen install goes straight to login`() = runBlocking {
        assertFalse(awaitIntro(viewModel(FakeIntroSeen(seen = true))))
    }

    /** Пришёл по ссылке: ведём в тусу, а не в рассказ о продукте. */
    @Test
    fun `pending deeplink suppresses the intro`() = runBlocking {
        pendingJoin.set("room-1")
        assertFalse(awaitIntro(viewModel(FakeIntroSeen())))
    }

    /**
     * Ссылку тапнули только что: диск ещё не ответил, а решать уже надо.
     *
     * Ровно этот случай и есть холодный старт по universal link — самый частый
     * путь приглашения.
     */
    @Test
    fun `freshly tapped link suppresses the intro before disk answers`() = runBlocking {
        pendingJoin.noteArrived()
        assertFalse(awaitIntro(viewModel(FakeIntroSeen())))
    }

    /** Прежняя отметка переносится: этот человек приветствие уже закрывал. */
    @Test
    fun `legacy mark is migrated`() = runBlocking {
        dataStore.edit { it[stringSetPreferencesKey("welcome_seen_accounts")] = setOf("1") }
        val introSeen = FakeIntroSeen()
        assertFalse(awaitIntro(viewModel(introSeen)))
        assertTrue(introSeen.hasSeen(), "перенос не состоялся")
    }

    /**
     * Главный случай миграции, и прежним хранилищем он НЕ покрывается.
     *
     * Прежняя отметка ставилась только при закрытии показанного приветствия, а
     * показывалось оно лишь тому, у кого нет ни одной тусы. У ветерана,
     * заведшего тусу раньше, чем приветствие появилось, хранилище пустое. Пока
     * он авторизован, это незаметно — его забирает ветка «есть сессия». Стоит
     * токену протухнуть, и он получил бы «что такое группа» как новичок.
     */
    @Test
    fun `veteran with a session is migrated and stays out after logout`() = runBlocking {
        session.signIn("token-A", Me(id = 1, displayName = "А"))
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == "token-A" } }

        val introSeen = FakeIntroSeen()
        val vm = viewModel(introSeen)
        assertEquals(false, awaitIntro(vm), "вошедшему приветствие не показывается")
        assertTrue(introSeen.hasSeen(), "вход не отметился как «видел»")

        session.logout()
        withTimeout(IO_WAIT_MS) { session.state.first { it?.token == null } }
        // Даём корню пересчитаться на новой эмиссии сессии.
        withTimeout(IO_WAIT_MS) {
            while (vm.showIntro.value != false) delay(10)
        }
        assertEquals(
            false,
            vm.showIntro.value,
            "ветеран после разлогина получил рассказ «что такое группа»",
        )
    }
}
