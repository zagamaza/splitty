package com.zagir.splitty.core.model

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Сколько расходов держат человека в группе.
 *
 * Раньше это знал только сервер: человек жал «Выйти» и получал отказ. Считаем
 * на клиенте — комната уже в памяти, — чтобы сказать заранее и назвать число.
 */
class LeaveBlockingOperationsTest {

    private fun user(id: Long) = User(id = id, username = null, displayName = "U$id")

    private fun operation(
        id: String,
        donorId: Long,
        recipientIds: List<Long>,
        isRepayment: Boolean = false,
    ) = Operation(
        id = id,
        description = "Ужин",
        sum = 100,
        isDebtRepayment = isRepayment,
        donor = user(donorId),
        recipients = recipientIds.map { OperationRecipient(user = user(it), sum = 50) },
        createdAt = Instant.parse("2026-08-12T10:00:00Z"),
    )

    private fun room(operations: List<Operation>) = RoomDetail(
        id = "room1",
        name = "Поездка",
        createdAt = Instant.parse("2026-08-12T09:00:00Z"),
        members = listOf(user(1), user(2)),
        currency = "RUB",
        totalSpent = 100,
        mySpent = 50,
        myBalance = 0,
        debts = emptyList(),
        operations = operations,
    )

    @Test
    fun `expense with the user as recipient blocks leaving`() {
        assertEquals(1, room(listOf(operation("1", donorId = 2, recipientIds = listOf(1, 2)))).operationsBlockingLeave(1).size)
    }

    @Test
    fun `expense paid by the user blocks leaving`() {
        assertEquals(1, room(listOf(operation("1", donorId = 1, recipientIds = listOf(2)))).operationsBlockingLeave(1).size)
    }

    /** Погашения не держат: они не перестают быть верными после ухода. */
    @Test
    fun `repayments do not block`() {
        assertTrue(room(listOf(operation("1", donorId = 1, recipientIds = listOf(2), isRepayment = true))).operationsBlockingLeave(1).isEmpty())
    }

    @Test
    fun `expenses of other people do not block`() {
        assertTrue(room(listOf(operation("1", donorId = 2, recipientIds = listOf(2)))).operationsBlockingLeave(1).isEmpty())
    }
}
