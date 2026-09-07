package com.zagir.splitty.core.money

import com.zagir.splitty.ui.settleup.filterSumInput
import java.util.Locale
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * Дробные суммы: показ и разбор ввода. Порт `MoneyFractionTests` из iOS.
 *
 * Ошибка здесь молчит: сумма 20,80 превращается в 21 и расходится с сервером,
 * который хранит точное значение.
 */
class MoneyFractionTest {

    @BeforeTest
    fun setUp() {
        MoneyFormat.localeOverride = Locale("ru", "RU")
    }

    @AfterTest
    fun tearDown() {
        MoneyFormat.localeOverride = null
    }

    /** Форматтер разделяет число и символ неразрывным пробелом. */
    private fun plain(text: String) = text.replace(' ', ' ').replace(' ', ' ')

    // --- Показ ---

    @Test
    fun `дробная часть показывается только когда она есть`() {
        assertEquals("20,80 $", plain(moneyMinor(2080, "USD")), "копейки потерялись")
        assertEquals("21 $", plain(moneyMinor(2100, "USD")), "ровная сумма показана с нулями")
    }

    @Test
    fun `в тусе без копеек показ не изменился`() {
        assertEquals(money(1234567, "RUB"), moneyMinor(123456700, "RUB"))
    }

    // --- Разбор ввода ---

    @Test
    fun `принимаются оба разделителя`() {
        assertEquals(2080L, minorFromInput("20,80"), "запятая с русской клавиатуры не принята")
        assertEquals(2080L, minorFromInput("20.80"), "точка с цифровой панели не принята")
    }

    @Test
    fun `один знак после разделителя — это десятые`() {
        assertEquals(2080L, minorFromInput("20,8"), "20,8 — это 80 копеек, а не 8")
    }

    @Test
    fun `целое число`() {
        assertEquals(2100L, minorFromInput("21"))
    }

    @Test
    fun `не суммы отвергаются`() {
        assertNull(minorFromInput(""), "пустая строка — не сумма")
        assertNull(minorFromInput("20,805"), "три знака после разделителя — не сумма")
        assertNull(minorFromInput("20,,8"), "два разделителя — не сумма")
        assertNull(minorFromInput("абв"), "буквы — не сумма")
        assertNull(minorFromInput("20,"), "разделитель без дробной части — не сумма")
    }

    // --- Поле ввода ---

    @Test
    fun `поле ввода не теряет точность`() {
        for (minor in listOf(2080L, 2100L, 1L, 99L, 100L, 123456L)) {
            assertEquals(minor, minorFromInput(inputTextFromMinor(minor)), "потеряли точность на $minor")
        }
    }

    @Test
    fun `ведущий ноль в копейках сохраняется`() {
        assertEquals("20,80", inputTextFromMinor(2080))
        assertEquals("20,08", inputTextFromMinor(2008), "ведущий ноль потерян")
        assertEquals("21", inputTextFromMinor(2100), "ровная сумма показана с нулями")
    }

    // --- Фильтр поля ---

    @Test
    fun `в тусе без копеек разделитель не набрать`() {
        assertEquals("2080", filterSumInput("20,80", fractional = false))
    }

    @Test
    fun `в тусе с копейками разделитель один и знаков два`() {
        assertEquals("20,80", filterSumInput("20,80", fractional = true))
        assertEquals("20,80", filterSumInput("20,805", fractional = true), "третий знак пропущен в поле")
        assertEquals("20,8", filterSumInput("20,,8", fractional = true), "второй разделитель пропущен")
        assertEquals("20", filterSumInput("2а0", fractional = true), "буквы пропущены в поле")
    }
}
