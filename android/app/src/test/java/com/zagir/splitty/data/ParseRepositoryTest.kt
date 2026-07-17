package com.zagir.splitty.data

import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.coroutines.test.runTest
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Parse-эндпоинт репозитория (Task 7): multipart собирается как в iOS APIClient
 * (части draft/text/audio/image, имена и MIME), коды ошибок сервера (413/415/429/
 * 503) мапятся в [ApiException] с русским message, addAlias — best-effort.
 */
class ParseRepositoryTest {

    private val server = MockWebServer()
    private val dir: File = Files.createTempDirectory("parse-cache").toFile()

    @AfterTest
    fun tearDown() {
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun repository(): SplittyRepository {
        server.start()
        val json = SplittyJson
        val retrofit = Retrofit.Builder()
            .baseUrl(server.url("/"))
            .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
            .build()
        val api = retrofit.create(SplittyApi::class.java)
        val parseApi = retrofit.create(ParseApi::class.java)
        return SplittyRepository(api, parseApi, json, ApiCache(dir, json))
    }

    @Test
    fun `parse builds multipart with draft, text and image parts`() = runTest {
        server.enqueue(
            MockResponse().setBody("""{"draft":{"description":"Кофе","sum":300},"questions":null}""")
        )
        val repo = repository()

        val draft = ParseDraft(
            description = "Кофе",
            sum = 0,
            donorId = 1L,
            items = listOf(OperationItem(name = "Латте", price = 300, shares = listOf(ItemShare(1L)))),
        )
        val response = repo.parseOperation(
            roomId = "room1",
            image = byteArrayOf(1, 2, 3, 4),
            text = "кофе латте",
            draft = draft,
        )

        assertEquals("Кофе", response.draft.description)
        assertEquals(300, response.draft.sum)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/rooms/room1/operations/parse", request.path)
        assertTrue(request.getHeader("Content-Type")!!.startsWith("multipart/form-data"))

        val body = request.body.readUtf8()
        assertTrue(body.contains("name=\"draft\""), "нет части draft")
        assertTrue(body.contains("name=\"text\""), "нет части text")
        assertTrue(body.contains("name=\"image\""), "нет части image")
        assertTrue(body.contains("filename=\"image.jpg\""), "у image нет filename")
        assertTrue(body.contains("image/jpeg"), "у image нет MIME image/jpeg")
        // Пустого audio не слали — части быть не должно.
        assertFalse(body.contains("name=\"audio\""))
    }

    @Test
    fun `parse maps 413 to russian message`() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(413)
                .setBody("""{"error":{"code":"too_large","message":"тело запроса слишком большое"}}""")
        )
        val repo = repository()

        val e = assertFailsWith<ApiException> {
            repo.parseOperation(roomId = "room1", image = byteArrayOf(1))
        }
        assertEquals(413, e.status)
        assertEquals("тело запроса слишком большое", e.message)
    }

    @Test
    fun `parse maps 415 unsupported media`() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(415)
                .setBody("""{"error":{"code":"unsupported_media","message":"неподдерживаемый формат"}}""")
        )
        val repo = repository()

        val e = assertFailsWith<ApiException> {
            repo.parseOperation(roomId = "room1", image = byteArrayOf(1))
        }
        assertEquals(415, e.status)
        assertEquals("неподдерживаемый формат", e.message)
    }

    @Test
    fun `parse maps 429 rate limited`() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(429)
                .setBody("""{"error":{"code":"rate_limited","message":"слишком часто"}}""")
        )
        val repo = repository()

        val e = assertFailsWith<ApiException> {
            repo.parseOperation(roomId = "room1", text = "кофе")
        }
        assertEquals(429, e.status)
        assertEquals("слишком часто", e.message)
    }

    @Test
    fun `parse maps 503 ai disabled`() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(503)
                .setBody("""{"error":{"code":"ai_disabled","message":"распознавание недоступно"}}""")
        )
        val repo = repository()

        val e = assertFailsWith<ApiException> {
            repo.parseOperation(roomId = "room1", text = "кофе")
        }
        assertEquals(503, e.status)
        assertEquals("распознавание недоступно", e.message)
    }

    @Test
    fun `addAlias returns true on 204 and false on 403 without throwing`() = runTest {
        server.enqueue(MockResponse().setResponseCode(204))
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":{"code":"forbidden","message":"нет общей комнаты"}}"""))
        val repo = repository()

        assertTrue(repo.addAlias(userId = 1L, alias = "Саня"))
        assertFalse(repo.addAlias(userId = 2L, alias = "Маша"))
    }
}
