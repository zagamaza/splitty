package com.zagir.splitty.core.money

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Подстановка валюты на экране создания тусы. Порт iOS
 * `RoomCurrencySuggestionTests`.
 *
 * Валюта — не вопрос, а подтверждение: следующая туса чаще всего заводится в
 * той же валюте, что и предыдущая, а у первой лучший ориентир — регион.
 */
class RoomCurrencySuggestionTest {
    private val available =
        listOf("RUB", "USD", "EUR", "JPY", "CNY", "KRW", "BRL", "IDR", "KZT", "UZS")

    @Test
    fun `валюта последней тусы важнее региона`() {
        assertEquals("EUR", suggestedRoomCurrency("EUR", "USD", available))
    }

    @Test
    fun `без прошлой тусы берётся регион`() {
        assertEquals("USD", suggestedRoomCurrency(null, "USD", available))
    }

    @Test
    fun `валюта региона вне справочника — умолчание`() {
        // Регион может дать валюту, которой у нас нет: молча падаем на
        // умолчание, а не показываем пустую строку.
        assertEquals("RUB", suggestedRoomCurrency(null, "CHF", available))
    }

    @Test
    fun `неизвестная прошлая валюта не мешает региону`() {
        assertEquals("EUR", suggestedRoomCurrency("CHF", "EUR", available))
    }

    @Test
    fun `без подсказок — умолчание`() {
        assertEquals("RUB", suggestedRoomCurrency(null, null, available))
    }

    @Test
    fun `регистр кода не важен`() {
        assertEquals("USD", suggestedRoomCurrency("usd", null, available))
    }
}
