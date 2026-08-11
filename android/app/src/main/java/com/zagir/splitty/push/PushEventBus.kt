package com.zagir.splitty.push

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Намерение перейти по тапу: маршрут плюс id того, кто был в аккаунте в момент
 * тапа. [ownerId] null — сессия ещё не прочитана (холодный старт по тапу это
 * норма) либо человек не вошёл.
 */
data class PendingPushRoute(val route: PushRoute, val ownerId: Long?)

/**
 * Мостик от FCM и активити до корневого экрана: тап по уведомлению и приход
 * пуша в открытое приложение. Аналог `TelegramAuthBus` и iOS-нотификаций
 * `.splittyPushTapped` / `.splittyPushReceived`.
 *
 * Синглтон и в памяти, без DataStore — в отличие от приглашения по ссылке.
 * Тап это живой жест: уведомление в момент тапа гасится (`setAutoCancel`), и
 * пережившее перезапуск процесса намерение утащило бы человека в комнату при
 * следующем обычном запуске приложения, без всякого тапа.
 */
@Singleton
class PushEventBus @Inject constructor() {

    private val _pendingRoute = MutableStateFlow<PendingPushRoute?>(null)

    /**
     * Куда просили перейти; null — переходить некуда.
     *
     * StateFlow, а не событие: тап случается РАНЬШЕ, чем собран корневой экран
     * (холодный старт), и раньше входа, если сессия успела протухнуть. Значение
     * ждёт своего исполнителя и гасится им же — [consumeRoute].
     */
    val pendingRoute: StateFlow<PendingPushRoute?> = _pendingRoute.asStateFlow()

    fun postRoute(route: PushRoute, ownerId: Long?) {
        _pendingRoute.value = PendingPushRoute(route, ownerId)
    }

    /** Намерение исполнено (или адресовано не этому аккаунту) — забываем. */
    fun consumeRoute() {
        _pendingRoute.value = null
    }

    private val _received = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

    /**
     * Пуш пришёл в ОТКРЫТОЕ приложение. Никуда не ведёт — только повод
     * перечитать счётчик непрочитанного: `ON_START` до следующего сворачивания
     * уже не сработает, и бейдж на колоколе оставался бы вчерашним, пока
     * человек смотрит на баннер о новом расходе (порт iOS `willPresent`).
     */
    val received: SharedFlow<Unit> = _received.asSharedFlow()

    fun noteReceived() {
        _received.tryEmit(Unit)
    }
}
