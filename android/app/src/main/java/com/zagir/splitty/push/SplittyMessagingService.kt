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
        val channelId = message.data["channel"]?.takeIf { it == CHANNEL_DEBTS } ?: CHANNEL_OPERATIONS
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

        /** Создаёт каналы уведомлений (идемпотентно). */
        fun ensureChannels(context: Context) {
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL_OPERATIONS, "Расходы", NotificationManager.IMPORTANCE_DEFAULT).apply {
                    description = "Добавление и изменение расходов в ваших тусах"
                },
            )
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL_DEBTS, "Долги", NotificationManager.IMPORTANCE_DEFAULT).apply {
                    description = "Возвраты долгов"
                },
            )
        }
    }
}
