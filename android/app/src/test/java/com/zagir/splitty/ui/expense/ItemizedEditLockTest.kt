package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Правка itemized-операции (по позициям чека) заблокирована плоской формой:
 * [AddExpenseForm.canSave] = false при [AddExpenseForm.isItemizedLocked],
 * даже когда все прочие поля валидны (временно, до Task 10).
 */
class ItemizedEditLockTest {

    private fun validForm(itemizedLocked: Boolean): AddExpenseForm = AddExpenseForm(
        isEditing = true,
        isEditingSynced = true,
        isItemizedLocked = itemizedLocked,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = listOf(User(1L, null, "Загир"), User(2L, null, "Алмаз")),
        description = "Ужин по чеку",
        sumText = "1200",
        payerId = 1L,
        recipientIds = setOf(1L, 2L),
        splitType = SplitType.EQUALLY,
    )

    @Test
    fun `itemized locked form cannot be saved`() {
        assertFalse(validForm(itemizedLocked = true).canSave)
    }

    @Test
    fun `same form without lock can be saved`() {
        assertTrue(validForm(itemizedLocked = false).canSave)
    }
}
