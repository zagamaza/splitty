package com.zagir.splitty.core.model

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * PUT-passthrough позиций чека: [OperationBody.of] проносит items
 * НЕТРОНУТЫМИ, а SplittyJson (explicitNulls = false) не сериализует их
 * у обычных расходов. Иначе плоский PUT затёр бы чек itemized-операции.
 */
class OperationBodyItemsTest {

    private val items = listOf(
        OperationItem(
            name = "Пицца",
            price = 1200,
            qty = 1,
            shares = listOf(ItemShare(userId = 1, weight = 1), ItemShare(userId = 2, amount = 400)),
        ),
        OperationItem(name = "Сбор", price = 120, kind = OperationItem.KIND_SURCHARGE, split = OperationItem.SPLIT_PROPORTIONAL, percent = 10),
    )

    @Test
    fun `of carries items untouched into body`() {
        val body = OperationBody.of(
            description = "Ужин",
            sum = 1320,
            donorId = 1L,
            split = ExpenseSplit.ByExactAmount(
                listOf(RecipientSum(1L, 660), RecipientSum(2L, 660)),
            ),
            items = items,
        )
        assertEquals(items, body.items)
    }

    @Test
    fun `serialized itemized body round-trips items`() {
        val body = OperationBody.of(
            description = "Ужин",
            sum = 1320,
            donorId = 1L,
            split = ExpenseSplit.Equally(listOf(1L, 2L)),
            items = items,
        )
        val json = SplittyJson.encodeToString(OperationBody.serializer(), body)
        assertTrue(json.contains("\"items\""))
        val decoded = SplittyJson.decodeFromString(OperationBody.serializer(), json)
        assertEquals(items, decoded.items)
    }

    @Test
    fun `ordinary body omits items field entirely`() {
        val body = OperationBody.of(
            description = "Такси",
            sum = 300,
            donorId = 1L,
            split = ExpenseSplit.Equally(listOf(1L)),
        )
        val json = SplittyJson.encodeToString(OperationBody.serializer(), body)
        assertFalse(json.contains("\"items\""))
    }
}
