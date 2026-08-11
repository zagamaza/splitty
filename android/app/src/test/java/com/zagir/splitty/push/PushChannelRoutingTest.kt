package com.zagir.splitty.push

import android.app.NotificationManager
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Маршрутизация `data["channel"]` от бэкенда в ChannelID уведомления.
 *
 * Дорого ошибиться: на Android 8+ уведомление с несуществующим каналом просто
 * не показывается — молча. Поэтому проверяется не только таблица маршрутов, но
 * и то, что каждый её результат реально СОЗДАЁТСЯ: раздельные списки в
 * `channelIdFor` и `ensureChannels` расходились бы при зелёных тестах.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34]) // NotificationManager нужен настоящий, каналы живут в нём
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

    @Test
    fun `every id routing can return is actually created`() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        SplittyMessagingService.ensureChannels(context)

        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val created = manager.notificationChannels.map { it.id }.toSet()
        // Все значения, которые шлёт бэкенд, плюс незнакомое и отсутствующее:
        // куда бы маршрут ни привёл, канал обязан существовать.
        val routed = listOf("operations", "debts", "invites", "brand_new", null)
            .map { SplittyMessagingService.channelIdFor(it) }
            .toSet()

        val missing = routed - created
        assertTrue(missing.isEmpty(), "каналы не созданы: $missing")
    }
}
