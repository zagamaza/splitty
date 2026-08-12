package com.zagir.splitty.core.model

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Клиентское превью долей itemized-чека ([derivedShares] и внутренние
 * [splitItem]/[splitSurcharge]/[splitByWeight]) — ТОЧНОЕ зеркало серверного
 * `DeriveShares` (`internal/api/itemsplit.go`). Кейсы портированы 1:1 из
 * серверного `itemsplit_test.go` и iOS `ItemDraftTests.swift`, включая
 * error-ветки и защиту от переполнения (проверяется на Long-краях, недоступных
 * через Int-поля DTO — там она структурно не срабатывает, но паритет сохранён).
 */
class OperationItemsTest {

    private fun sumMap(m: Map<Long, Long>): Long = m.values.sum()

    // --- SplitItem (порт internal/api itemsplit_test.go: TestSplitItem) ---

    @Test
    fun `splitItem поровну на троих`() {
        val got = splitItem(300, listOf(share(1, 1), share(2, 1), share(3, 1)))
        assertEquals(mapOf(1L to 100L, 2L to 100L, 3L to 100L), got)
    }

    @Test
    fun `splitItem неравные веса 5-3-2`() {
        val got = splitItem(500, listOf(share(2, 5), share(1, 3), share(4, 2)))
        assertEquals(mapOf(2L to 250L, 1L to 150L, 4L to 100L), got)
    }

    @Test
    fun `splitItem микс фикс плюс остаток поровну`() {
        val got = splitItem(3000, listOf(shareAmount(4, 500), share(1, 1), share(2, 1)))
        assertEquals(mapOf(4L to 500L, 1L to 1250L, 2L to 1250L), got)
    }

    @Test
    fun `splitItem полностью ручные суммы`() {
        val got = splitItem(2500, listOf(shareAmount(1, 1500), shareAmount(2, 700), shareAmount(3, 300)))
        assertEquals(mapOf(1L to 1500L, 2L to 700L, 3L to 300L), got)
    }

    @Test
    fun `splitItem неровный остаток поровну — меньший userId`() {
        val got = splitItem(100, listOf(share(3, 1), share(1, 1), share(2, 1)))
        assertEquals(mapOf(1L to 34L, 2L to 33L, 3L to 33L), got)
    }

    @Test
    fun `splitItem неровный остаток по весам — крупнейшей доле`() {
        val got = splitItem(10, listOf(share(1, 2), share(2, 1), share(3, 1)))
        assertEquals(mapOf(1L to 6L, 2L to 2L, 3L to 2L), got)
    }

    @Test
    fun `splitItem одиночный участник получает всё`() {
        assertEquals(mapOf(4L to 400L), splitItem(400, listOf(share(4, 1))))
    }

    @Test
    fun `splitItem фикс ровно равен цене`() {
        assertEquals(mapOf(1L to 800L), splitItem(800, listOf(shareAmount(1, 800))))
    }

    @Test
    fun `splitItem перебор фиксов над ценой — null`() {
        assertNull(splitItem(100, listOf(shareAmount(1, 150))))
    }

    @Test
    fun `splitItem фиксы не сходятся с ценой — null`() {
        assertNull(splitItem(1000, listOf(shareAmount(1, 400), shareAmount(2, 400))))
    }

    @Test
    fun `splitItem пустые shares при ненулевой цене — null`() {
        assertNull(splitItem(400, emptyList()))
    }

    @Test
    fun `splitItem отрицательный фикс — null`() {
        assertNull(splitItem(400, listOf(shareAmount(1, -50), share(2, 1))))
    }

    // --- Переполнение (порт overflow-ветвей; проверяется на Long-краях) ---

    @Test
    fun `splitByWeight переполнение суммы весов — null`() {
        assertNull(splitByWeight(100, listOf(WeightShare(1, Long.MAX_VALUE), WeightShare(2, 1))))
    }

    @Test
    fun `splitItem перебор фиксов на Int-краях — null`() {
        // Int-поля не дают переполнить сумму фиксов в Long, но два Int.MAX над
        // меньшей ценой — over-allocated → null (то же наблюдаемое поведение).
        val big = Int.MAX_VALUE.toLong()
        assertNull(splitItem(big, listOf(shareAmount(1, big), shareAmount(2, big))))
    }

