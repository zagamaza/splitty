package com.zagir.splitty.core.analytics

import com.zagir.splitty.core.model.SplittyJson
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlinx.coroutines.test.runTest

/**
 * Очередь продуктовых событий: персистентность, владение и потолок.
 */
class AnalyticsQueueTest {

    private val dir: File = Files.createTempDirectory("analytics-test").toFile()
    private val file: File = File(dir, "analytics.json")

    @AfterTest
    fun tearDown() {
        dir.deleteRecursively()
    }

    private fun queue(): AnalyticsQueue = AnalyticsQueue(file, SplittyJson)

    private fun record(id: String, owner: Long, name: String = "app_open") = AnalyticsRecord(
        id = id,
        name = name,
        at = "2026-09-04T10:00:00Z",
        session = "s-1",
        appVersion = "1.8",
        locale = "ru-RU",
        ownerUserId = owner,
    )

    /** Очередь переживает перезапуск: файл — единственное, что её держит. */
    @Test
    fun survivesRestart() = runTest {
        val first = queue()
        first.append(record("a-1", owner = 1))
        first.append(record("a-2", owner = 1))

        val reopened = queue()
        assertEquals(listOf("a-1", "a-2"), reopened.snapshot().map { it.id })
    }

    /**
     * Вход другого человека НЕ забирает события предыдущего.
     *
     * У очереди расходов записи наследуются — расход человек ввёл сам, и терять
     * его нельзя. С событием наоборот: содержимого оно не несёт, а приклеенное
     * чужому человеку это и испорченная аналитика, и приватность.
     */
    @Test
    fun eventsOfPreviousOwnerAreDropped() = runTest {
        val q = queue()
        q.append(record("a-1", owner = 1))
        q.append(record("b-1", owner = 2))

        q.keepOwned(2)

        assertEquals(listOf("b-1"), q.snapshot().map { it.id })
        assertTrue(q.take(10, owner = 1).isEmpty())
    }

    /** Выход чистит очередь целиком: отправлять эти события больше не под кем. */
    @Test
    fun logoutClearsQueue() = runTest {
        val q = queue()
        q.append(record("a-1", owner = 1))
        q.keepOwned(null)
        assertTrue(q.snapshot().isEmpty())
    }

    /** Потолок: файл не должен расти без предела, свежие события полезнее давних. */
    @Test
    fun dropsOldestOverCapacity() = runTest {
        val q = queue()
        repeat(AnalyticsQueue.CAPACITY + 10) { i -> q.append(record("e-$i", owner = 1)) }

        val ids = q.snapshot().map { it.id }
        assertEquals(AnalyticsQueue.CAPACITY, ids.size)
        assertEquals("e-10", ids.first())
    }

    /** Отправленные записи уходят, остальные остаются. */
    @Test
    fun removesOnlySentRecords() = runTest {
        val q = queue()
        q.append(record("a-1", owner = 1))
        q.append(record("a-2", owner = 1))

        q.remove(setOf("a-1"))

        assertEquals(listOf("a-2"), q.snapshot().map { it.id })
    }
}
