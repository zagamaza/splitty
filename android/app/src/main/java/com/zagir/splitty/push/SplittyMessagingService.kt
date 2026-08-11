package com.zagir.splitty.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import com.zagir.splitty.MainActivity
import com.zagir.splitty.R
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/**
 * Приём FCM-пушей. Бэкенд шлёт notification+data: в фоне системный трей рисует
 * сам (канал — из manifest meta default_notification_channel_id), в форграунде —
 * onMessageReceived, тогда показываем баннер вручную (тем же каналом из data).
 *
 * onNewToken — ротация FCM-токена: перерегистрируем на бэкенде.
 */
@AndroidEntryPoint
class SplittyMessagingService : FirebaseMessagingService() {

    @Inject
    lateinit var registrar: PushTokenRegistrar

    override fun onNewToken(token: String) {
        registrar.onTokenRefreshed(token)
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val channelId = channelIdFor(message.data["channel"])
        val title = message.notification?.title ?: message.data["title"] ?: getString(R.string.app_name)
        val body = message.notification?.body ?: message.data["body"] ?: return

        ensureChannels(this)

        val intent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            // Deeplink-данные — на будущее (открытие комнаты/операции по тапу).
            message.data["roomId"]?.let { putExtra("roomId", it) }
            message.data["operationId"]?.let { putExtra("operationId", it) }
        }
        val pending = PendingIntent.getActivity(
            this, 0, intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val notification = NotificationCompat.Builder(this, channelId)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setAutoCancel(true)
            .setContentIntent(pending)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        // ID по комнате: пуши одной тусы схлопываются в одну строку, не спамят.
        val id = message.data["roomId"]?.hashCode() ?: System.identityHashCode(message)
        nm.notify(id, notification)
    }

    companion object {
        const val CHANNEL_OPERATIONS = "operations"
        const val CHANNEL_DEBTS = "debts"
        const val CHANNEL_INVITES = "invites"

        /**
         * Канал уведомления по значению `data["channel"]` от бэкенда.
         *
         * Важно: бэкенд кладёт это значение прямо в ChannelID уведомления
         * (internal/push/push.go), и на Android 8+ уведомление с НЕСУЩЕСТВУЮЩИМ
         * каналом просто не показывается — молча. Поэтому список каналов здесь
         * обязан покрывать все значения, которые шлёт сервер, а неизвестное
         * значение уходит в operations, а не теряется.
         */
        fun channelIdFor(channel: String?): String = when (channel) {
            CHANNEL_DEBTS -> CHANNEL_DEBTS
            CHANNEL_INVITES -> CHANNEL_INVITES
            else -> CHANNEL_OPERATIONS
        }

        /** Создаёт каналы уведомлений (идемпотентно). */
        fun ensureChannels(context: Context) {
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            fun channel(id: String, nameRes: Int, descRes: Int) = nm.createNotificationChannel(
                NotificationChannel(
                    id,
                    context.getString(nameRes),
                    NotificationManager.IMPORTANCE_DEFAULT,
                ).apply { description = context.getString(descRes) },
            )
            channel(CHANNEL_OPERATIONS, R.string.push_channel_operations, R.string.push_channel_operations_desc)
            channel(CHANNEL_DEBTS, R.string.push_channel_debts, R.string.push_channel_debts_desc)
            channel(CHANNEL_INVITES, R.string.push_channel_invites, R.string.push_channel_invites_desc)
        }
    }
}