    @Test(timeout = 2000L)
    fun `splitByWeight overflow возвращает null быстро без зацикливания`() {
        // Огромные amount*weight переполнили бы Long: наивная раздача остатка
        // ушла бы в почти бесконечный цикл (DoS). Ждём быстрый null.
        val got = splitByWeight(
            6917529027641081856L,
            listOf(WeightShare(1, 1_000_000), WeightShare(2, 1_000_000)),
        )
        assertNull(got)
    }

    @Test(timeout = 2000L)
    fun `splitItem overflow возвращает null быстро`() {
        val got = splitItem(
            6917529027641081856L,
            listOf(share(1, weight = 1_000_000), share(2, weight = 1_000_000)),
        )
        assertNull(got)
    }

    // --- SplitSurcharge (порт TestSplitSurcharge) ---

    @Test
    fun `splitSurcharge пропорционально съеденному`() {
        val base = mapOf(1L to 1800L, 2L to 1900L, 3L to 400L, 4L to 600L)
        val got = splitSurcharge(470, OperationItem.SPLIT_PROPORTIONAL, base)
        assertEquals(mapOf(1L to 180L, 2L to 190L, 3L to 40L, 4L to 60L), got)
        assertEquals(470L, sumMap(got!!))
    }

    @Test
    fun `splitSurcharge поровну между участниками базы`() {
        val base = mapOf(1L to 1800L, 2L to 1900L, 3L to 400L, 4L to 600L)
        val got = splitSurcharge(400, OperationItem.SPLIT_EQUALLY, base)
        assertEquals(mapOf(1L to 100L, 2L to 100L, 3L to 100L, 4L to 100L), got)
    }

    @Test
    fun `splitSurcharge поровну с остатком — меньший userId`() {
        val got = splitSurcharge(10, OperationItem.SPLIT_EQUALLY, mapOf(1L to 500L, 2L to 500L, 3L to 500L))
        assertEquals(mapOf(1L to 4L, 2L to 3L, 3L to 3L), got)
    }

    // --- DeriveShares через публичный derivedShares (порт TestDeriveShares_*) ---

    @Test
    fun `derivedShares полный чек (серверный фикстур, 4 юзера)`() {
        val items = listOf(
            item("Пицца", 1200, share(1, 1), share(2, 1), share(3, 1)),
            item("Баурсаки", 500, share(2, 5), share(1, 3), share(4, 2), qty = 10),
            item("Вино", 3000, shareAmount(4, 500), share(1, 1), share(2, 1)),
            surcharge("Сервисный сбор", 470, OperationItem.SPLIT_PROPORTIONAL, percent = 10),
        )
        val result = assertNotNull(items.derivedShares())
        assertEquals(5170L, result.total)
        assertEquals(mapOf(1L to 1980L, 2L to 2090L, 3L to 440L, 4L to 660L), result.shares)
        assertEquals(result.total, result.shares.values.sum())
    }

    @Test
    fun `derivedShares полный чек (iOS-фикстур, 3 юзера)`() {
        // user 1 — «я», user 2 — Лёха, user 3 — Маша.
        val items = listOf(
            item("Пицца", 1200, share(1, 1), share(2, 1), share(3, 1)),
            item("Баурсаки", 500, share(2, 5), share(1, 3), share(3, 2), qty = 10),
            item("Вино", 3000, shareAmount(3, 500), share(1, 1), share(2, 1)),
            surcharge("Сервисный сбор", 470, OperationItem.SPLIT_PROPORTIONAL, percent = 10),
        )
        val result = assertNotNull(items.derivedShares())
        assertEquals(5170L, result.total)
        assertEquals(mapOf(1L to 1980L, 2L to 2090L, 3L to 1100L), result.shares)
    }

    @Test
    fun `derivedShares неровный остаток — меньший userId`() {
        val items = listOf(item("Такси", 100, share(1, 1), share(2, 1), share(3, 1)))
        val result = assertNotNull(items.derivedShares())
        assertEquals(mapOf(1L to 34L, 2L to 33L, 3L to 33L), result.shares)
        assertEquals(100L, result.total)
    }

    @Test
    fun `derivedShares фикс плюс вес`() {
        val items = listOf(item("Вино", 3000, shareAmount(3, 500), share(1, 1), share(2, 1)))
        val result = assertNotNull(items.derivedShares())
        assertEquals(mapOf(1L to 1250L, 2L to 1250L, 3L to 500L), result.shares)
    }

    @Test
    fun `derivedShares Qty не влияет на расчёт`() {
        val a = listOf(item("Пиво", 100, share(1, 1), share(2, 1), qty = 1))
        val b = listOf(item("Пиво", 100, share(1, 1), share(2, 1), qty = 7))
        assertEquals(a.derivedShares()!!.shares, b.derivedShares()!!.shares)
    }

