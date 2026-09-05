package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
import com.zagir.splitty.core.money.MoneyLocale
import java.util.Locale
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Валидация распределения в режиме «По суммам» — порт iOS
 * `AddExpenseDistributionTests` на чистые вычисления [AddExpenseForm].
 */
class AddExpenseDistributionTest {

    /**
     * Локаль фиксируется: суммы форматирует системный форматтер, и без этого
     * тест проверял бы настройки машины, а не код.
     */
    @BeforeTest
    fun setUpLocale() {
        MoneyLocale.override = Locale("ru", "RU")
    }

    @AfterTest
    fun tearDownLocale() {
        MoneyLocale.override = null
    }

    private val members = (1L..3L).map { User(it, null, "U$it") }

    private fun byAmounts(sum: String, recipients: Set<Long>, amounts: Map<Long, String>) = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        description = "Ужин",
        payerId = 1L,
        sumText = sum,
        recipientIds = recipients,
        amountTexts = amounts,
        splitType = SplitType.BY_EXACT_AMOUNT,
    )

    @Test
    fun `remaining to distribute and blocked save`() {
        val f = byAmounts("1000", setOf(1L, 2L), mapOf(1L to "700", 2L to "200"))
        assertEquals(900, f.distributedTotal)
        assertEquals(100, f.remainingToDistribute)
        assertFalse(f.isDistributionBalanced)
        assertFalse(f.canSave)
    }

    @Test
    fun `exact distribution enables save`() {
        val f = byAmounts("1000", setOf(1L, 2L), mapOf(1L to "700", 2L to "300"))
        assertEquals(0, f.remainingToDistribute)
        assertTrue(f.isDistributionBalanced)
        assertTrue(f.canSave)
    }

    @Test
    fun `over distribution blocks save`() {
        val f = byAmounts("1000", setOf(1L, 2L), mapOf(1L to "800", 2L to "300"))
        assertEquals(-100, f.remainingToDistribute)
        assertFalse(f.canSave)
    }

    @Test
    fun `unselected member amounts are ignored`() {
        val f = byAmounts("1000", setOf(1L, 2L), mapOf(1L to "700", 2L to "300", 3L to "999"))
        assertEquals(1000, f.distributedTotal)
        assertTrue(f.isDistributionBalanced)
    }

    @Test
    fun `empty amount field counts as zero`() {
        val f = byAmounts("500", setOf(1L, 2L), mapOf(1L to "500"))
        assertEquals(0, f.enteredAmount(2L))
        assertEquals(0, f.remainingToDistribute)
        assertTrue(f.isDistributionBalanced)
    }

    @Test
    fun `zero sum is never balanced`() {
        val f = byAmounts("", setOf(1L), emptyMap())
        assertFalse(f.isDistributionBalanced)
        assertFalse(f.canSave)
    }

    @Test
    fun `equally mode does not block save`() {
        val f = byAmounts("", setOf(1L), emptyMap()).copy(splitType = SplitType.EQUALLY)
        assertEquals(SplitType.EQUALLY, f.splitType)
        assertTrue(f.canSave)
    }

    @Test
    fun `distribution hint shows remainder balanced overspent`() {
        val f = byAmounts("1000", setOf(1L), mapOf(1L to "400"))
        // Сверяем ресурс и подстановку, а не готовый русский текст: текст
        // зависит от локали, а подсказка обязана быть той же на любой.
        assertEquals(UiText.res(R.string.expense_remaining, "600 ₽"), f.distributionHint)
        assertEquals(
            UiText.res(R.string.expense_distributed),
            f.copy(amountTexts = mapOf(1L to "1000")).distributionHint,
        )
        assertEquals(
            UiText.res(R.string.expense_overspent, "100 ₽"),
            f.copy(amountTexts = mapOf(1L to "1100")).distributionHint,
        )
    }
}
