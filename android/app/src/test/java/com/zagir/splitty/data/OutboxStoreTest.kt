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
        sum: Long = 100,
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
    fun `old outbox json without items field loads without losing queue`() = runTest {
        // Формат ДО Task 1 (нет поля items) — на устройствах тестеров такие
        // файлы уже лежат; они обязаны прочитаться, а не молча стереть очередь.
        dir.mkdirs()
        file.writeText(
            """
            [
              {
                "localId": "old1",
                "roomId": "room1",
                "kind": "create",
                "payload": {
                  "description": "Такси",
                  "sum": 100,
                  "donorId": 1,
                  "recipientIds": [1, 2]
                },
                "createdAt": "2026-07-05T12:00:00Z",
                "status": "pending"
              }
            ]
            """.trimIndent()
        )

        val store = store()
        store.awaitLoaded()

        assertEquals(listOf("old1"), store.entries.value.map { it.localId })
        val restored = store.entry("old1")!!
        assertEquals("Такси", restored.payload.description)
        assertNull(restored.payload.items)
    }

    @Test
    fun `entry with items round-trips through file`() = runTest {
        val items = listOf(
            com.zagir.splitty.core.model.OperationItem(
                name = "Пицца",
                price = 1200,
                shares = listOf(
                    com.zagir.splitty.core.model.ItemShare(userId = 1L, weight = 1),
                    com.zagir.splitty.core.model.ItemShare(userId = 2L, amount = 400),
                ),
            ),
        )
        val first = store()
        first.add(
            OutboxEntry(
                localId = "it1",
                roomId = "room1",
                payload = OutboxPayload(
                    description = "Ужин по чеку",
                    sum = 1200,
                    donorId = 1L,
                    recipientSums = listOf(
                        com.zagir.splitty.core.model.RecipientSum(userId = 1L, sum = 800),
                        com.zagir.splitty.core.model.RecipientSum(userId = 2L, sum = 400),
                    ),
                    items = items,
                ),
                createdAt = Instant.parse("2026-07-05T12:00:00Z"),
            )
        )

        val second = store()
        second.awaitLoaded()

        val restored = second.entry("it1")!!
        assertEquals(items, restored.payload.items)
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

    @Test
    fun `unreadable queue file is not overwritten by the next write`() = runTest {
        // Файл есть, но читать его нельзя (на устройстве — ошибка ввода-вывода).
        // Раньше это было неотличимо от «файла нет», и первая же запись стирала
        // очередь неотправленных расходов начисто.
        file.writeText("[]")
        file.setReadable(false)
        val store = store()
        store.add(entry("local-new"))

        file.setReadable(true)
        assertEquals("[]", file.readText(), "нечитаемый файл перезаписан вслепую")
    }

    @Test
    fun `corrupted queue file does not disable persistence`() = runTest {
        // Битый JSON восстановлению не подлежит; отказ писать навсегда был бы
        // хуже — офлайн-расходы жили бы только в памяти и пропадали при закрытии.
        file.writeText("{ not json")
        val store = store()
        store.add(entry("local-1"))

        assertTrue("local-1" in file.readText(), "запись после битого файла не сохранилась")
        assertEquals(listOf("local-1"), OutboxStore(file, SplittyJson).also { it.awaitLoaded() }.entries.value.map { it.localId })
    }

    @Test
    fun `clear wipes the file even when the first read failed`() = runTest {
        // Реальная последовательность: первое чтение провалилось (на iOS —
        // залоченное устройство), позже файл снова читается. Логаут не должен
        // вернуть на диск очередь ПРЕДЫДУЩЕГО аккаунта через слияние в
        // persistLocked — иначе следующий вошедший отправит чужие расходы.
        store().add(entry("local-prev"))

        file.setReadable(false)
        val store = store()
        store.awaitLoaded() // чтение провалилось, didRead остаётся false
        file.setReadable(true)

        store.clear()

        assertFalse("local-prev" in file.readText(), "логаут оставил очередь прошлого аккаунта")
    }

    @Test
    fun `failed read does not latch loaded state`() = runTest {
        // Транзиентная ошибка чтения не должна считаться загрузкой: раньше
        // isLoaded взводился и после провала, очередь навсегда оставалась пустой
        // в памяти — расходы пропадали из UI, а OutboxSyncer после awaitLoaded
        // видел пустой список и не досылал их ни при одном восстановлении сети.
        store().add(entry("local-prev"))

        file.setReadable(false)
        val store = store()
        store.awaitLoaded() // чтение провалилось

        file.setReadable(true)
        store.awaitLoaded() // повторная попытка обязана прочитать файл

        assertEquals(
            listOf("local-prev"),
            store.entries.value.map { it.localId },
            "очередь не перечиталась после транзиентной ошибки — расходы потеряны для UI и досылки",
        )
    }
}
