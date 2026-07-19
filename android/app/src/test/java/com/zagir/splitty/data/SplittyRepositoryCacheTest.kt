package com.zagir.splitty.data

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
 * Офлайн-контракт репозитория (Task 14): что видит пользователь без сети.
 * Правило одно и оно легко ломается правкой `cached`: транспортная ошибка →
 * последний успешный ответ из кеша (fromCache = true), HTTP-ошибка сервера
 * кешем НЕ гасится (иначе 500 показал бы вчерашние данные как свежие).
 */
class SplittyRepositoryCacheTest {

    private val server = MockWebServer()
    private val dir: File = Files.createTempDirectory("repo-cache").toFile()
    private val cacheDir = File(dir, "cache-api")

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
        return SplittyRepository(
            retrofit.create(SplittyApi::class.java),
            retrofit.create(ParseApi::class.java),
            json,
            ApiCache(cacheDir, json),
        )
    }

    private fun roomsBody(name: String, id: String = "room1") = MockResponse().setBody(
        """
        [{"id":"$id","name":"$name","createdAt":"2026-07-18T12:00:00Z","memberCount":2,
          "currency":"RUB","totalSpent":1000,"myBalance":-500}]
        """.trimIndent()
    )

    /**
     * «Нет сети»: сервер выключен — OkHttp получает ConnectException, то есть
     * ровно ту транспортную ошибку, на которую рассчитан фоллбэк на кеш.
     */
    private fun goOffline() = server.shutdown()

    @Test
    fun `fresh network response is returned and cached`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Поездка"))

        val fetched = repo.rooms()

        assertFalse(fetched.fromCache)
        assertEquals("Поездка", fetched.value.single().name)
        assertTrue(File(cacheDir, "rooms.json").isFile)
    }

    @Test
    fun `transport error falls back to cache`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Поездка"))
        repo.rooms() // прогрев кеша

        goOffline()
        val offline = repo.rooms()

        // Экран показывает данные, а не ошибку — офлайн-политика v1.
        assertTrue(offline.fromCache)
        assertEquals("Поездка", offline.value.single().name)
    }

    @Test
    fun `transport error without cache propagates the error`() = runTest {
        val repo = repository()
        goOffline()

        val error = assertFailsWith<ApiException> { repo.rooms() }

        assertEquals(ApiException.CODE_TRANSPORT, error.code)
    }

    @Test
    fun `server error is not masked by cache`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Поездка"))
        repo.rooms()

        server.enqueue(MockResponse().setResponseCode(500).setBody(""))
        val error = assertFailsWith<ApiException> { repo.rooms() }

        // 500 — это поломка сервера, показывать вчерашний список как живой
        // нельзя: пользователь не поймёт, почему его правки «не сохранились».
        assertEquals(500, error.status)
    }

    @Test
    fun `cache is per key so archived rooms do not shadow active`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Активная"))
        repo.rooms(archived = false)
        server.enqueue(roomsBody("Архивная", id = "room9"))
        repo.rooms(archived = true)

        goOffline()

        assertEquals("Активная", repo.rooms(archived = false).value.single().name)
        assertEquals("Архивная", repo.rooms(archived = true).value.single().name)
    }

    @Test
    fun `successful response refreshes stale cache`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Старое имя"))
        repo.rooms()

        server.enqueue(roomsBody("Новое имя"))
        assertEquals("Новое имя", repo.rooms().value.single().name)

        goOffline()
        assertEquals("Новое имя", repo.rooms().value.single().name)
    }

    @Test
    fun `corrupted cache file does not break offline read`() = runTest {
        val repo = repository()
        server.enqueue(roomsBody("Поездка"))
        repo.rooms()

        // Обрыв процесса на записи, ручная чистка — кеш не должен ронять экран.
        File(cacheDir, "rooms.json").writeText("{ битый json")
        goOffline()

        val error = assertFailsWith<ApiException> { repo.rooms() }
        assertEquals(ApiException.CODE_TRANSPORT, error.code)
    }
}
