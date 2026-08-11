package com.zagir.splitty.push

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Маршрутизация `data["channel"]` от бэкенда в ChannelID уведомления.
 *
 * Дорого ошибиться: на Android 8+ уведомление с несуществующим каналом просто
 * не показывается — молча. Поэтому здесь проверяется и то, что все значения,
 * которые шлёт сервер (`internal/push/push.go`), имеют свой канал, и то, что
 * незнакомое значение уходит в operations, а не теряется.
 */
class PushChannelRoutingTest {

    @Test
    fun `known channels map to their own ids`() {
        assertEquals(
            SplittyMessagingService.CHANNEL_OPERATIONS,
            SplittyMessagingService.channelIdFor("operations"),
        )
        assertEquals(
            SplittyMessagingService.CHANNEL_DEBTS,
            SplittyMessagingService.channelIdFor("debts"),
        )
        // Приглашения — новый канал: без него пуш о приглашении в фоне не
        // показывался бы вовсе.
        assertEquals(
            SplittyMessagingService.CHANNEL_INVITES,
            SplittyMessagingService.channelIdFor("invites"),
        )
    }

    @Test
    fun `unknown and missing channel fall back to operations, not lost`() {
        assertEquals(
            SplittyMessagingService.CHANNEL_OPERATIONS,
            SplittyMessagingService.channelIdFor("something_new_from_backend"),
        )
        assertEquals(
            SplittyMessagingService.CHANNEL_OPERATIONS,
            SplittyMessagingService.channelIdFor(null),
        )
    }
}
