package com.zagir.splitty.ui.expense

import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.data.OutboxPayload
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue

/**
 * Ключ идемпотентности создания расхода и политика «что переживает в очереди».
 *
 * Сервер мог записать расход и не успеть ответить. Повтор с НОВЫМ ключом заводил
 * бы второй такой же расход, а отказ вовсе — терял бы работу человека.
 */
class CreateIdempotencyTest {

    private fun payload(description: String = "Ужин", sum: Long = 1_200) =
        OutboxPayload.of(description, sum, donorId = 1L, split = ExpenseSplit.Equally(listOf(1L, 2L)))

    @Test
    fun `repeat with the same content keeps the key`() {
        val idempotency = CreateIdempotency()
        val first = idempotency.key(payload())
        val second = idempotency.key(payload())
        assertEquals(first, second, "повтор ушёл с новым ключом — сервер заведёт второй такой же расход")
    }

    @Test
    fun `changed content gets a new key`() {
        val idempotency = CreateIdempotency()
        val first = idempotency.key(payload(sum = 1_200))
        val second = idempotency.key(payload(sum = 1_500))
        assertNotEquals(first, second, "исправленная сумма ушла со старым ключом — вернётся первая операция")
    }

    @Test
    fun `server errors 5xx go to the queue, 4xx do not`() {
        assertTrue(ApiException(status = null, code = ApiException.CODE_TRANSPORT, message = "").deservesOutbox)
        assertTrue(ApiException(status = 500, code = "internal", message = "").deservesOutbox)
        assertTrue(ApiException(status = 502, code = "internal", message = "").deservesOutbox)
        assertFalse(
            ApiException(status = 400, code = "validation", message = "").deservesOutbox,
            "отказ по данным спрятался в очередь — человек его не увидит и не исправит",
        )
        assertFalse(ApiException(status = 409, code = "conflict", message = "").deservesOutbox)
    }
}
