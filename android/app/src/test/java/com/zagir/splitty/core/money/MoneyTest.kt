package com.zagir.splitty.core.money

import com.zagir.splitty.core.model.CurrencySum
import kotlin.test.Test
import kotlin.test.assertEquals

/** Денежная арифметика и формат — канонические правила проекта (docs/API.md). */
class MoneyTest {

    // --- money(): формат «1 234 567 ₽» ---

    @Test
    fun `money formats thousands with spaces and currency symbol after`() {
        assertEquals("1 234 567 ₽", money(1_234_567, "RUB"))
        assertEquals("1 200 $", money(1_200, "USD"))
        assertEquals("999 €", money(999, "EUR"))
        assertEquals("15 000 Rp", money(15_000, "IDR"))
        assertEquals("0 ₽", money(0, "RUB"))
    }

    @Test
    fun `money keeps minus sign and unknown currency code as is`() {
        assertEquals("-4 300 ₽", money(-4_300, "RUB"))
        assertEquals("500 GBP", money(500, "GBP"))
    }

    @Test
    fun `money renders tenge and sum symbols`() {
        assertEquals("500 ₸", money(500, "KZT"))
        assertEquals("12 000 сум", money(12_000, "UZS"))
    }

    @Test
    fun `moneyRange formats uneven split hint`() {
        assertEquals("333–334 ₽", moneyRange(333, 334, "RUB"))
        assertEquals("1 000–1 001 $", moneyRange(1_000, 1_001, "USD"))
    }

    @Test
    fun `currencySymbol maps contract codes`() {
        assertEquals("₽", currencySymbol("RUB"))
        assertEquals("$", currencySymbol("USD"))
        assertEquals("€", currencySymbol("EUR"))
        assertEquals("Rp", currencySymbol("IDR"))
        assertEquals("XYZ", currencySymbol("XYZ"))
    }

    // --- shares(): каноническое правило деления (base = S/n, остаток первым r) ---

    @Test
    fun `shares 1000 by 3 gives 334 333 333`() {
        assertEquals(listOf(334, 333, 333), shares(1_000, 3))
    }

    @Test
    fun `shares divides exactly when no remainder`() {
        assertEquals(listOf(400, 400, 400), shares(1_200, 3))
    }

    @Test
    fun `shares remainder goes to first recipients in order`() {
        assertEquals(listOf(3, 3, 2, 2), shares(10, 4))
        assertEquals(listOf(1, 1, 1, 0, 0), shares(3, 5))
    }

    @Test
    fun `shares sum always equals total`() {
        for (sum in listOf(1, 7, 100, 999, 1_000_003)) {
            for (count in 1..7) {
                assertEquals(sum, shares(sum, count).sum(), "sum=$sum count=$count")
            }
        }
    }

    @Test
    fun `shares of non-positive count is empty`() {
        assertEquals(emptyList(), shares(100, 0))
        assertEquals(emptyList(), shares(100, -1))
    }

    // --- aggregateByCurrency(): валюты не смешиваются ---

    @Test
    fun `aggregate sums per currency without mixing`() {
        val result = aggregateByCurrency(
            listOf(
                CurrencySum("RUB", 500),
                CurrencySum("USD", -100),
                CurrencySum("RUB", 200),
            )
        )
        assertEquals(
            listOf(CurrencySum("RUB", 700), CurrencySum("USD", -100)),
            result,
        )
    }

    @Test
    fun `aggregate drops zero totals`() {
        val result = aggregateByCurrency(
            listOf(
                CurrencySum("RUB", 500),
                CurrencySum("RUB", -500),
                CurrencySum("USD", 10),
            )
        )
        assertEquals(listOf(CurrencySum("USD", 10)), result)
    }

    @Test
    fun `aggregate sorts by absolute sum descending with code tiebreak`() {
        val result = aggregateByCurrency(
            listOf(
                CurrencySum("USD", -100),
                CurrencySum("EUR", 100),
                CurrencySum("RUB", 5_000),
            )
        )
        // RUB — наибольший |суммы|; EUR и USD равны по модулю → по коду.
        assertEquals(
            listOf(
                CurrencySum("RUB", 5_000),
                CurrencySum("EUR", 100),
                CurrencySum("USD", -100),
            ),
            result,
        )
    }

    @Test
    fun `aggregate of empty list is empty`() {
        assertEquals(emptyList(), aggregateByCurrency(emptyList()))
    }
}
