package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.money.MoneyLocale
import java.util.Locale
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// Чистые JVM-тесты подсказок чек-строки (ReceiptCard) — зеркало iOS shareHint/
// perPersonText/isEven. Логика без Android-зависимостей.
//
// Проверяем ТИП подсказки, а не её текст: текст зависит от языка приложения, а
// правило деления — нет. Раньше тест сравнивал русские строки и тем самым
// закреплял то, что экран чека переведён не был.
class ReceiptHintTest {

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

    private fun item(
        name: String = "Позиция",
        price: Long,
        qty: Int = 1,
        shares: List<ItemShare>? = null,
        kind: String = OperationItem.KIND_ITEM,
        split: String? = null,
        unknown: List<String>? = null,
    ) = OperationItem(
        name = name,
        price = price,
        qty = qty,
        shares = shares,
        kind = kind,
        split = split,
        unknown = unknown,
    )

    private fun share(id: Long, weight: Int = 1, amount: Long? = null) = ItemShare(id, weight, amount)

    @Test fun singleShare_wholeItem() {
        assertEquals(ShareHint.Whole, shareHint(item(price = 500, shares = listOf(share(1)))))
    }

    @Test fun evenSplit_exact() {
        val it = item(price = 800, shares = listOf(share(1), share(2)))
        assertEquals(ShareHint.PerPerson(price = 800, people = 2), shareHint(it))
    }

    @Test fun evenSplit_range_whenNotDivisible() {
        // 100 на троих: 33–34, честный диапазон (иначе «по 33 × 3» не сходится с 100).
        val it = item(price = 100, shares = listOf(share(1), share(2), share(3)))
        assertEquals(ShareHint.PerPerson(price = 100, people = 3), shareHint(it))
    }

    @Test fun pricelessWithShares() {
        assertEquals(ShareHint.NoPrice, shareHint(item(price = 0, shares = listOf(share(1)))))
    }

    @Test fun pricelessNoShares() {
        assertEquals(ShareHint.None, shareHint(item(price = 0, shares = emptyList())))
    }

    @Test fun unknownName() {
        val it = item(price = 500, shares = listOf(share(1)), unknown = listOf("Саня"))
        assertEquals(ShareHint.Unknown, shareHint(it))
    }

    @Test fun exactAmounts_onlyFixed() {
        val it = item(price = 300, shares = listOf(share(1, amount = 100), share(2, amount = 200)))
        assertEquals(ShareHint.ExactAmounts, shareHint(it))
    }

    @Test fun fixedPlusWeighted() {
        val it = item(price = 300, shares = listOf(share(1, amount = 100), share(2)))
        assertEquals(ShareHint.FixedThenEven(fixed = 100), shareHint(it))
    }

    @Test fun weightedUneven_perUnit() {
        // Вова съел вдвое больше: 3 доли, 90 / 3 = 30 за штуку.
        val it = item(price = 90, shares = listOf(share(1, weight = 1), share(2, weight = 2)))
        assertEquals(ShareHint.PerUnit(price = 90, units = 3), shareHint(it))
    }

    @Test fun isEven_equalWeights_true() {
        assertTrue(isEven(item(price = 100, shares = listOf(share(1), share(2)))))
    }

    @Test fun isEven_mixedWeights_false() {
        assertFalse(isEven(item(price = 100, shares = listOf(share(1, weight = 1), share(2, weight = 2)))))
    }

    @Test fun isEven_ignoresFixedAmounts() {
        // Фиксы не участвуют в «ровности» — весов нет, значит even.
        assertTrue(isEven(item(price = 100, shares = listOf(share(1, amount = 50), share(2, amount = 50)))))
    }

    @Test fun perPersonText_exactAndRange() {
        assertEquals("50 ₽", perPersonText(100, 2, "RUB"))
        assertEquals("33–34 ₽", perPersonText(100, 3, "RUB"))
    }
}
