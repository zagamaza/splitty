package com.zagir.splitty.core.model

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Декодирование образцов JSON из docs/API.md (раздел DTO) — контрактный тест:
 * незнакомые поля игнорируются (ignoreUnknownKeys), опциональные поля могут
 * отсутствовать, даты RFC3339.
 */
class ModelsDecodingTest {

    @Test
    fun `decodes User and ignores unknown keys`() {
        val user = SplittyJson.decodeFromString<User>(
            """{"id": 123, "username": "zagir", "displayName": "Загир", "unknownField": 42}"""
        )
        assertEquals(123L, user.id)
        assertEquals("zagir", user.username)
        assertEquals("Загир", user.displayName)
    }

    @Test
    fun `decodes User without optional username`() {
        val user = SplittyJson.decodeFromString<User>(
            """{"id": 7, "displayName": "Алмаз"}"""
        )
        assertNull(user.username)
    }

    @Test
    fun `decodes Me`() {
        val me = SplittyJson.decodeFromString<Me>(
            """{"id": 123, "username": "zagir", "displayName": "Загир", "lang": "ru", "notificationOn": true}"""
        )
        assertEquals(123L, me.id)
        assertEquals("ru", me.lang)
        assertTrue(me.notificationOn)
    }

    @Test
    fun `decodes AuthResponse`() {
        val auth = SplittyJson.decodeFromString<AuthResponse>(
            """{"token": "eyJhbGciOi...", "user": {"id": 1, "displayName": "Dev"}}"""
        )
        assertEquals("eyJhbGciOi...", auth.token)
        assertEquals(1L, auth.user.id)
    }

    @Test
    fun `decodes Operation with recipients splitType and files`() {
        val operation = SplittyJson.decodeFromString<Operation>(
            """
            {
              "id": "65a0f",
              "description": "Ужин",
              "sum": 1200,
              "isDebtRepayment": false,
              "splitType": "equally",
              "donor": {"id": 123, "displayName": "Загир"},
              "recipients": [
                {"user": {"id": 123, "displayName": "Загир"}, "sum": 600},
                {"user": {"id": 456, "displayName": "Алмаз"}, "sum": 600}
              ],
              "createdAt": "2026-07-05T12:00:00Z",
              "files": [{"type": "image", "fileId": "AgAC123"}]
            }
            """.trimIndent()
        )
        assertEquals("65a0f", operation.id)
        assertEquals(1200, operation.sum)
        assertFalse(operation.isDebtRepayment)
        assertEquals(SplitType.EQUALLY, operation.splitType)
        assertEquals(2, operation.recipients.size)
        assertEquals(600, operation.recipientSum(456))
        assertEquals(Instant.parse("2026-07-05T12:00:00Z"), operation.createdAt)
        assertTrue(operation.hasFiles)
        assertEquals("AgAC123", operation.files?.first()?.fileId)
        // Нетто-позиции по хранимым долям: донор одолжил sum − своя доля.
        assertEquals(600, operation.netPosition(123))
        assertEquals(-600, operation.netPosition(456))
        assertNull(operation.netPosition(999))
    }

    @Test
    fun `decodes itemized operation with items and shares`() {
        // Фикстура зеркалит серверный read-path (operationItemDto): позиция с
        // долями по весам + надбавка «surcharge»/«proportional» с percent.
        val operation = SplittyJson.decodeFromString<Operation>(
            """
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
                    {"userId": 2, "weight": 1, "amount": 400}
                  ]
                },
                {
                  "name": "Сервисный сбор", "price": 120, "qty": 1,
                  "kind": "surcharge", "split": "proportional", "percent": 10
                }
              ]
            }
            """.trimIndent()
        )
        val items = operation.items
        assertEquals(2, items?.size)
        val pizza = items!!.first()
        assertEquals("Пицца", pizza.name)
        assertEquals(1200, pizza.price)
        assertEquals(1, pizza.qty)
        assertEquals(OperationItem.KIND_ITEM, pizza.kind)
        assertEquals(2, pizza.shares?.size)
        assertEquals(1L, pizza.shares!![0].userId)
        assertNull(pizza.shares!![0].amount)
        assertEquals(400, pizza.shares!![1].amount)
        val surcharge = items[1]
        assertEquals(OperationItem.KIND_SURCHARGE, surcharge.kind)
        assertEquals(OperationItem.SPLIT_PROPORTIONAL, surcharge.split)
        assertEquals(10, surcharge.percent)
        assertNull(surcharge.shares)
    }

    @Test
    fun `decodes ordinary operation without items - items stays null`() {
        val operation = SplittyJson.decodeFromString<Operation>(
            """
            {
              "id": "65a3", "description": "Такси", "sum": 300,
              "donor": {"id": 1, "displayName": "A"},
              "recipients": [{"user": {"id": 1, "displayName": "A"}, "sum": 300}],
              "createdAt": "2026-07-05T12:00:00Z"
            }
            """.trimIndent()
        )
        assertNull(operation.items)
    }

