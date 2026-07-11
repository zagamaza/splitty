package com.zagir.splitty.data

import com.zagir.splitty.core.model.SplittyJson
import java.io.File
import java.nio.file.Files
import java.time.Instant
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlinx.coroutines.test.runTest

/**
 * OutboxStore: постановка/правка/удаление записей, FIFO-порядок, статус
 * failed и персистентность (новый инстанс читает тот же файл).
 */
class OutboxStoreTest {

    private val dir: File = Files.createTempDirectory("outbox-test").toFile()
    private val file: File = File(dir, "outbox.json")

    @AfterTest
    fun tearDown() {
        dir.deleteRecursively()
    }

    private fun store(): OutboxStore = OutboxStore(file, SplittyJson)

    private fun entry(
        localId: String,
        roomId: String = "room1",
        description: String = "Такси",
        sum: Int = 100,
        createdAt: Instant = Instant.parse("2026-07-05T12:00:00Z"),
    ): OutboxEntry = OutboxEntry(
        localId = localId,
        roomId = roomId,
        payload = OutboxPayload(
            description = description,
            sum = sum,
            donorId = 1L,
            recipientIds = listOf(1L, 2L),
        ),
        createdAt = createdAt,
    )

    @Test
    fun `add appends entries in FIFO order`() = runTest {
        val store = store()
        store.add(entry("a"))
        store.add(entry("b"))
        store.add(entry("c"))

        assertEquals(listOf("a", "b", "c"), store.entries.value.map { it.localId })
        assertTrue(store.entries.value.all { it.status == OutboxStatus.PENDING })
    }

    @Test
    fun `update replaces payload and resets failed to pending`() = runTest {
        val store = store()
        store.add(entry("a", description = "Такси", sum = 100))
        store.markFailed("a", "Некорректные данные")

        store.update(
            "a",
            OutboxPayload(description = "Обед", sum = 250, donorId = 2L, recipientIds = listOf(2L)),
        )

        val updated = store.entry("a")!!
        assertEquals("Обед", updated.payload.description)
        assertEquals(250, updated.payload.sum)
        assertEquals(OutboxStatus.PENDING, updated.status)
        assertNull(updated.errorMessage)
    }

    @Test
    fun `remove deletes entry and keeps order of others`() = runTest {
        val store = store()
        store.add(entry("a"))
        store.add(entry("b"))
        store.add(entry("c"))

        store.remove("b")

        assertEquals(listOf("a", "c"), store.entries.value.map { it.localId })
        assertNull(store.entry("b"))
    }

    @Test
    fun `markFailed sets status and server message`() = runTest {
        val store = store()
        store.add(entry("a"))

        store.markFailed("a", "Нет доступа")

        val failed = store.entry("a")!!
        assertEquals(OutboxStatus.FAILED, failed.status)
        assertEquals("Нет доступа", failed.errorMessage)
        assertTrue(failed.isFailed)
    }

    @Test
    fun `entries survive restart via file`() = runTest {
        val first = store()
        first.add(entry("a", description = "Отель", sum = 1200))
        first.markFailed("a", "Ошибка сервера (500)")
        first.add(entry("b"))

        val second = store()
        second.awaitLoaded()

        assertEquals(listOf("a", "b"), second.entries.value.map { it.localId })
        val restored = second.entry("a")!!
        assertEquals("Отель", restored.payload.description)
        assertEquals(1200, restored.payload.sum)
        assertEquals(OutboxStatus.FAILED, restored.status)
        assertEquals("Ошибка сервера (500)", restored.errorMessage)
    }

    @Test
    fun `write is atomic - no tmp file left behind`() = runTest {
        val store = store()
        store.add(entry("a"))

        assertTrue(file.isFile)
        assertFalse(File(dir, "outbox.json.tmp").exists())
    }

    @Test
    fun `corrupted file loads as empty outbox`() = runTest {
        dir.mkdirs()
        file.writeText("{ это не json ]")

        val store = store()
        store.awaitLoaded()

        assertTrue(store.entries.value.isEmpty())
    }

    @Test
    fun `clear removes everything`() = runTest {
        val store = store()
        store.add(entry("a"))
        store.add(entry("b"))

        store.clear()

        assertTrue(store.entries.value.isEmpty())
        val reopened = store()
        reopened.awaitLoaded()
        assertTrue(reopened.entries.value.isEmpty())
    }

    @Test
    fun `payload split type follows recipientSums presence`() {
        val equally = OutboxPayload(description = "x", sum = 10, donorId = 1L, recipientIds = listOf(1L))
        val exact = OutboxPayload(
            description = "x",
            sum = 10,
            donorId = 1L,
            recipientSums = listOf(com.zagir.splitty.core.model.RecipientSum(userId = 1L, sum = 10)),
        )
        assertEquals(com.zagir.splitty.core.model.SplitType.EQUALLY, equally.splitType)
        assertEquals(com.zagir.splitty.core.model.SplitType.BY_EXACT_AMOUNT, exact.splitType)
        assertEquals(listOf(1L), equally.recipientOrder)
        assertEquals(listOf(1L), exact.recipientOrder)
    }
}