    @Test
    fun `derivedShares сумма сбора из Price, Percent игнорируется`() {
        val items = listOf(
            item("Еда", 1000, share(1, 1), share(2, 1)),
            surcharge("Сбор", 200, OperationItem.SPLIT_EQUALLY, percent = 999),
        )
        val result = assertNotNull(items.derivedShares())
        assertEquals(mapOf(1L to 600L, 2L to 600L), result.shares)
        assertEquals(1200L, result.total)
    }

    @Test
    fun `derivedShares перебор фиксов — null`() {
        val items = listOf(item("Позиция", 100, shareAmount(1, 80), shareAmount(2, 40)))
        assertNull(items.derivedShares())
    }

    @Test
    fun `derivedShares надбавка с нулевой ценой — null`() {
        val items = listOf(
            item("Кофе", 300, share(1, 1)),
            surcharge("Сбор", 0, OperationItem.SPLIT_EQUALLY),
        )
        assertNull(items.derivedShares())
    }

    @Test
    fun `derivedShares пустой список — пустой результат`() {
        val result = assertNotNull(emptyList<OperationItem>().derivedShares())
        assertEquals(0L, result.total)
        assertTrue(result.shares.isEmpty())
    }

    // --- Хелперы поверх DTO (isSurcharge / shareList / hasUnknown) ---

    @Test
    fun `item helpers isSurcharge shareList hasUnknown`() {
        val pizza = item("Пицца", 1200, share(1, 1), share(2, 1))
        assertFalse(pizza.isSurcharge)
        assertEquals(listOf(1L, 2L), pizza.shareList.map { it.userId })
        assertFalse(pizza.hasUnknown)

        val fee = surcharge("Сбор", 200, OperationItem.SPLIT_EQUALLY)
        assertTrue(fee.isSurcharge)
        assertTrue(fee.shareList.isEmpty())

        val beer = OperationItem(name = "Пиво", price = 0, shares = emptyList(), unknown = listOf("Саня"))
        assertTrue(beer.hasUnknown)
    }

    /**
     * Участник с нулевой базой (ничего не ел) не должен получать остаток от
     * округления пропорциональной надбавки: до схлопывания нулевых весов он
     * выигрывал tie-break по меньшему userId и платил весь сбор целиком.
     */
    @Test
    fun `надбавка не задевает участника с нулевой базой`() {
        val items = listOf(
            item("Пицца", 10, shareAmount(1L, 0), share(2L, 1), share(3L, 1)),
            surcharge("Сбор", 1, OperationItem.SPLIT_PROPORTIONAL),
        )
        val result = assertNotNull(items.derivedShares())
        assertEquals(11L, result.total)
        assertEquals(0, result.shares[1L]!!, "нулевой участник заплатил надбавку")
    }

    @Test
    fun `total over Int MAX_VALUE is calculated, not refused`() {
        // Поле ввода пропускает 9 цифр на позицию, позиций до 50 — итог легко
        // перебирает 32 бита. Раньше клиент на этом сдавался (сначала молча
        // заворачивал сумму в минус, потом честно отказывался считать), хотя
        // счёт в рупиях на несколько миллиардов — обычное дело. Теперь суммы
        // 64-битные, и чек считается целиком.
        val items = List(4) { i ->
            item("Позиция $i", 900_000_000, share(1L, 1), share(2L, 1))
        }
        val result = assertNotNull(items.derivedShares())
        assertEquals(3_600_000_000L, result.total)
        assertEquals(mapOf(1L to 1_800_000_000L, 2L to 1_800_000_000L), result.shares)
        assertEquals(result.total, result.shares.values.sum())
    }

    // --- Фикстуры ---

    private fun share(userId: Long, weight: Int) = ItemShare(userId = userId, weight = weight)
    private fun shareAmount(userId: Long, amount: Long) = ItemShare(userId = userId, amount = amount)

    private fun item(name: String, price: Long, vararg shares: ItemShare, qty: Int = 1) =
        OperationItem(name = name, price = price, qty = qty, shares = shares.toList())

    private fun surcharge(name: String, price: Long, split: String, percent: Int? = null) =
        OperationItem(
            name = name,
            price = price,
            shares = null,
            kind = OperationItem.KIND_SURCHARGE,
            split = split,
            percent = percent,
        )
}
