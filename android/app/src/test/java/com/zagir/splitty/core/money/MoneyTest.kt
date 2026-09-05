package com.zagir.splitty.core.money

import com.zagir.splitty.core.model.CurrencySum
import java.util.Locale
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.assertTrue
import kotlin.test.assertFalse
import kotlin.test.Test
import kotlin.test.assertEquals

/** Денежная арифметика и формат — канонические правила проекта (docs/API.md). */
class MoneyTest {

    /**
     * Локаль фиксируется: иначе тест проверял бы настройки машины, а не код.
     * Разделитель тысяч и место символа валюты теперь берёт системный форматтер.
     */
    @BeforeTest
    fun setUp() {
        MoneyLocale.override = Locale("ru", "RU")
    }

    @AfterTest
    fun tearDown() {
        MoneyLocale.override = null
    }

    // --- money(): формат «1 234 567 ₽» ---

    @Test
    fun `money formats thousands with spaces and currency symbol after`() {
        assertEquals("1 234 567 ₽", money(1_234_567, "RUB"))
        assertEquals("1 200 $", money(1_200, "USD"))
        assertEquals("999 €", money(999, "EUR"))
        assertEquals("15 000 Rp", money(15_000, "IDR"))
        assertEquals("0 ₽", money(0, "RUB"))
    }

    @Test
    fun `money keeps minus sign and unknown currency code as is`() {
        assertEquals("-4 300 ₽", money(-4_300, "RUB"))
        assertEquals("500 GBP", money(500, "GBP"))
    }

    @Test
    fun `money renders tenge and sum symbols`() {
        assertEquals("500 ₸", money(500, "KZT"))
        assertEquals("12 000 сум", money(12_000, "UZS"))
    }

    @Test
    fun `moneyRange formats uneven split hint`() {
        assertEquals("333–334 ₽", moneyRange(333, 334, "RUB"))
        assertEquals("1 000–1 001 $", moneyRange(1_000, 1_001, "USD"))
    }

    @Test
    fun `currencySymbol maps contract codes`() {
        assertEquals("₽", currencySymbol("RUB"))
        assertEquals("$", currencySymbol("USD"))
        assertEquals("€", currencySymbol("EUR"))
        assertEquals("Rp", currencySymbol("IDR"))
        assertEquals("XYZ", currencySymbol("XYZ"))
    }

    @Test
    fun `sum is written in the language of the interface`() {
        // У сума нет знака — только слово, и оно единственное во всей таблице,
        // что зависит от языка. Было русским всегда: немец читал «12 000 сум»
        // кириллицей. Значения совпадают с iOS.
        MoneyLocale.override = Locale("de", "DE")
        assertEquals("Sum", currencySymbol("UZS"))
        for (language in listOf("en", "es", "fr")) {
            MoneyLocale.override = Locale(language)
            assertEquals("sum", currencySymbol("UZS"), "$language: сум написан не на своём языке")
        }
        MoneyLocale.override = Locale("ru", "RU")
        assertEquals("сум", currencySymbol("UZS"))
    }

    // --- shares(): каноническое правило деления (base = S/n, остаток первым r) ---

    @Test
    fun `shares 1000 by 3 gives 334 333 333`() {
        assertEquals(listOf(334L, 333L, 333L), shares(1_000, 3))
    }

    @Test
    fun `shares divides exactly when no remainder`() {
        assertEquals(listOf(400L, 400L, 400L), shares(1_200, 3))
    }

    @Test
    fun `shares remainder goes to first recipients in order`() {
        assertEquals(listOf(3L, 3L, 2L, 2L), shares(10, 4))
        assertEquals(listOf(1L, 1L, 1L, 0L, 0L), shares(3, 5))
    }

    @Test
    fun `shares sum always equals total`() {
        for (sum in listOf(1L, 7L, 100L, 999L, 1_000_003L)) {
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

    /**
     * Kotlin усекает целочисленное деление к нулю, поэтому для отрицательных
     * сумм остаток отрицательный: наивное `index < remainder` не срабатывало
     * ни разу и Σ долей не сходилась с sum вопреки доккомментарию.
     */
    @Test
    fun `shares conserves negative sums`() {
        for (sum in listOf(0L, -1L, -5L, -10L, -100L, -999L)) {
            for (count in 1..7) {
                assertEquals(sum, shares(sum, count).sum(), "shares($sum, $count)")
            }
        }
    }

    @Test
    fun `moneyRange keeps sign of lower bound`() {
        assertEquals("-100–50\u00A0\u20BD", moneyRange(-100L, 50L, "RUB"))
    }

    /**
     * Суммы 64-битные на всём пути, поэтому итог по валюте больше не насыщается
     * и не заворачивается: в рупиях счёт за поездку легко переваливает за
     * 2.1 миллиарда, и раньше на этом «вы должны» превращалось в «вам должны».
     */
    @Test
    fun `aggregateByCurrency keeps totals beyond 32 bits`() {
        val huge = listOf(
            CurrencySum("IDR", Int.MAX_VALUE.toLong()),
            CurrencySum("IDR", Int.MAX_VALUE.toLong()),
        )
        assertEquals(Int.MAX_VALUE.toLong() * 2, aggregateByCurrency(huge).single().sum)
    }

    /**
     * Крупнейший по модулю долг — «основной»: он показывается крупно. Раньше
     * насыщение давало ровно Int.MIN_VALUE, abs от него отрицателен, и самый
     * большой долг уезжал в конец списка.
     */
    @Test
    fun `largest debt sorts first even beyond 32 bits`() {
        val got = aggregateByCurrency(
            listOf(
                CurrencySum(currency = "USD", sum = 100),
                CurrencySum(currency = "RUB", sum = Int.MIN_VALUE.toLong() * 4),
            )
        )
        assertEquals("RUB", got.first().currency)
    }

    /**
     * Крупная сумма в рупиях не должна терять разряды: раньше она не помещалась
     * в 32 бита и показывалась отрицательной, то есть человек видел долг вместо
     * траты.
     */
    @Test
    fun `money keeps every digit of a large rupiah sum`() {
        assertEquals("3 600 000 000 Rp", money(3_600_000_000L, "IDR"))
        assertEquals("-3 600 000 000 Rp", money(-3_600_000_000L, "IDR"))
    }

    /**
     * Разделитель тысяч и место символа валюты меняются вместе с языком.
     * Раньше они склеивались руками по русскому образцу, и человек с
     * английским интерфейсом видел «1 234 567 $» вместо «$1,234,567».
     */
    @Test
    fun `format follows the locale`() {
        MoneyLocale.override = Locale("en", "US")
        val english = money(1_234_567L, "USD")
        assertTrue(english.startsWith("$"), "в английском символ валюты стоит перед суммой: $english")
        assertTrue(english.contains(","), "в английском разделитель тысяч — запятая: $english")

        MoneyLocale.override = Locale("ru", "RU")
        val russian = money(1_234_567L, "RUB")
        assertTrue(russian.endsWith("\u20BD"), "в русском символ валюты стоит после суммы: $russian")
        assertFalse(russian.contains(","), "в русском запятая — это дробная часть, а не тысячи: $russian")
    }

    /** Сумма не должна переноситься посередине: пробелы неразрывные. */
    @Test
    fun `money uses non-breaking spaces`() {
        assertTrue(
            money(1_000L, "RUB").contains("\u00A0"),
            "обычный пробел позволил бы разорвать «1 000 ₽» переносом строки",
        )
    }
}
