package com.zagir.splitty.ui.components

import com.zagir.splitty.core.model.CurrencySum
import kotlin.test.assertEquals
import org.junit.Test

/** Проверяет ветки Glossary — особенно нулевую (её отсутствие врало на расчёте). */
class GlossaryTest {

    @Test
    fun `single sum captions by sign`() {
        assertEquals("вам должны", Glossary.balanceCaption(4300))
        assertEquals("вы должны", Glossary.balanceCaption(-990))
    }

    @Test
    fun `zero sum is settled, not a debt direction`() {
        assertEquals(Glossary.SETTLED, Glossary.balanceCaption(0))
        assertEquals("в расчёте", Glossary.balanceCaption(0))
    }

    @Test
    fun `mutual debts when totals have both signs`() {
        val totals = listOf(
            CurrencySum(currency = "RUB", sum = 500),
            CurrencySum(currency = "USD", sum = -300),
        )
        assertEquals("взаимные долги", Glossary.balanceCaption(totals, totals.first()))
    }

    @Test
    fun `multi caption falls back to primary sign when one direction`() {
        val positive = listOf(
            CurrencySum(currency = "RUB", sum = 500),
            CurrencySum(currency = "USD", sum = 300),
        )
        assertEquals("должен(на) вам", Glossary.balanceCaption(positive, positive.first()))

        val negative = listOf(CurrencySum(currency = "RUB", sum = -500))
        assertEquals("вы должны", Glossary.balanceCaption(negative, negative.first()))
    }

    @Test
    fun `multi caption zero primary is settled`() {
        val settled = listOf(CurrencySum(currency = "RUB", sum = 0))
        assertEquals(Glossary.SETTLED, Glossary.balanceCaption(settled, settled.first()))
    }

    @Test
    fun `pluralRu picks russian forms including teens`() {
        assertEquals("операция", pluralRu(1, "операция", "операции", "операций"))
        assertEquals("операции", pluralRu(3, "операция", "операции", "операций"))
        assertEquals("операций", pluralRu(5, "операция", "операции", "операций"))
        // 11–14 — всегда «many», несмотря на последнюю цифру.
        assertEquals("операций", pluralRu(11, "операция", "операции", "операций"))
        assertEquals("операций", pluralRu(13, "операция", "операции", "операций"))
        assertEquals("операция", pluralRu(21, "операция", "операции", "операций"))
    }
}
