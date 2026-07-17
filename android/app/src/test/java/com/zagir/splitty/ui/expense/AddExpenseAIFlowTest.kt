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
 * AI-поток формы расхода — порт iOS `AddExpenseAIFlowTests` на чистые функции
 * [AddExpenseForm]: применение ответа распознавания, подсветка/отмена правки,
 * сброс позиций, сопоставление имени, правило сбора и `canSave` itemized.
 */
class AddExpenseAIFlowTest {

    private val members = listOf(User(1L, null, "Загир"), User(2L, null, "Алмаз"))

    private fun form(items: List<OperationItem> = emptyList()) = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        draftItems = items,
        didRecognize = items.isNotEmpty(),
    )

    private fun surcharge(price: Int, split: String) = OperationItem(
        name = "Сбор", price = price, shares = null,
        kind = OperationItem.KIND_SURCHARGE, split = split, percent = 10,
    )

    // MARK: applyingParse

    @Test
    fun `apply parse fills form and syncs recipients`() {
        val items = listOf(
            OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L))),
        )
        val next = form().applyingParse(
            ParseResponse(ParseDraft("Ужин", 1200, 1L, items), questions = listOf("Кто платил?")),
        )
        assertEquals("Ужин", next.description)
        assertEquals("1200", next.sumText)
        assertEquals(items, next.draftItems)
        assertTrue(next.hasDraftItems)
        assertEquals(setOf(1L, 2L), next.recipientIds)
        assertEquals(listOf("Кто платил?"), next.parseQuestions)
        assertTrue(next.didRecognize)
        assertFalse(next.isEmptyForm)
    }

    @Test
    fun `flat result marked recognized not empty`() {
        val next = form().applyingParse(ParseResponse(ParseDraft("Такси", 400, null, null)))
        assertFalse(next.hasDraftItems)
        assertTrue(next.didRecognize)
        assertFalse(next.isEmptyForm)
    }

    @Test
    fun `empty result keeps composer without alert when question present`() {
        val next = form().applyingParse(
            ParseResponse(ParseDraft("", 0, null, null), questions = listOf("не удалось распознать")),
        )
        assertFalse(next.didRecognize)
        assertTrue(next.isEmptyForm)
        assertEquals(listOf("не удалось распознать"), next.parseQuestions)
        assertNull(next.alertMessage)
    }

    @Test
    fun `empty result without questions shows alert`() {
        val next = form().applyingParse(ParseResponse(ParseDraft("", 0, null, null)))
        assertFalse(next.didRecognize)
        assertTrue(next.isEmptyForm)
        assertTrue(next.alertMessage != null)
    }

    // MARK: подсветка/отмена правки

    @Test
    fun `correction marks changed items and allows undo`() {
        val pizza = OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L)))
        val beer = OperationItem(name = "Пиво", price = 600, shares = listOf(ItemShare(1L), ItemShare(2L)))
        val first = form().applyingParse(ParseResponse(ParseDraft("Ужин", 1800, 1L, listOf(pizza, beer))))
        assertFalse(first.canUndoParse)
        assertTrue(first.changedItemIndices.isEmpty())

        val beerFixed = OperationItem(name = "Пиво", price = 600, shares = listOf(ItemShare(2L)))
        val corrected = first.applyingParse(ParseResponse(ParseDraft("Ужин", 1800, 1L, listOf(pizza, beerFixed))))
        assertEquals(setOf(1), corrected.changedItemIndices)
        assertTrue(corrected.canUndoParse)

        val undone = corrected.undoingParse()
        assertEquals(listOf(pizza, beer), undone.draftItems)
        assertFalse(undone.canUndoParse)
        assertTrue(undone.changedItemIndices.isEmpty())
    }

    @Test
    fun `changed indices detects edits and additions`() {
        val a = OperationItem(name = "A", price = 100, shares = listOf(ItemShare(1L)))
        val b = OperationItem(name = "B", price = 200, shares = listOf(ItemShare(1L)))
        val b2 = OperationItem(name = "B", price = 250, shares = listOf(ItemShare(1L)))
        val c = OperationItem(name = "C", price = 300, shares = listOf(ItemShare(1L)))
        assertEquals(setOf(1, 2), changedItemIndices(listOf(a, b), listOf(a, b2, c)))
        assertEquals(emptySet(), changedItemIndices(listOf(a, b), listOf(a, b)))
    }

    // MARK: сброс / поровну

    @Test
    fun `reset items clears draft and switches to equally`() {
        val next = form(listOf(OperationItem(name = "Кофе", price = 300, shares = listOf(ItemShare(1L)))))
            .copy(splitType = SplitType.BY_EXACT_AMOUNT)
            .resettingItems()
        assertFalse(next.hasDraftItems)
        assertEquals(SplitType.EQUALLY, next.splitType)
    }

    @Test
    fun `collapse to equal keeps sum and allows undo`() {
        val items = listOf(OperationItem(name = "Пицца", price = 1000, shares = listOf(ItemShare(1L), ItemShare(2L))))
        val collapsed = form(items).collapsingToEqualSplit()
        assertFalse(collapsed.hasDraftItems)
        assertEquals("1000", collapsed.sumText)
        assertTrue(collapsed.canUndoParse)
        assertEquals(items, collapsed.undoingParse().draftItems)
    }

    // MARK: сопоставление имени

    @Test
    fun `resolve unknown applies locally and clears unknown`() {
        val next = form(
            listOf(
                OperationItem(
                    name = "Пиво", price = 500,
                    shares = listOf(ItemShare(1L)), unknown = listOf("Саня"),
                ),
            ),
        ).resolvingUnknown(itemIndex = 0, name = "Саня", userId = 2L)
        assertFalse(next.hasUnknownItems)
        assertNull(next.draftItems[0].unknown)
        assertTrue(next.draftItems[0].shares!!.any { it.userId == 2L })
        assertTrue(next.canSave)
        assertTrue(next.toastMessage != null)
    }

    // MARK: canSave itemized

    @Test
    fun `unknown present blocks save`() {
        val next = form(
            listOf(
                OperationItem(
                    name = "Пиво", price = 500,
                    shares = listOf(ItemShare(1L)), unknown = listOf("Саня"),
                ),
            ),
        )
        assertTrue(next.hasUnknownItems)
        assertEquals("Саня", next.firstUnknownName)
        assertFalse(next.canSave)
    }

    @Test
    fun `flat sum mismatch does not block itemized save`() {
        val next = form(
            listOf(OperationItem(name = "Пицца", price = 1000, shares = listOf(ItemShare(1L), ItemShare(2L)))),
        ).copy(sumText = "999")
        assertEquals(1000, next.itemizedTotal)
        assertTrue(next.canSave)
    }

    @Test
    fun `priceless item blocks then price fills`() {
        var next = form(
            listOf(
                OperationItem(name = "Пицца", price = 0, shares = listOf(ItemShare(1L), ItemShare(2L))),
                OperationItem(name = "Салат", price = 300, shares = listOf(ItemShare(1L))),
            ),
        )
        assertTrue(next.hasPricelessItems)
        assertFalse(next.canSave)
        next = next.replacingItem(0, OperationItem(name = "Пицца", price = 600, shares = listOf(ItemShare(1L), ItemShare(2L))))
        assertFalse(next.hasPricelessItems)
        assertTrue(next.canSave)
    }

    // MARK: правило сбора / подытоги

    @Test
    fun `toggle surcharge rule flips split and recalculates`() {
        var next = form(
            listOf(
                OperationItem(name = "Пицца", price = 1000, shares = listOf(ItemShare(1L))),
                OperationItem(name = "Салат", price = 200, shares = listOf(ItemShare(2L))),
                surcharge(120, OperationItem.SPLIT_PROPORTIONAL),
            ),
        )
        assertEquals(listOf(100, 20), next.personShares!!.map { it.surchargePart })
        next = next.togglingSurchargeRule(2)
        assertEquals(OperationItem.SPLIT_EQUALLY, next.draftItems[2].split)
        assertEquals(listOf(60, 60), next.personShares!!.map { it.surchargePart })
    }

    @Test
    fun `subtotal surcharges total`() {
        val next = form(
            listOf(
                OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L))),
                surcharge(120, OperationItem.SPLIT_PROPORTIONAL),
            ),
        )
        assertEquals(1200, next.itemizedSubtotal)
        assertEquals(120, next.itemizedSurcharges)
        assertEquals(1320, next.itemizedTotal)
    }

    // MARK: add / delete

    @Test
    fun `add blank item returns index and syncs`() {
        val (next, index) = form(
            listOf(OperationItem(name = "Пицца", price = 600, shares = listOf(ItemShare(1L)))),
        ).addingBlankItem()
        assertEquals(1, index)
        assertEquals(2, next.draftItems.size)
    }

    @Test
    fun `delete last item returns to flat form`() {
        val next = form(listOf(OperationItem(name = "Пицца", price = 600, shares = listOf(ItemShare(1L)))))
            .deletingItem(0)
        assertFalse(next.hasDraftItems)
    }

    // MARK: missingInfoHints

    @Test
    fun `missing info hints collect unknown prices and questions`() {
        val next = form(
            listOf(
                OperationItem(name = "Пицца", price = 0, shares = listOf(ItemShare(1L)), unknown = listOf("Саня")),
                OperationItem(name = "Салат", price = 300, shares = listOf(ItemShare(1L))),
            ),
        ).copy(parseQuestions = listOf("Сколько стоила пицца?", "Кто платил?"))
        assertEquals(
            listOf("Кто это — «Саня»?", "Сколько стоит «Пицца»?", "Кто платил?"),
            next.missingInfoHints,
        )
    }
}
