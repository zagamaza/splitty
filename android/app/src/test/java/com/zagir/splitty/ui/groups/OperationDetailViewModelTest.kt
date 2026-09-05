package com.zagir.splitty.ui.groups

import com.zagir.splitty.IO_WAIT_MS
import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.UiState
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

/**
 * VM карточки операции (Task 9): деталь комнаты кладёт в state участников
 * (нужны ReceiptCard'у) и позиции чека, а вложения различаются по типу.
 * Сеть — MockWebServer, Main — реальный диспетчер (viewModelScope живой),
 * состояние ждём через [state] с таймаутом.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class OperationDetailViewModelTest {

    /** Фейк-шифр для SessionStore: реальный Keystore в JVM недоступен. */
    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() {}
    }

    private val server = MockWebServer()
    private lateinit var cacheDir: File
    private lateinit var sessionDir: File
    private lateinit var scope: CoroutineScope

    @BeforeTest
    fun setUp() {
        Dispatchers.setMain(Dispatchers.Default)
        server.start()
        cacheDir = Files.createTempDirectory("op-detail-cache").toFile()
        sessionDir = Files.createTempDirectory("op-detail-session").toFile()
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

    private fun viewModel(): OperationDetailViewModel {
        val json = SplittyJson
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
            .build()
        val api = retrofit.create(SplittyApi::class.java)
        val parseApi = retrofit.create(ParseApi::class.java)
        val repository = SplittyRepository(api, parseApi, json, ApiCache(cacheDir, json))
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(sessionDir, "session.preferences_pb")
        }
        val session = SessionStore(dataStore, FakeTokenCipher(), scope)
        return OperationDetailViewModel(repository, session)
    }

    @Test
    fun `load puts room members, items and files into the card`() = runBlocking {
        server.enqueue(MockResponse().setBody(ROOM_JSON))
        val vm = viewModel()
        vm.start("65af", "op1")

        val card = withTimeout(IO_WAIT_MS) {
            vm.state.first { it is UiState.Content } as UiState.Content
        }.value

        // Участники комнаты доехали в state — ReceiptCard есть чем отрисовать.
        assertEquals(2, card.members.size)
        assertEquals(setOf(1L, 2L), card.members.map { it.id }.toSet())
        // Позиции чека распознались (позиция + сбор).
        assertEquals(2, card.operation.itemList.size)
        assertEquals("Пицца", card.operation.itemList.first().name)
        // Вложение считалось.
        assertEquals(1, card.operation.files?.size)
        assertEquals("image", card.operation.files?.first()?.type)
    }

    @Test
    fun `missing operation yields error state`() = runBlocking {
        server.enqueue(MockResponse().setBody(ROOM_JSON))
        val vm = viewModel()
        vm.start("65af", "nope")

        val state = withTimeout(IO_WAIT_MS) {
            vm.state.first { it is UiState.Error } as UiState.Error
        }
        assertEquals(UiText.res(R.string.error_operation_not_found), state.message)
    }

    @Test
    fun `attachmentKind maps raw api types`() {
        assertEquals(AttachmentKind.PHOTO, attachmentKind("photo"))
        assertEquals(AttachmentKind.PHOTO, attachmentKind("image"))
        assertEquals(AttachmentKind.VIDEO, attachmentKind("video"))
        assertEquals(AttachmentKind.DOCUMENT, attachmentKind("document"))
        assertEquals(AttachmentKind.OTHER, attachmentKind("sticker"))
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
              "totalSpent": 1320, "mySpent": 660,
              "myBalance": 660,
              "debts": [],
              "operations": [
                {
                  "id": "op1",
                  "description": "Ужин",
                  "sum": 1320,
                  "isDebtRepayment": false,
                  "donor": {"id": 1, "displayName": "Аня"},
                  "recipients": [
                    {"user": {"id": 1, "displayName": "Аня"}, "sum": 660},
                    {"user": {"id": 2, "displayName": "Боря"}, "sum": 660}
                  ],
                  "splitType": "by_exact_amount",
                  "createdAt": "2026-07-05T12:00:00Z",
                  "files": [{"type": "image", "fileId": "f1"}],
                  "items": [
                    {
                      "name": "Пицца", "price": 1200, "qty": 1,
                      "shares": [{"userId": 1, "weight": 1}, {"userId": 2, "weight": 1}]
                    },
                    {"name": "Сбор", "price": 120, "kind": "surcharge", "split": "proportional", "percent": 10}
                  ]
                }
              ]
            }
        """.trimIndent()
    }
}
