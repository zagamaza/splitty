package com.zagir.splitty.core.model

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

/**
 * Инвариант приёмки (Task 15): itemized-операции iOS↔Android взаимно не портятся.
 *
 * Проверяется полная петля на РЕАЛЬНОЙ форме серверного ответа (та же фикстура,
 * что в iOS-тестах): сервер → декод → тело PUT → энкод → декод. Точечные тесты
 * рядом проверяют куски петли (ModelsDecodingTest — декод, OperationBodyItemsTest —
 * passthrough), но именно склейка ловит регрессию вида «поле декодится, но теряется
 * при сборке тела» — а это и есть затирание чека, ради которого делался Task 1.
 *
 * Второй инвариант: клиентский [derivedShares] обязан сойтись с плоскими
 * `recipients`, которые сервер посчитал по тем же позициям. Разойдутся — в форме
 * будут одни суммы, а в группе появятся другие.
 */
class ItemizedCrossClientTest {

    /** Ответ сервера по itemized-операции; recipients здесь — результат серверного DeriveShares. */
    private val serverPayload = """
        {
          "id": "65ai",
          "description": "Ужин по чеку",
          "sum": 1320,
          "splitType": "by_exact_amount",
          "donor": {"id": 1, "displayName": "Загир"},
          "recipients": [
            {"user": {"id": 1, "displayName": "Загир"}, "sum": 660},
            {"user": {"id": 2, "displayName": "Алмаз"}, "sum": 660}
          ],
          "createdAt": "2026-07-05T12:00:00Z",
          "items": [
            {
              "name": "Пицца", "price": 1200, "qty": 1, "kind": "item",
              "shares": [
                {"userId": 1, "weight": 1},
                {"userId": 2, "weight": 1}
              ]
            },
            {
              "name": "Сервисный сбор", "price": 120, "qty": 1,
              "kind": "surcharge", "split": "proportional", "percent": 10
            }
          ]
        }
    """.trimIndent()

    @Test
    fun `items survive full server to PUT body loop untouched`() {
        val operation = SplittyJson.decodeFromString<Operation>(serverPayload)
        val original = operation.items
        assertNotNull(original)

        // Ровно то, что делает SplittyRepository.updateOperation: плоские поля
        // пересобираются, items проносятся оригинальными.
        val body = OperationBody.of(
            description = operation.description,
            sum = operation.sum,
            donorId = 1L,
            split = ExpenseSplit.ByExactAmount(listOf(RecipientSum(1L, 660), RecipientSum(2L, 660))),
            items = original,
        )
        val wire = SplittyJson.encodeToString(OperationBody.serializer(), body)
        val reread = SplittyJson.decodeFromString(OperationBody.serializer(), wire).items

        assertEquals(original, reread, "правка плоских полей не должна менять чек")
    }

    @Test
    fun `client derived shares match flat recipients written by server`() {
        val operation = SplittyJson.decodeFromString<Operation>(serverPayload)
        val derived = operation.items!!.derivedShares()
        assertNotNull(derived, "фикстура сервера обязана считаться клиентом")

        assertEquals(operation.sum, derived.total)
        val serverShares = operation.recipients.associate { it.user.id to it.sum }
        assertEquals(serverShares, derived.shares)
    }

    @Test
    fun `ordinary operation stays flat - no items leak into PUT`() {
        val body = OperationBody.of(
            description = "Такси",
            sum = 300,
            donorId = 1L,
            split = ExpenseSplit.Equally(listOf(1L, 2L)),
            items = null,
        )
        val wire = SplittyJson.encodeToString(OperationBody.serializer(), body)
        assertTrue("\"items\"" !in wire, "у плоского расхода поля items быть не должно")
    }
}
