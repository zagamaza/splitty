package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Чистая логика применения ответа AI-распознавания к форме ([applyingParse]) и
 * сборки текущего черновика ([currentParseDraft]) — без VM и сети.
 */
class ParseApplyTest {

    private val members = listOf(User(1L, null, "Загир"), User(2L, null, "Алмаз"))

    private fun baseForm() = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        recipientIds = emptySet(),
    )

    @Test
    fun `flat draft fills description, sum, payer and all recipients`() {
        val response = ParseResponse(
            draft = ParseDraft(description = "Ужин", sum = 1000, donorId = 2L, items = null),
            questions = listOf("кто платил?"),
        )
        val form = baseForm().applyingParse(response)

        assertEquals("Ужин", form.description)
        assertEquals("1000", form.sumText)
        assertEquals(2L, form.payerId)
        assertEquals(setOf(1L, 2L), form.recipientIds)
        assertTrue(form.draftItems.isEmpty())
        assertFalse(form.hasDraftItems)
        assertEquals(listOf("кто платил?"), form.parseQuestions)
    }

    @Test
    fun `itemized draft stores items and locks save`() {
        val items = listOf(
            OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1L), ItemShare(2L))),
        )
        val response = ParseResponse(
            draft = ParseDraft(description = "Чек", sum = 800, donorId = 1L, items = items),
        )
        val form = baseForm().applyingParse(response)

        assertEquals("Чек", form.description)
        assertEquals("800", form.sumText)
        assertEquals(items, form.draftItems)
        assertTrue(form.hasDraftItems)
        // Раскладка сходится (2 участника поровну) → itemized-сохранение доступно.
        assertTrue(form.canSave)
    }

    @Test
    fun `donor outside members does not change payer`() {
        val start = baseForm().copy(payerId = 1L)
        val response = ParseResponse(
            draft = ParseDraft(description = "Кофе", sum = 300, donorId = 999L),
        )
        val form = start.applyingParse(response)
        assertEquals(1L, form.payerId)
    }

    @Test
    fun `empty draft fields keep existing input`() {
        val start = baseForm().copy(description = "Такси", sumText = "250")
        val response = ParseResponse(draft = ParseDraft(description = "", sum = 0))
        val form = start.applyingParse(response)

        assertEquals("Такси", form.description)
        assertEquals("250", form.sumText)
    }

    @Test
    fun `currentParseDraft is null for empty form`() {
        assertNull(baseForm().currentParseDraft())
    }

    @Test
    fun `currentParseDraft carries description sum donor and items`() {
        val items = listOf(OperationItem(name = "Пиво", price = 200))
        val form = baseForm().copy(
            description = "Бар",
            sumText = "200",
            payerId = 1L,
            draftItems = items,
            splitType = SplitType.EQUALLY,
        )
        val draft = form.currentParseDraft()!!
        assertEquals("Бар", draft.description)
        assertEquals(200, draft.sum)
        assertEquals(1L, draft.donorId)
        assertEquals(items, draft.items)
    }

    @Test
    fun `correction that recognizes only the payer keeps the receipt`() {
        val items = listOf(
            OperationItem(name = "Пицца", price = 600, shares = listOf(ItemShare(userId = 1L))),
            OperationItem(name = "Кола", price = 200, shares = listOf(ItemShare(userId = 2L))),
        )
        val withReceipt = baseForm().copy(draftItems = items, didRecognize = true)

        // «платил Саша» — в черновике заполнен ТОЛЬКО donorId.
        val response = ParseResponse(
            draft = ParseDraft(description = "", sum = 0, donorId = 2L, items = null),
            questions = null,
        )
        val form = withReceipt.applyingParse(response)

        assertEquals(items, form.draftItems, "правка плательщика стёрла собранный чек")
        assertEquals(2L, form.payerId)
        assertNull(form.alertMessage, "распознанный плательщик — не пустой ответ")
        assertTrue(form.canUndoParse, "отмена должна остаться доступной")
    }

    // Ручная правка суммы сносит распознанный чек — но обратимо. Иначе одна
    // цифра в поле суммы уничтожала серверный чек уже сохранённой операции:
    // PUT уходил без позиций, и сервер переводил её в плоскую ветку.
    @Test
    fun `manual sum edit keeps an undo snapshot of the receipt`() {
        val items = listOf(
            OperationItem(name = "Пицца", price = 600, shares = listOf(ItemShare(userId = 1L))),
            OperationItem(name = "Кола", price = 200, shares = listOf(ItemShare(userId = 2L))),
        )
        val withReceipt = baseForm().copy(
            draftItems = items,
            description = "Ужин",
            sumText = "800",
            payerId = 1L,
            didRecognize = true,
        )

        val reset = withReceipt.resettingItems()
        assertTrue(reset.draftItems.isEmpty(), "позиции должны сброситься")
        assertEquals(SplitType.EQUALLY, reset.splitType)
        assertTrue(reset.canUndoParse, "баннер «Отменить» обязан появиться")
        assertEquals(items, reset.undoSnapshot?.draftItems, "снапшот чека не сохранён")

        // «Отменить» после ввода новой суммы возвращает чек целиком.
        val undone = reset.copy(sumText = "5").undoingParse()
        assertEquals(items, undone.draftItems)
        assertEquals("800", undone.sumText)
        assertEquals("Ужин", undone.description)
        assertEquals(1L, undone.payerId)
        assertFalse(undone.canUndoParse)
    }

    @Test
    fun `manual sum edit on a flat form does not offer undo`() {
        val flat = baseForm().copy(sumText = "100")
        val reset = flat.resettingItems()
        assertFalse(reset.canUndoParse, "чека не было — отменять нечего")
        assertNull(reset.undoSnapshot)
    }
}
