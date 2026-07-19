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

    /**
     * Регрессия: kotlinx по умолчанию НЕ сериализует значения-дефолты, поэтому
     * `weight = 1` выпадал из тела. Сервер видел долю без веса и либо отвечал
     * 400 («цена позиции не распределена полностью») на равном делении, либо при
     * смешанных весах молча считал деление иначе, чем показал предпросмотр.
     *
     * Проверка идёт по СЫРОМУ JSON: round-trip через тот же сериализатор
     * восстановил бы дефолт и пропустил дефект.
     */
    @Test
    fun `default weight is present on the wire`() {
        val body = OperationBody.of(
            description = "Ужин",
            sum = 1200,
            donorId = 1L,
            split = ExpenseSplit.ByExactAmount(listOf(RecipientSum(1L, 600), RecipientSum(2L, 600))),
            items = listOf(
                OperationItem(
                    name = "Пицца",
                    price = 1200,
                    shares = listOf(ItemShare(userId = 1), ItemShare(userId = 2)),
                ),
            ),
        )
        val wire = SplittyJson.encodeToString(OperationBody.serializer(), body)
        assertTrue("\"weight\":1" in wire, "weight=1 не ушёл на провод: $wire")
    }
}
