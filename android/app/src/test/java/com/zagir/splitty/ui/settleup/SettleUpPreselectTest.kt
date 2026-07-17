package com.zagir.splitty.ui.settleup

import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.User
import kotlin.test.assertEquals
import kotlin.test.assertNull
import org.junit.Test

/**
 * Предвыбор долга по nav-аргументам (переход «Погасить» из строки балансов):
 * matchPreselect находит ровно тот долг «должник→кредитор», игнорирует
 * отсутствующие (-1) аргументы и несовпадения (тогда работает обычный список).
 */
class SettleUpPreselectTest {

    private fun user(id: Long) = User(id = id, username = null, displayName = "u$id")

    private val debts = listOf(
        Debt(debtor = user(1), lender = user(2), sum = 500),
        Debt(debtor = user(3), lender = user(2), sum = 300),
    )

    @Test
    fun `matches exact debtor and lender pair`() {
        val match = matchPreselect(debts, debtorId = 3, lenderId = 2)
        assertEquals(300, match?.sum)
        assertEquals(3L, match?.debtor?.id)
    }

    @Test
    fun `no argument (negative ids) yields null`() {
        assertNull(matchPreselect(debts, debtorId = -1, lenderId = -1))
    }

    @Test
    fun `reversed direction does not match`() {
        // 2 должен 1 — такого долга нет (есть только 1→2).
        assertNull(matchPreselect(debts, debtorId = 2, lenderId = 1))
    }

    @Test
    fun `unknown users yield null`() {
        assertNull(matchPreselect(debts, debtorId = 99, lenderId = 42))
    }
}
