package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// Чистые JVM-тесты подсказок чек-строки (ReceiptCard) — зеркало iOS shareHint/
// perPersonText/isEven. Логика без Android-зависимостей.
class ReceiptHintTest {

    private fun item(
        name: String = "Позиция",
        price: Int,
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

    private fun share(id: Long, weight: Int = 1, amount: Int? = null) = ItemShare(id, weight, amount)

    @Test fun singleShare_wholeItem() {
        assertEquals("целиком", shareHint(item(price = 500, shares = listOf(share(1))), "RUB"))
    }

    @Test fun evenSplit_exact() {
        val it = item(price = 800, shares = listOf(share(1), share(2)))
        assertEquals("по 400 ₽ × 2", shareHint(it, "RUB"))
    }

    @Test fun evenSplit_range_whenNotDivisible() {
        // 100 на троих: 33–34, честный диапазон (иначе «по 33 × 3» не сходится с 100).
        val it = item(price = 100, shares = listOf(share(1), share(2), share(3)))
        assertEquals("по 33–34 ₽ × 3", shareHint(it, "RUB"))
    }

    @Test fun pricelessWithShares() {
        assertEquals("укажите цену", shareHint(item(price = 0, shares = listOf(share(1))), "RUB"))
    }

    @Test fun pricelessNoShares() {
        assertEquals("", shareHint(item(price = 0, shares = emptyList()), "RUB"))
    }

    @Test fun unknownName() {
        val it = item(price = 500, shares = listOf(share(1)), unknown = listOf("Саня"))
        assertEquals("кто это — выберите", shareHint(it, "RUB"))
    }

    @Test fun exactAmounts_onlyFixed() {
        val it = item(price = 300, shares = listOf(share(1, amount = 100), share(2, amount = 200)))
        assertEquals("точные суммы", shareHint(it, "RUB"))
    }

    @Test fun fixedPlusWeighted() {
        val it = item(price = 300, shares = listOf(share(1, amount = 100), share(2)))
        assertEquals("100 ₽ фиксом · остальное поровну", shareHint(it, "RUB"))
    }

    @Test fun weightedUneven_perUnit() {
        // Вова съел вдвое больше: 3 доли, 90 / 3 = 30 за штуку.
        val it = item(price = 90, shares = listOf(share(1, weight = 1), share(2, weight = 2)))
        assertEquals("3 шт · 30 ₽ за шт", shareHint(it, "RUB"))
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
        assertEquals("50 ₽", perPersonText(100, 2, "RUB"))
        assertEquals("33–34 ₽", perPersonText(100, 3, "RUB"))
    }
}
