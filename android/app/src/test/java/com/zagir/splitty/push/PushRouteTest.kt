package com.zagir.splitty.push

import android.content.Intent
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Разбор данных push-уведомления в маршрут перехода.
 *
 * Литералы ключей продублированы здесь намеренно: серверную сторону пинает
 * internal/bot/notifier_push_payload_test.go, iOS — PushRouteTests.swift, а
 * общего кода у трёх сторон нет. Расхождение обязано валить тест, а не молча
 * ломать переход по пушу — ровно так `operationId` и пролежал год
 * невостребованным.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PushRouteTest {

    @Test
    fun `expense push leads to the operation card`() {
        assertEquals(
            PushRoute.Operation("68f2a1c4d9", "6a7b339f63a4ee45a2aed6db"),
            PushRoute.fromData(
                mapOf(
                    "channel" to "operations",
                    "roomId" to "68f2a1c4d9",
                    "operationId" to "6a7b339f63a4ee45a2aed6db",
                    "type" to "operation",
                ),
            ),
        )
    }

    @Test
    fun `debt push leads to the room`() {
        assertEquals(
            PushRoute.Room("room-1"),
            PushRoute.fromData(mapOf("channel" to "debts", "roomId" to "room-1", "type" to "debt")),
        )
    }

    /**
     * Пустая строка — не операция: payload FCM состоит только из строк, и
     * «operationId»: "" уводило бы в карточку с пустым id.
     */
    @Test
    fun `empty operationId falls back to the room`() {
        assertEquals(
            PushRoute.Room("room-1"),
            PushRoute.fromData(mapOf("roomId" to "room-1", "operationId" to "")),
        )
    }

    /**
     * Приглашение ведёт в раздел, а НЕ в комнату: доступа к ней у приглашённого
     * ещё нет, и переход упёрся бы в «вы не участник этой комнаты».
     */
    @Test
    fun `invite leads to notifications even with room and operation`() {
        assertEquals(
            PushRoute.Notifications,
            PushRoute.fromData(mapOf("roomId" to "room-1", "type" to "invite")),
        )
        assertEquals(
            PushRoute.Notifications,
            PushRoute.fromData(
                mapOf("roomId" to "room-1", "operationId" to "op-1", "type" to "invite"),
            ),
        )
    }

    @Test
    fun `payload without a room leads nowhere`() {
        assertNull(PushRoute.fromData(emptyMap()))
        assertNull(PushRoute.fromData(mapOf("roomId" to "")))
        // Бэкенд шлёт camelCase; вернувшийся snake_case обязан быть заметен.
        assertNull(PushRoute.fromData(mapOf("room_id" to "room-1")))
    }

    /**
     * Тап по уведомлению приходит в активити ИНТЕНТОМ: extras кладём либо мы
     * сами (форграунд), либо FCM, когда уведомление рисовал системный трей.
     * До этого их не читал никто — тап просто открывал приложение.
     */
    @Test
    fun `intent extras reach the route`() {
        val intent = Intent()
            .putExtra("roomId", "room-1")
            .putExtra("operationId", "op-1")
            .putExtra("type", "operation")

        assertEquals(PushRoute.Operation("room-1", "op-1"), PushRoute.fromIntent(intent))
    }

    @Test
    fun `intent without push extras leads nowhere`() {
        assertNull(PushRoute.fromIntent(Intent()))
        assertNull(PushRoute.fromIntent(null))
        // Обычный запуск из лаунчера несёт свои extras, но не наши.
        assertNull(PushRoute.fromIntent(Intent().putExtra("someOtherKey", "x")))
    }
}
