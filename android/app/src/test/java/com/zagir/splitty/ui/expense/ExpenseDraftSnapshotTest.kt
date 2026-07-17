package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Снимок черновика формы для SavedStateHandle (восстановление после process
 * death): JSON round-trip и накат снимка на заново загруженную комнату.
 */
class ExpenseDraftSnapshotTest {

    private val members = listOf(User(1L, null, "Загир"), User(2L, null, "Алмаз"))

    private fun encode(s: ExpenseDraftSnapshot): String =
        SplittyJson.encodeToString(ExpenseDraftSnapshot.serializer(), s)

    private fun decode(raw: String): ExpenseDraftSnapshot =
        SplittyJson.decodeFromString(ExpenseDraftSnapshot.serializer(), raw)

    @Test
    fun `snapshot survives json round trip including item shares`() {
        val snapshot = ExpenseDraftSnapshot(
            selectedRoomId = "room1",
            description = "Чек из бара",
            sumText = "1200",
            payerId = 1L,
            recipientIds = listOf(1L, 2L),
            splitType = SplitType.BY_EXACT_AMOUNT,
            amountTexts = mapOf(1L to "700", 2L to "500"),
            draftItems = listOf(OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1L)))),
            parseQuestions = listOf("кто платил?"),
            didRecognize = true,
        )

        val restored = decode(encode(snapshot))
        assertEquals(snapshot, restored)
    }

    @Test
    fun `snapshot from form and applyTo recreate the form fields`() {
        val form = AddExpenseForm(
            isEditing = false,
            showsRoomPicker = false,
            selectedRoomId = "room1",
            members = members,
            currency = "RUB",
            description = "Ужин",
            sumText = "900",
            payerId = 2L,
            recipientIds = setOf(1L, 2L),
            splitType = SplitType.EQUALLY,
            draftItems = listOf(OperationItem(name = "Салат", price = 900)),
            didRecognize = true,
        )

        val snapshot = decode(encode(ExpenseDraftSnapshot.from(form)))
        // «Свежая» форма после process death: комната перезагружена (участники есть),
        // пользовательский ввод восстанавливается из снимка.
        val fresh = AddExpenseForm(
            isEditing = false,
            showsRoomPicker = false,
            selectedRoomId = "room1",
            members = members,
            currency = "RUB",
        )
        val restored = snapshot.applyTo(fresh)

        assertEquals("Ужин", restored.description)
        assertEquals("900", restored.sumText)
        assertEquals(2L, restored.payerId)
        assertEquals(setOf(1L, 2L), restored.recipientIds)
        assertEquals(form.draftItems, restored.draftItems)
        assertTrue(restored.didRecognize)
    }

    @Test
    fun `applyTo drops fields for members not in reloaded room`() {
        val snapshot = ExpenseDraftSnapshot(
            selectedRoomId = "room1",
            description = "Кофе",
            sumText = "300",
            payerId = 42L, // такого участника в комнате нет
            recipientIds = listOf(42L),
            amountTexts = mapOf(42L to "300"),
        )
        val fresh = AddExpenseForm(
            isEditing = false,
            showsRoomPicker = false,
            selectedRoomId = "room1",
            members = members,
            currency = "RUB",
            payerId = 1L,
            recipientIds = setOf(1L),
        )
        val restored = snapshot.applyTo(fresh)

        // Донор-«призрак» не подставляется; получатели-призраки отфильтрованы →
        // остаётся текущий выбор формы; чужие amountTexts выброшены.
        assertEquals(1L, restored.payerId)
        assertEquals(setOf(1L), restored.recipientIds)
        assertTrue(restored.amountTexts.isEmpty())
    }

    @Test
    fun `hasContent is false for empty draft`() {
        assertFalse(ExpenseDraftSnapshot.hasContent(ExpenseDraftSnapshot()))
        assertTrue(ExpenseDraftSnapshot.hasContent(ExpenseDraftSnapshot(description = "Кофе")))
    }
}
