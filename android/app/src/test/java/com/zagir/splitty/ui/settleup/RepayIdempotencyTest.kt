package com.zagir.splitty.ui.settleup

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals

/**
 * Ключ идемпотентности погашения.
 *
 * Ошибиться дорого в обе стороны: постоянный ключ теряет правку суммы,
 * меняющийся — списывает деньги дважды.
 */
class RepayIdempotencyTest {

    @Test
    fun `retry of the same attempt reuses the key`() {
        val keeper = RepayIdempotency()
        val first = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        val retry = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        // «Ошибка сети, жму ещё раз» обязано уйти с тем же ключом: сервер
        // отклоняет только возврат СВЕРХ долга, два частичных платежа пройдут оба.
        assertEquals(first, retry)
    }

    @Test
    fun `edited sum gets a new key`() {
        val keeper = RepayIdempotency()
        val first = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        val edited = keeper.key(debtorId = 1, lenderId = 2, sum = 500)
        // Иначе сервер вернёт прежнюю операцию на 400, и правка исчезнет молча.
        assertNotEquals(first, edited)
    }

    @Test
    fun `another debt gets a new key`() {
        val keeper = RepayIdempotency()
        val first = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        val other = keeper.key(debtorId = 3, lenderId = 2, sum = 400)
        assertNotEquals(first, other)
    }

    @Test
    fun `returning to the previous sum does not resurrect the old key`() {
        val keeper = RepayIdempotency()
        val first = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        keeper.key(debtorId = 1, lenderId = 2, sum = 500)
        val back = keeper.key(debtorId = 1, lenderId = 2, sum = 400)
        // Платёж на 500 мог уже уйти на сервер: вернувшись к 400, старый ключ
        // получил бы в ответ ту операцию вместо нового платежа.
        assertNotEquals(first, back)
    }
}
