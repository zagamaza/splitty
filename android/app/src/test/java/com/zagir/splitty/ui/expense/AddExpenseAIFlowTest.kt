package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.RecipientSum
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.money.MoneyFormat
import java.util.Locale
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.OperationBody
import com.zagir.splitty.core.money.minorToUnitsRounded

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

    private fun surcharge(price: Long, split: String) = OperationItem(
        name = "Сбор", price = price, shares = null,
        kind = OperationItem.KIND_SURCHARGE, split = split, percent = 10,
    )

    // MARK: applyingParse

    /**
     * Плоская диктовка «ужин 20,80» не теряет копейки. Порт iOS
     * `testFlatParseKeepsCents`.
     *
     * Сервер отвечает парой {sum: 21, sumMinor: 2080}. Клиент читал только
     * округлённое поле, показывал 21, и сохранение честно отправляло 2100 —
     * 80 копеек исчезали молча, ровно в том пути, ради которого точная сумма
     * черновика и заводилась.
     */
    @Test
    fun `flat parse keeps cents`() {
        // Разделитель в поле ввода — из локали, поэтому её фиксируем: иначе
        // тест проверял бы настройки машины, а не код.
        MoneyFormat.localeOverride = Locale("ru", "RU")
        try {
        val json = """{"draft":{"description":"Ужин","sum":21,"sumMinor":2080},"questions":[]}"""
        val response = SplittyJson.decodeFromString<ParseResponse>(json)
        assertEquals(2080L, response.draft.exactMinor, "точное поле не разобралось")

        val next = form().applyingParse(response)
        assertEquals("20,80", next.sumText, "форма заполнена округлённой суммой")
        assertEquals(2080L, next.sumMinor)

        // Следующий круг правки уходит с той же точной суммой.
        val draft = next.currentParseDraft()!!
        assertEquals(2080L, draft.sumMinor)
        assertEquals(21L, draft.sum)
        } finally {
            MoneyFormat.localeOverride = null
        }
    }

    /** Ответ прежней версии сервера точного поля не несёт — работает целое. */
    @Test
    fun `flat parse without minor field stays whole`() {
        val json = """{"draft":{"description":"Такси","sum":300},"questions":[]}"""
        val next = form().applyingParse(SplittyJson.decodeFromString<ParseResponse>(json))
        assertEquals("300", next.sumText)
    }


    @Test
    fun `apply parse fills form and syncs recipients`() {
        val items = listOf(
            OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L))),
        )
        val next = form().applyingParse(
            ParseResponse(ParseDraft(description = "Ужин", sum = 1200, donorId = 1L, items = items), questions = listOf("Кто платил?")),
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
        val next = form().applyingParse(ParseResponse(ParseDraft(description = "Такси", sum = 400, donorId = null, items = null)))
        assertFalse(next.hasDraftItems)
        assertTrue(next.didRecognize)
        assertFalse(next.isEmptyForm)
    }

    @Test
    fun `empty result keeps composer without alert when question present`() {
        val next = form().applyingParse(
            ParseResponse(ParseDraft(description = "", sum = 0, donorId = null, items = null), questions = listOf("не удалось распознать")),
        )
        assertFalse(next.didRecognize)
        assertTrue(next.isEmptyForm)
        assertEquals(listOf("не удалось распознать"), next.parseQuestions)
        assertNull(next.alertMessage)
    }

    @Test
    fun `empty result without questions shows alert`() {
        val next = form().applyingParse(ParseResponse(ParseDraft(description = "", sum = 0, donorId = null, items = null)))
        assertFalse(next.didRecognize)
        assertTrue(next.isEmptyForm)
        assertTrue(next.alertMessage != null)
    }

    // MARK: подсветка/отмена правки

    @Test
    fun `correction marks changed items and allows undo`() {
        val pizza = OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L)))
        val beer = OperationItem(name = "Пиво", price = 600, shares = listOf(ItemShare(1L), ItemShare(2L)))
        val first = form().applyingParse(ParseResponse(ParseDraft(description = "Ужин", sum = 1800, donorId = 1L, items = listOf(pizza, beer))))
        assertFalse(first.canUndoParse)
        assertTrue(first.changedItemIndices.isEmpty())

        val beerFixed = OperationItem(name = "Пиво", price = 600, shares = listOf(ItemShare(2L)))
        val corrected = first.applyingParse(ParseResponse(ParseDraft(description = "Ужин", sum = 1800, donorId = 1L, items = listOf(pizza, beerFixed))))
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
        // Итог МИНОРНЫЙ: позиция без копеечного поля — легаси-форма, её цена
        // выводится из целой.
        assertEquals(100_000, next.itemizedTotal)
        assertTrue(next.canSave)
    }

    @Test
    fun `itemized sum comes from items, not from the stale sum field`() {
        // Поле суммы в itemized-режиме read-only и не пересчитывается при правке
        // позиции. Отправив его, мы расходились с Σ долей — сервер отвечал 400
        // «сумма долей должна равняться сумме операции» на каждую правку чека.
        val next = form(
            listOf(OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L)))),
        ).copy(sumText = "1000") // сумма от прошлого разбора, позиция подорожала
        val sums = listOf(RecipientSum.ofMinor(1L, 60_000), RecipientSum.ofMinor(2L, 60_000))

        // Величины МИНОРНЫЕ: 1200 единиц валюты — это 120 000.
        assertEquals(120_000, effectiveSumMinor(next, sums))
        assertEquals(
            sums.sumOf { it.exactMinor },
            effectiveSumMinor(next, sums),
            "sum разошёлся с Σ долей → 400",
        )
    }

    @Test
    fun `itemized receipt without a recognized total is still saveable`() {
        // Модель распознала блюда без общей суммы: sumText пуст, а поле read-only
        // — раньше «Сохранить» была активна, но save() упирался в «Введите сумму»
        // и черновик спасался только сбросом чека.
        val next = form(
            listOf(OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1L), ItemShare(2L)))),
        ).copy(sumText = "")
        assertNull(next.sum)

        assertEquals(
            80_000,
            effectiveSumMinor(next, listOf(RecipientSum.ofMinor(1L, 40_000), RecipientSum.ofMinor(2L, 40_000))),
        )
    }

    @Test
    fun `flat expense still uses the sum field`() {
        assertEquals(50_000, effectiveSumMinor(form().copy(sumText = "500"), null))
        assertNull(effectiveSumMinor(form().copy(sumText = ""), null))
        assertNull(effectiveSumMinor(form().copy(sumText = "0"), null))
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
        // Части сбора — в минорных: 100 и 20 единиц валюты.
        assertEquals(listOf(10_000L, 2_000L), next.personShares!!.map { it.surchargePart })
        next = next.togglingSurchargeRule(2)
        assertEquals(OperationItem.SPLIT_EQUALLY, next.draftItems[2].split)
        assertEquals(listOf(6_000L, 6_000L), next.personShares!!.map { it.surchargePart })
    }

    @Test
    fun `subtotal surcharges total`() {
        val next = form(
            listOf(
                OperationItem(name = "Пицца", price = 1200, shares = listOf(ItemShare(1L), ItemShare(2L))),
                surcharge(120, OperationItem.SPLIT_PROPORTIONAL),
            ),
        )
        assertEquals(120_000, next.itemizedSubtotal)
        assertEquals(12_000, next.itemizedSurcharges)
        assertEquals(132_000, next.itemizedTotal)
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
            listOf(
                UiText.res(R.string.expense_hint_who_is, "Саня"),
                UiText.res(R.string.expense_hint_how_much, "Пицца"),
                UiText.Raw("Кто платил?"),
            ),
            next.missingInfoHints,
        )
    }

    // MARK: Чек с копейками уходит верной парой величин

    /**
     * Расход по чеку на 20,80 отправляется как {sum: 21, sumMinor: 2080}.
     * Порт iOS `testItemizedReceiptSendsCorrectMoneyPair`.
     *
     * Доли чека давно минорные, а пара строилась сложением их целых полей с
     * последующим умножением на сто — уходило 2080/208000. Онлайн это прятал
     * сервер (он выводит итог из позиций заново), но в очередь, в показ
     * неотправленного и в ключ идемпотентности попадало враньё.
     */
    @Test
    fun `itemized receipt sends correct money pair`() {
        val next = form(
            listOf(
                OperationItem(
                    name = "Кофе", price = 10, priceMinor = 1040,
                    shares = listOf(ItemShare(1L)),
                ),
                OperationItem(
                    name = "Десерт", price = 10, priceMinor = 1040,
                    shares = listOf(ItemShare(2L)),
                ),
            ),
        )
        val sums = itemizedRecipientSums(next, listOf(1L, 2L))!!
        assertEquals(listOf(1040L, 1040L), sums.map { it.exactMinor }, "доли чека потеряли копейки")
        assertEquals(listOf(10L, 10L), sums.map { it.sum }, "целое поле доли — округлённая проекция")

        val sumMinor = effectiveSumMinor(next, sums)!!
        assertEquals(2080L, sumMinor, "точная сумма чека потеряна")
        assertEquals(21L, minorToUnitsRounded(sumMinor), "целое поле — округлённая проекция точного")

        // Тело запроса: проверяем то, что реально уедет на сервер.
        val body = OperationBody.of(
            description = "Ужин",
            sum = minorToUnitsRounded(sumMinor),
            sumMinor = sumMinor,
            donorId = 1L,
            split = ExpenseSplit.ByExactAmount(sums),
        )
        val wire = SplittyJson.encodeToString(OperationBody.serializer(), body)
        assertTrue("\"sum\":21" in wire, "в теле не 21: $wire")
        assertTrue("\"sumMinor\":2080" in wire, "в теле нет точной суммы: $wire")
        assertTrue("\"sumMinor\":1040" in wire, "в теле нет точных долей: $wire")
    }

    /**
     * Правка позиции меняет сумму следующего черновика: модели уходит текущий
     * итог, а не тот, что был при разборе.
     */
    @Test
    fun `parse draft follows edited items`() {
        val next = form(
            listOf(
                OperationItem(
                    name = "Кофе", price = 26, priceMinor = 2560,
                    shares = listOf(ItemShare(1L)),
                ),
                OperationItem(
                    name = "Десерт", price = 10, priceMinor = 1040,
                    shares = listOf(ItemShare(2L)),
                ),
            ),
        ).copy(sumText = "41,60") // итог прошлого разбора

        val draft = next.currentParseDraft()!!
        assertEquals(3600L, draft.sumMinor, "модели ушёл итог от прошлого разбора")
        assertEquals(36L, draft.sum)
    }
}
