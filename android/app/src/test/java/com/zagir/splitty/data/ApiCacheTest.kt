package com.zagir.splitty.data

import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.builtins.ListSerializer

/** ApiCache: запись/чтение JSON-файлов, атомарность, битые файлы, clear. */
class ApiCacheTest {

    private val dir: File = Files.createTempDirectory("api-cache-test").toFile()
    private val cache = ApiCache(dir, SplittyJson)

    private val me = Me(id = 123L, username = "zagir", displayName = "Загир")

    @AfterTest
    fun tearDown() {
        dir.deleteRecursively()
    }

    @Test
    fun `write then read roundtrips value`() = runTest {
        cache.write(ApiCache.Keys.ME, Me.serializer(), me)

        assertEquals(me, cache.read(ApiCache.Keys.ME, Me.serializer()))
    }

    @Test
    fun `write overwrites previous value and leaves no tmp files`() = runTest {
        cache.write(ApiCache.Keys.ME, Me.serializer(), me)
        val updated = me.copy(displayName = "Новое имя")
        cache.write(ApiCache.Keys.ME, Me.serializer(), updated)

        assertEquals(updated, cache.read(ApiCache.Keys.ME, Me.serializer()))
        // Атомарная запись: рабочий файл один, .tmp после move не остаётся.
        assertTrue(dir.listFiles()!!.none { it.name.endsWith(".tmp") })
    }

    @Test
    fun `read returns null when key missing`() = runTest {
        assertNull(cache.read("no-such-key", Me.serializer()))
    }

    @Test
    fun `read returns null for corrupted file`() = runTest {
        dir.mkdirs()
        File(dir, "${ApiCache.Keys.ME}.json").writeText("{оборванный json")

        assertNull(cache.read(ApiCache.Keys.ME, Me.serializer()))
    }

    @Test
    fun `lists roundtrip through ListSerializer`() = runTest {
        val friends = listOf(me, me.copy(id = 456L, displayName = "Алмаз"))
        cache.write(ApiCache.Keys.FRIENDS, ListSerializer(Me.serializer()), friends)

        assertEquals(friends, cache.read(ApiCache.Keys.FRIENDS, ListSerializer(Me.serializer())))
    }

    @Test
    fun `keys are distinct per room`() {
        assertEquals("room-abc", ApiCache.Keys.room("abc"))
        assertEquals("statistics-abc", ApiCache.Keys.statistics("abc"))
        assertEquals("rooms", ApiCache.Keys.rooms(archived = false))
        assertEquals("rooms-archived", ApiCache.Keys.rooms(archived = true))
    }

    @Test
    fun `clear wipes all cached responses`() = runTest {
        cache.write(ApiCache.Keys.ME, Me.serializer(), me)
        cache.write(ApiCache.Keys.rooms(false), ListSerializer(Me.serializer()), listOf(me))

        cache.clear()

        assertNull(cache.read(ApiCache.Keys.ME, Me.serializer()))
        assertNull(cache.read(ApiCache.Keys.rooms(false), ListSerializer(Me.serializer())))
        assertFalse(dir.listFiles().orEmpty().any { it.extension == "json" })
    }
}