    @Test
    fun `decodes repayment operation without splitType and files`() {
        val operation = SplittyJson.decodeFromString<Operation>(
            """
            {
              "id": "65a1", "description": "Возврат", "sum": 500,
              "isDebtRepayment": true,
              "donor": {"id": 456, "displayName": "Алмаз"},
              "recipients": [{"user": {"id": 123, "displayName": "Загир"}, "sum": 500}],
              "createdAt": "2026-07-05T12:00:00+03:00"
            }
            """.trimIndent()
        )
        assertTrue(operation.isDebtRepayment)
        assertNull(operation.splitType)
        assertFalse(operation.hasFiles)
        assertEquals(Instant.parse("2026-07-05T09:00:00Z"), operation.createdAt)
    }

    @Test
    fun `unknown splitType falls back to equally`() {
        val operation = SplittyJson.decodeFromString<Operation>(
            """
            {
              "id": "65a2", "description": "X", "sum": 10,
              "splitType": "by_share_of_future",
              "donor": {"id": 1, "displayName": "A"},
              "recipients": [],
              "createdAt": "2026-07-05T12:00:00Z"
            }
            """.trimIndent()
        )
        assertEquals(SplitType.EQUALLY, operation.splitType)
    }

    @Test
    fun `decodes RoomSummary`() {
        val room = SplittyJson.decodeFromString<RoomSummary>(
            """
            {
              "id": "65af", "name": "Сплит по Стамбулу", "createdAt": "2026-07-05T12:00:00Z",
              "isArchived": false,
              "currency": "RUB",
              "members": [{"id": 1, "displayName": "A"}], "memberCount": 4,
              "totalSpent": 34000,
              "myBalance": 500
            }
            """.trimIndent()
        )
        assertEquals("Сплит по Стамбулу", room.name)
        assertEquals("RUB", room.currency)
        assertEquals(4, room.memberCount)
        assertEquals(34_000, room.totalSpent)
        assertEquals(500, room.myBalance)
    }

    @Test
    fun `decodes RoomDetail with debts and operations`() {
        val room = SplittyJson.decodeFromString<RoomDetail>(
            """
            {
              "id": "65af", "name": "Поездка", "createdAt": "2026-07-05T12:00:00Z",
              "isArchived": false,
              "currency": "USD",
              "members": [{"id": 1, "displayName": "A"}, {"id": 2, "displayName": "B"}],
              "totalSpent": 34000, "mySpent": 8500,
              "myBalance": -500,
              "debts": [
                {"debtor": {"id": 1, "displayName": "A"}, "lender": {"id": 2, "displayName": "B"}, "sum": 500}
              ],
              "operations": []
            }
            """.trimIndent()
        )
        assertEquals("USD", room.currency)
        assertEquals(8_500, room.mySpent)
        assertEquals(-500, room.myBalance)
        assertEquals(1, room.debts.size)
        assertEquals(500, room.debts.first().sum)
        assertEquals(1L, room.debts.first().debtor.id)
    }

    @Test
    fun `decodes FriendBalance with totalsByCurrency and room currencies`() {
        val friend = SplittyJson.decodeFromString<FriendBalance>(
            """
            {
              "user": {"id": 456, "displayName": "Алмаз"},
              "totalsByCurrency": [{"currency": "RUB", "sum": 500}, {"currency": "USD", "sum": -1100}],
              "rooms": [{"roomId": "65af", "roomName": "Поездка", "currency": "RUB", "balance": 500}]
            }
            """.trimIndent()
        )
        assertEquals(456L, friend.user.id)
        assertEquals(2, friend.totalsByCurrency.size)
        assertEquals("RUB", friend.rooms.first().currency)
        // totals — агрегированные: по убыванию |суммы|.
        assertEquals(
            listOf(CurrencySum("USD", -1100), CurrencySum("RUB", 500)),
            friend.totals,
        )
    }

    @Test
    fun `decodes ActivityItem with roomCurrency`() {
        val item = SplittyJson.decodeFromString<ActivityItem>(
            """
            {
              "roomId": "65af", "roomName": "Поездка", "roomCurrency": "EUR",
              "operation": {
                "id": "65a3", "description": "Кофе", "sum": 12,
                "donor": {"id": 1, "displayName": "A"},
                "recipients": [],
                "createdAt": "2026-07-05T12:00:00Z"
              }
            }
            """.trimIndent()
        )
        assertEquals("EUR", item.roomCurrency)
        assertEquals("Кофе", item.operation.description)
    }

