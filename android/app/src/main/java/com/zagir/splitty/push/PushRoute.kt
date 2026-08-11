package com.zagir.splitty.push

import android.content.Intent

/**
 * Куда ведёт тап по push-уведомлению.
 *
 * Порт iOS `PushRoute` (ios/Splitty/Core/PushManager.swift): правила разбора
 * payload на двух клиентах обязаны совпадать буква в букву, поэтому и ключи, и
 * их приоритет живут здесь одним куском, а не растекаются по активити.
 *
 * Ключи шлёт бэкенд (internal/bot/notifier.go), в фоне их же кладёт в extras
 * сам FCM — поэтому одна и та же таблица разбирает и наш PendingIntent
 * (форграунд), и системный тап по уведомлению из трея.
 */
sealed interface PushRoute {
    /** Возврат долга и любой пуш без операции — открываем комнату. */
    data class Room(val roomId: String) : PushRoute

    /** Расход: в payload есть и комната, и операция — открываем карточку. */
    data class Operation(val roomId: String, val operationId: String) : PushRoute

    /**
     * Приглашение — раздел «Уведомления», а НЕ комната: у человека с ожидающим
     * приглашением доступа к ней ещё нет, и переход упёрся бы в «вы не участник».
     */
    data object Notifications : PushRoute

    companion object {
        const val KEY_ROOM_ID = "roomId"
        const val KEY_OPERATION_ID = "operationId"
        const val KEY_TYPE = "type"
        private const val TYPE_INVITE = "invite"

        /** Разбор данных пуша; null — вести некуда (пуш не про комнату). */
        fun from(roomId: String?, operationId: String?, type: String?): PushRoute? {
            // Приглашение сильнее всего остального: даже придя с operationId,
            // в комнату оно вести не должно.
            if (type == TYPE_INVITE) return Notifications
            if (roomId.isNullOrEmpty()) return null
            // Пустая строка = операции нет: payload FCM состоит только из строк,
            // и "operationId": "" уводило бы в карточку с пустым id.
            if (!operationId.isNullOrEmpty()) return Operation(roomId, operationId)
            return Room(roomId)
        }

        /** Payload FCM (форграунд, `onMessageReceived`). */
        fun fromData(data: Map<String, String>): PushRoute? =
            from(data[KEY_ROOM_ID], data[KEY_OPERATION_ID], data[KEY_TYPE])

        /**
         * Extras интента запуска: их кладём мы сами (форграунд) либо FCM,
         * когда уведомление рисовал системный трей.
         */
        fun fromIntent(intent: Intent?): PushRoute? {
            val extras = intent?.extras ?: return null
            return from(
                extras.getString(KEY_ROOM_ID),
                extras.getString(KEY_OPERATION_ID),
                extras.getString(KEY_TYPE),
            )
        }
    }
}
