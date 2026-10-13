package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.RoomSummary
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Нулевой общий итог по группам не должен выдаваться за «все долги погашены»,
 * когда долги на самом деле есть или не считаются (паритет iOS).
 */
class ZeroBalanceSummaryTest {
    private fun room(id: String, balance: Long, unavailable: Boolean = false) = RoomSummary(
        id = id, name = id, createdAt = Instant.ofEpochSecond(1_750_000_000), memberCount = 2,
        currency = "RUB", totalSpent = 1000, myBalance = balance, debtsUnavailable = unavailable,
    )

    @Test
    fun `opposite debts cancel out are not settled`() {
        assertEquals(ZeroBalanceSummary.CANCEL_OUT, zeroBalanceSummary(listOf(room("a", 500), room("b", -500))))
    }

    @Test
    fun `only unavailable rooms are not settled`() {
        assertEquals(ZeroBalanceSummary.UNAVAILABLE, zeroBalanceSummary(listOf(room("a", 0, unavailable = true))))
    }

    @Test
    fun `truly settled`() {
        assertEquals(ZeroBalanceSummary.SETTLED, zeroBalanceSummary(listOf(room("a", 0), room("b", 0, unavailable = true))))
        assertEquals(ZeroBalanceSummary.SETTLED, zeroBalanceSummary(emptyList()))
    }
}