    @Test
    fun `decodes Statistics`() {
        val stats = SplittyJson.decodeFromString<Statistics>(
            """
            {
              "currency": "RUB",
              "totalSpent": 9400,
              "operationCount": 12,
              "monthSpent": 9400,
              "byDay": [{"date": "2026-07-05", "sum": 9400}],
              "byMonth": [
                {"month": "2026-02", "sum": 0},
                {"month": "2026-03", "sum": 0},
                {"month": "2026-04", "sum": 0},
                {"month": "2026-05", "sum": 0},
                {"month": "2026-06", "sum": 0},
                {"month": "2026-07", "sum": 9400}
              ],
              "paidByMember": [{"user": {"id": 1, "displayName": "A"}, "sum": 4500}],
              "shareByMember": [{"user": {"id": 1, "displayName": "A"}, "sum": 3133}],
              "topOperations": [
                {"id": "65a4", "description": "Ужин", "sum": 3600,
                 "donor": {"id": 1, "displayName": "A"}, "createdAt": "2026-07-05T12:00:00Z"}
              ]
            }
            """.trimIndent()
        )
        assertEquals("RUB", stats.currency)
        assertEquals(9_400, stats.totalSpent)
        assertEquals(12, stats.operationCount)
        assertEquals("2026-07-05", stats.byDay.first().date)
        // Контракт byMonth: 6 календарных месяцев включая текущий, ascending, с нулями.
        assertEquals(6, stats.byMonth.size)
        assertEquals(MonthlySum("2026-02", 0), stats.byMonth.first())
        assertEquals(MonthlySum("2026-07", 9_400), stats.byMonth.last())
        assertEquals(4_500, stats.paidByMember.first().sum)
        assertEquals(3_133, stats.shareByMember.first().sum)
        assertEquals("Ужин", stats.topOperations.first().description)
    }

    @Test
    fun `decodes Statistics from old server without byMonth and operationCount`() {
        val stats = SplittyJson.decodeFromString<Statistics>(
            """{"currency": "RUB", "totalSpent": 100, "monthSpent": 100}"""
        )
        assertEquals(0, stats.operationCount)
        assertTrue(stats.byMonth.isEmpty())
        assertTrue(stats.byDay.isEmpty())
    }

    @Test
    fun `decodes CurrencyInfo list`() {
        val currencies = SplittyJson.decodeFromString<List<CurrencyInfo>>(
            """[{"code": "RUB", "symbol": "₽", "flag": "🇷🇺"}, {"code": "USD", "symbol": "$", "flag": "🇺🇸"}]"""
        )
        assertEquals(2, currencies.size)
        assertEquals("₽", currencies.first().symbol)
    }

    @Test
    fun `decodes NotifySettings with all three categories`() {
        val settings = SplittyJson.decodeFromString<NotifySettings>(
            """
            {
              "operations": {"telegram": true, "push": true},
              "debts": {"telegram": false, "push": false},
              "invites": {"telegram": false, "push": true}
            }
            """.trimIndent()
        )
        assertTrue(settings.operations.push)
        assertFalse(settings.debts.telegram)
        assertFalse(settings.invites.telegram)
        assertTrue(settings.invites.push)
    }

    /**
     * PATCH /me/notifications частичный: категорию, которой нет в теле, сервер
     * оставляет как есть. Пропусти клиент `invites` — её тумблеры «немели» бы
     * так же, как это уже было с дефолтами ChannelPrefs.
     */
    @Test
    fun `NotifySettings sends every category in PATCH body`() {
        val body = SplittyJson.encodeToString(NotifySettings.serializer(), NotifySettings())
        assertTrue("operations" in body)
        assertTrue("debts" in body)
        assertTrue("invites" in body)
    }

    @Test
    fun `OperationBody serializes exactly one split mode`() {
        val equally = SplittyJson.encodeToString(
            OperationBody.serializer(),
            OperationBody.of("Ужин", 1200, 123, ExpenseSplit.Equally(listOf(123, 456))),
        )
        assertTrue("recipientIds" in equally)
        assertFalse("recipientSums" in equally)

        val exact = SplittyJson.encodeToString(
            OperationBody.serializer(),
            OperationBody.of(
                "Отель", 1200, 123,
                ExpenseSplit.ByExactAmount(
                    listOf(RecipientSum(123, 700), RecipientSum(456, 500))
                ),
            ),
        )
        assertTrue("recipientSums" in exact)
        assertFalse("recipientIds" in exact)
    }

    /**
     * Счёт в рупиях на несколько миллиардов — обычное дело, а сервер считает
     * суммы 64-битными. Пока клиентские поля были 32-битными, такой ответ либо
     * не разбирался вовсе, либо число молча заворачивалось в отрицательное:
     * человек видел, что должен минус два миллиарда.
     */
    @Test
    fun `decodes sums beyond 32 bits without distortion`() {
        val huge = 3_600_000_000L
        val operation = SplittyJson.decodeFromString<Operation>(
            """{"id":"op1","description":"Вилла на месяц","sum":$huge,
               "donor":{"id":1,"displayName":"Загир"},
               "recipients":[{"user":{"id":1,"displayName":"Загир"},"sum":$huge}],
               "createdAt":"2026-08-12T10:00:00Z"}"""
        )
        assertEquals(huge, operation.sum)
        assertEquals(huge, operation.recipients.single().sum)
        assertEquals(huge, operation.recipientSum(1L))

        val room = SplittyJson.decodeFromString<RoomSummary>(
            """{"id":"r1","name":"Бали","createdAt":"2026-08-12T10:00:00Z",
               "memberCount":3,"currency":"IDR","totalSpent":$huge,"myBalance":${-huge}}"""
        )
        assertEquals(huge, room.totalSpent)
        assertEquals(-huge, room.myBalance)
    }
}
