package com.zagir.splitty.ui.expense

import android.content.Context
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.billing.BillingService
import com.zagir.splitty.billing.SubscriptionRepository
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.analytics.testAnalytics
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
import java.io.File
import java.nio.file.Files
import javax.inject.Provider
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
import kotlinx.coroutines.flow.MutableStateFlow
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
 * Куда уходит захваченное медиа: правило одно для голоса и фото (см.
 * [stopsAtReview]). Первый ввод в пустую форму ОСТАНАВЛИВАЕТСЯ на экране
 * разбора (распознавание не стартует), а всё, что уточняет готовый черновик или
 * досылается ко второму уже приложенному источнику, уходит в /parse сразу.
 *
 * Сеть — MockWebServer, NetworkMonitor требует Context — отсюда Robolectric.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AddExpenseCaptureRoutingTest {

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

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        dir = Files.createTempDirectory("add-expense-capture").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
    }

    @AfterTest
    fun tearDown() {
        Dispatchers.resetMain()
        server.shutdown()
        scope.cancel()
        dir.deleteRecursively()
    }

    private fun viewModel(): AddExpenseViewModel {
        val context: Context = ApplicationProvider.getApplicationContext()
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
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        val session = SessionStore(dataStore, FakeTokenCipher(), scope)
        val outbox = OutboxStore(File(dir, "outbox.json"), json)
        val syncer = OutboxSyncer(outbox, repository, session, MutableStateFlow(true), scope)
        // SubscriptionRepository появился в конструкторе вместе с платным тарифом
        // (коммит «Splitor Plus»), а этот тест не обновили — весь юнит-сорссет
        // перестал компилироваться. Настоящий репозиторий здесь безвреден:
        // BillingClient только собирается, соединение с Play открывает
        // startConnection, которого мы не зовём, а api берётся лениво.
        val subscriptions = SubscriptionRepository(
            Provider { retrofit.create(SplittyApi::class.java) },
            BillingService(context),
            testAnalytics(dir, SplittyJson, session, scope),
        )
        return AddExpenseViewModel(
            repository, session, outbox, syncer, SavedStateHandle(), subscriptions,
            testAnalytics(dir, SplittyJson, session, scope),
            NetworkMonitor(context),
        )
    }

    /** VM с загруженной формой группы (комната отдаётся MockWebServer'ом). */
    private suspend fun loadedViewModel(): AddExpenseViewModel {
        server.enqueue(MockResponse().setBody(ROOM_JSON))
        val vm = viewModel()
        vm.start("65af", null)
        withTimeout(5_000) { vm.state.first { it is UiState.Content } }
        return vm
    }

    private fun receiptFile(): String =
        File(dir, "receipt.jpg").apply { writeBytes(ByteArray(64) { 7 }) }.absolutePath

    private fun form(vm: AddExpenseViewModel): AddExpenseForm =
        (vm.state.value as UiState.Content).value

    @Test
    fun `first receipt into an empty form waits on the review screen`() = runBlocking {
        val vm = loadedViewModel()

        vm.onReceiptCaptured(receiptFile())

        // Распознавание НЕ стартовало: фото ждёт решения (распознать / добавить
        // голос / убрать), как и первая диктовка.
        assertFalse(form(vm).isParsing)
        assertNotNull(vm.pendingReceiptPath.value)
        // Единственный запрос — загрузка комнаты, в /parse ничего не ушло.
        assertEquals(1, server.requestCount)
    }

    @Test
    fun `receipt refining an existing draft goes straight to recognition`() = runBlocking {
        val vm = loadedViewModel()
        vm.onDescriptionChange("Ужин")

        vm.onReceiptCaptured(receiptFile())

        assertTrue(form(vm).isParsing)
        assertNull(vm.pendingReceiptPath.value)
    }

    @Test
    fun `receipt added to a recorded voice goes straight to recognition`() = runBlocking {
        val vm = loadedViewModel()
        // Диктовка уже ждёт на экране разбора («Добавить фото чека»): фото уйдёт
        // вместе с ней одним запросом, второй остановки быть не должно.
        vm.attachAudio(File(dir, "voice.wav").apply { writeBytes(ByteArray(32) { 1 }) }.absolutePath)

        vm.onReceiptCaptured(receiptFile())

        assertTrue(form(vm).isParsing)
        assertNull(vm.pendingReceiptPath.value)
        assertNull(vm.pendingAudioPath.value)
    }

    @Test
    fun `voice recorded over an attached receipt goes straight to recognition`() = runBlocking {
        val vm = loadedViewModel()
        vm.attachReceipt(receiptFile())

        vm.onVoiceRecorded(File(dir, "voice.wav").apply { writeBytes(ByteArray(32) { 1 }) }.absolutePath)

        assertTrue(form(vm).isParsing)
        assertNull(vm.pendingAudioPath.value)
        assertNull(vm.pendingReceiptPath.value)
    }

    @Test
    fun `first voice into an empty form waits on the review screen`() = runBlocking {
        val vm = loadedViewModel()

        vm.onVoiceRecorded(File(dir, "voice.wav").apply { writeBytes(ByteArray(32) { 1 }) }.absolutePath)

        assertFalse(form(vm).isParsing)
        assertNotNull(vm.pendingAudioPath.value)
        assertEquals(1, server.requestCount)
    }

    private companion object {
        val ROOM_JSON = """
            {
              "id": "65af", "name": "Ужин", "createdAt": "2026-07-05T12:00:00Z",
              "isArchived": false,
              "currency": "RUB",
              "members": [
                {"id": 1, "displayName": "Аня"},
                {"id": 2, "displayName": "Боря"}
              ],
              "totalSpent": 0, "mySpent": 0, "myBalance": 0,
              "debts": [], "operations": []
            }
        """.trimIndent()
    }
}
