package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationRecipient
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.model.operationsBlockingLeave
import com.zagir.splitty.core.model.RoomDetail
import java.time.Instant
import java.time.YearMonth
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * «Показать эти расходы» из отказа в выходе.
 *
 * Отказ советует убрать себя из расходов, но не говорит, из каких: в группе их
 * бывают сотни, и совет оставался невыполнимым. Фильтр показывает ровно те,
 * что держат, — поэтому он обязан не показывать лишнего.
 */
class BlockingFilterTest {

    private fun user(id: Long) = User(id = id, username = null, displayName = "U$id")

    private fun operation(
        id: String,
        donorId: Long,
        recipientIds: List<Long>,
        isRepayment: Boolean = false,
    ) = Operation(
        id = id,
        description = "Ужин $id",
        sum = 100,
        isDebtRepayment = isRepayment,
        donor = user(donorId),
        recipients = recipientIds.map { OperationRecipient(user = user(it), sum = 50) },
        createdAt = Instant.parse("2026-08-12T10:00:00Z"),
    )

    private fun section(month: String, operations: List<Operation>) = MonthSection(
        month = YearMonth.parse(month),
        title = month,
        operations = operations,
    )

    @Test
    fun `filter keeps only the listed operations`() {
        val sections = listOf(
            section("2026-08", listOf(operation("1", 1, listOf(2)), operation("2", 2, listOf(2)))),
            section("2026-07", listOf(operation("3", 1, listOf(2)))),
        )

        val kept = sectionsKeepingOnly(sections, setOf("1", "3"))

        assertEquals(listOf(listOf("1"), listOf("3")), kept.map { s -> s.operations.map { it.id } })
    }

    /** Месяц без единого мешающего расхода не должен показывать пустой заголовок. */
    @Test
    fun `months without matches disappear`() {
        val sections = listOf(
            section("2026-08", listOf(operation("1", 1, listOf(2)))),
            section("2026-07", listOf(operation("2", 2, listOf(2)))),
        )

        val kept = sectionsKeepingOnly(sections, setOf("1"))

        assertEquals(listOf(YearMonth.parse("2026-08")), kept.map { it.month })
    }

    /**
     * Фильтр и подпись под кнопкой выхода обязаны считать одно и то же:
     * «расходов: 2», а показанный список из трёх — это уже другая ошибка.
     */
    @Test
    fun `filter shows exactly what the refusal counts`() {
        val operations = listOf(
            operation("1", donorId = 1, recipientIds = listOf(2)),
            operation("2", donorId = 2, recipientIds = listOf(1, 2)),
            operation("3", donorId = 2, recipientIds = listOf(2)),
            operation("4", donorId = 1, recipientIds = listOf(2), isRepayment = true),
        )
        val room = RoomDetail(
            id = "room1",
            name = "Поездка",
            createdAt = Instant.parse("2026-08-12T09:00:00Z"),
            members = listOf(user(1), user(2)),
            currency = "RUB",
            totalSpent = 400,
            mySpent = 100,
            myBalance = 0,
            debts = emptyList(),
            operations = operations,
        )
        val blocking = room.operationsBlockingLeave(1)

        val kept = sectionsKeepingOnly(listOf(section("2026-08", operations)), blocking.map { it.id }.toSet())

        assertEquals(blocking.size, kept.sumOf { it.operations.size })
        assertEquals(listOf("1", "2"), kept.single().operations.map { it.id })
        assertTrue(kept.single().operations.none { it.id == "3" }, "чужой расход попал в фильтр")
    }
}
