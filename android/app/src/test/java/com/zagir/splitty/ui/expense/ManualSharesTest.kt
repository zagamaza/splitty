package com.zagir.splitty.ui.expense

import com.zagir.splitty.R
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * «Изменить суммы участников»: чек сворачивается в плоский расход по суммам.
 * Порт iOS `AddExpenseManualSharesTests`.
 *
 * Раньше в режиме чека карточки деления не было вовсе, а единственный выход —
 * «Поровну на всех» — выбрасывал распределение по позициям. Пользователь после
 * ИИ-разбора не мог поправить долю одного человека.
 */
class ManualSharesTest {

    private val members = listOf(User(1L, null, "Аня"), User(2L, null, "Боря"), User(3L, null, "Вера"))

    private fun form(items: List<OperationItem>, fractional: Boolean = false) = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        draftItems = items,
        didRecognize = true,
        fractional = fractional,
    )

    private fun item(name: String, price: Long, ids: List<Long>) = OperationItem(
        name = name, price = price, shares = ids.map { ItemShare(userId = it, weight = 1) },
    )

    /** Неравные доли со сбором: у каждого ровно то, что насчитали позиции. */
    @Test
    fun `convert keeps each persons share including surcharge`() {
        val before = form(
            listOf(
                item("Пицца", 1000, listOf(1, 2)),
                item("Салат", 200, listOf(2)),
                OperationItem(
                    name = "Сбор", price = 120, shares = null,
                    kind = OperationItem.KIND_SURCHARGE,
                    split = OperationItem.SPLIT_PROPORTIONAL, percent = 10,
                ),
            ),
        )
        assertFalse(assertNotNull(before.manualSharesPreview()).wasRounded)

        val after = before.convertingToManualShares()

        assertFalse(after.hasDraftItems, "позиции обязаны уйти — иначе сервер проигнорирует суммы")
        assertEquals(SplitType.BY_EXACT_AMOUNT, after.splitType)
        assertEquals(setOf(1L, 2L), after.recipientIds)
        assertEquals(mapOf(1L to "550", 2L to "770"), after.amountTexts)
        assertEquals("1320", after.sumText)
    }

    /** 100 на троих в целой тусе: 34 / 33 / 33, итог 100, и об округлении говорим. */
    @Test
    fun `whole room rounds by largest remainder and says so`() {
        val before = form(listOf(item("Такси", 100, listOf(1, 2, 3))))
        assertTrue(assertNotNull(before.manualSharesPreview()).wasRounded)

        val after = before.convertingToManualShares()

        assertEquals(mapOf(1L to "34", 2L to "33", 3L to "33"), after.amountTexts)
        assertEquals("100", after.sumText, "итог обязан сохраниться")
    }

    /** Та же сумма в тусе с копейками: доли точные, округления нет. */
    @Test
    fun `fractional room keeps exact minor`() {
        val preview = assertNotNull(
            form(listOf(item("Такси", 100, listOf(1, 2, 3))), fractional = true).manualSharesPreview(),
        )
        assertFalse(preview.wasRounded)
        assertEquals(listOf(3334L, 3333L, 3333L), preview.shares.map { it.second })
    }

    /** 1 ₽ на троих: после округления двое должны ноль — их не отправляем. */
    @Test
    fun `zero shares after rounding are dropped`() {
        val after = form(listOf(item("Жвачка", 1, listOf(1, 2, 3)))).convertingToManualShares()

        assertEquals(setOf(1L), after.recipientIds)
        assertEquals(mapOf(1L to "1"), after.amountTexts)
        assertEquals("1", after.sumText)
    }

    /** Нераспознанное имя: потеря позиций разблокировала бы неверное сохранение. */
    @Test
    fun `not offered while name unknown`() {
        val items = listOf(
            OperationItem(
                name = "Пицца", price = 1200,
                shares = listOf(ItemShare(userId = 1, weight = 1)), unknown = listOf("Саня"),
            ),
        )
        assertNull(form(items).manualSharesPreview())
    }

    /** Позиция без цены — то же самое. */
    @Test
    fun `not offered while price missing`() {
        assertNull(form(listOf(item("Пицца", 0, listOf(1, 2)))).manualSharesPreview())
    }

    /**
     * Отмена возвращает не только позиции, но и деление, которое было ДО.
     * Снимок раньше хранил позиции, описание, сумму и плательщика.
     */
    @Test
    fun `undo restores items and split`() {
        val items = listOf(item("Пицца", 1000, listOf(1, 2)))
        val before = form(items).copy(recipientIds = setOf(1L, 2L), sumText = "1000")

        val undone = before.convertingToManualShares().undoingParse()

        assertEquals(items, undone.draftItems)
        assertEquals(SplitType.EQUALLY, undone.splitType)
        assertEquals(setOf(1L, 2L), undone.recipientIds)
        assertEquals(emptyMap(), undone.amountTexts)
        assertEquals("1000", undone.sumText)
    }

    /** «Поровну на всех» отменяется так же полно — с прежними получателями. */
    @Test
    fun `undo after equal split restores recipients`() {
        val items = listOf(item("Пицца", 1000, listOf(1, 2)))
        val collapsed = form(items).copy(recipientIds = setOf(1L, 2L)).collapsingToEqualSplit()
        assertEquals(setOf(1L, 2L, 3L), collapsed.recipientIds)

        val undone = collapsed.undoingParse()

        assertEquals(items, undone.draftItems)
        assertEquals(setOf(1L, 2L), undone.recipientIds)
    }

    /**
     * Отмена после ПЛОСКИХ ручных сумм возвращает их. Ровно ради этого снимок и
     * расширен: 70/30 → голосовая правка с другими участниками → «Отменить».
     */
    @Test
    fun `undo after voice correction restores manual amounts`() {
        val manual = form(emptyList())
            .applyingParse(ParseResponse(draft = ParseDraft(description = "Ужин", sum = 100, donorId = 1)))
            .copy(
                splitType = SplitType.BY_EXACT_AMOUNT,
                recipientIds = setOf(1L, 2L),
                amountTexts = mapOf(1L to "70", 2L to "30"),
            )
        val corrected = manual.applyingParse(
            ParseResponse(
                draft = ParseDraft(
                    description = "Ужин", sum = 100, donorId = 1,
                    items = listOf(item("Ужин", 100, listOf(2, 3))),
                ),
                questions = listOf("Кто такой Саня?"),
            ),
        )
        assertTrue(corrected.canUndoParse)

        val undone = corrected.undoingParse()

        assertFalse(undone.hasDraftItems)
        assertEquals(SplitType.BY_EXACT_AMOUNT, undone.splitType)
        assertEquals(setOf(1L, 2L), undone.recipientIds)
        assertEquals(mapOf(1L to "70", 2L to "30"), undone.amountTexts)
        assertEquals(emptyList(), undone.parseQuestions)
    }

    /** Кнопка не прячется, а объясняет, почему недоступна. */
    @Test
    fun `unavailable reason is given instead of hiding the button`() {
        val unknown = listOf(
            OperationItem(
                name = "Пицца", price = 1200,
                shares = listOf(ItemShare(userId = 1, weight = 1)), unknown = listOf("Саня"),
            ),
        )
        assertEquals(R.string.expense_edit_amounts_unavailable_items, form(unknown).manualSharesUnavailableReason())
        assertNull(form(listOf(item("Пицца", 1200, listOf(1, 2)))).manualSharesUnavailableReason())
    }

    @Test
    fun `distribute preserves total`() {
        val shares = distributeShares(listOf(1L to 3334L, 2L to 3333L, 3L to 3333L), 100L)
        assertEquals(listOf(3400L, 3300L, 3300L), shares.map { it.second })
        assertEquals(10_000L, shares.sumOf { it.second })
    }

    /** Ничья по остатку — в порядке появления в чеке. */
    @Test
    fun `distribute breaks ties by order`() {
        assertEquals(listOf(100L, 0L), distributeShares(listOf(1L to 50L, 2L to 50L), 100L).map { it.second })
    }
}
