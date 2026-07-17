package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Живой расчёт деления позиции в шите ([computeItemSplitStatus]) — зеркало
 * серверного derivedShares: нет цены/участников, перебор/недобор фиксов и
 * сошедшееся деление по весам/суммам.
 */
class ItemSheetSplitTest {

    private val members = listOf(User(1L, null, "A"), User(2L, null, "B"))

    private fun status(
        price: Int,
        participating: Set<Long>,
        byAmount: Boolean = false,
        weights: Map<Long, Int> = emptyMap(),
        amounts: Map<Long, String> = emptyMap(),
    ) = computeItemSplitStatus(price, members, participating, byAmount, weights, amounts)

    @Test
    fun `no price`() {
        assertEquals(ItemSplitStatus.NoPrice, status(price = 0, participating = setOf(1L)))
    }

    @Test
    fun `no participants`() {
        assertEquals(ItemSplitStatus.NoParticipants, status(price = 100, participating = emptySet()))
    }

    @Test
    fun `even weights split`() {
        val s = status(price = 100, participating = setOf(1L, 2L))
        assertTrue(s is ItemSplitStatus.Ok)
        assertEquals(mapOf(1L to 50, 2L to 50), (s as ItemSplitStatus.Ok).shares)
    }

    @Test
    fun `weighted split gives more to bigger weight`() {
        val s = status(price = 300, participating = setOf(1L, 2L), weights = mapOf(1L to 2, 2L to 1))
        assertTrue(s is ItemSplitStatus.Ok)
        assertEquals(mapOf(1L to 200, 2L to 100), (s as ItemSplitStatus.Ok).shares)
    }

    @Test
    fun `all fixed under price is under`() {
        val s = status(price = 100, participating = setOf(1L, 2L), byAmount = true, amounts = mapOf(1L to "30", 2L to "20"))
        assertEquals(ItemSplitStatus.Under(50), s)
    }

    @Test
    fun `fixed over price is over`() {
        val s = status(price = 100, participating = setOf(1L, 2L), byAmount = true, amounts = mapOf(1L to "80", 2L to "40"))
        assertEquals(ItemSplitStatus.Over(20), s)
    }

    @Test
    fun `fixed plus auto splits remainder`() {
        val s = status(price = 100, participating = setOf(1L, 2L), byAmount = true, amounts = mapOf(1L to "30"))
        assertTrue(s is ItemSplitStatus.Ok)
        assertEquals(mapOf(1L to 30, 2L to 70), (s as ItemSplitStatus.Ok).shares)
    }
}
