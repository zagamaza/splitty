package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Task 10 снимает запрет Task 1: itemized-операция (по позициям чека) правится
 * интерактивным чеком и сохраняется с [AddExpenseForm.draftItems]. Сохранение
 * блокируется только пока раскладка невыводима — нераспознанные имена, позиции
 * без цены или перебор фиксов (сервер отклонил бы то же самое).
 */
class ItemizedEditLockTest {

    private val members = listOf(User(1L, null, "Загир"), User(2L, null, "Алмаз"))

    private fun itemizedForm(items: List<OperationItem>): AddExpenseForm = AddExpenseForm(
        isEditing = true,
        isEditingSynced = true,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        description = "Ужин по чеку",
        sumText = "1200",
        payerId = 1L,
        recipientIds = setOf(1L, 2L),
        splitType = SplitType.EQUALLY,
        draftItems = items,
        didRecognize = true,
    )

    @Test
    fun `valid itemized form can be saved`() {
        val form = itemizedForm(
            listOf(OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L)))),
        )
        assertTrue(form.canSave)
        assertNull(form.saveBlockedReason)
    }

    @Test
    fun `unknown name blocks save with reason`() {
        val form = itemizedForm(
            listOf(
                OperationItem(
                    name = "Пиво", price = 500,
                    shares = listOf(ItemShare(1L)), unknown = listOf("Саня"),
                ),
            ),
        )
        assertFalse(form.canSave)
        assertTrue(form.hasUnknownItems)
        assertEquals(UiText.res(R.string.expense_block_unknown_items), form.saveBlockedReason)
    }

    @Test
    fun `priceless item blocks save`() {
        val form = itemizedForm(
            listOf(OperationItem(name = "Пицца", price = 0, shares = listOf(ItemShare(1L), ItemShare(2L)))),
        )
        assertFalse(form.canSave)
        assertTrue(form.hasPricelessItems)
    }

    @Test
    fun `over allocated fixed amounts block save`() {
        val form = itemizedForm(
            listOf(
                OperationItem(
                    name = "Позиция", price = 100,
                    shares = listOf(ItemShare(1L, amount = 80), ItemShare(2L, amount = 40)),
                ),
            ),
        )
        assertFalse(form.canSave)
    }
}
