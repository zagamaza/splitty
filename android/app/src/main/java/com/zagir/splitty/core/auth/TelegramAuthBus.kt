package com.zagir.splitty.core.auth

import com.zagir.splitty.core.model.TelegramLoginBody
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Мостик от `splitty://tg-callback` до экрана входа.
 *
 * Callback прилетает интентом в активити, а обменивать payload на сессию
 * должен `LoginViewModel` — напрямую активити до него не дотянется. Шина
 * живёт синглтоном, потому что активити при возврате из Custom Tabs может
 * быть пересоздана, и результат обязан пережить пересоздание.
 *
 * `replay = 1`: интент приходит РАНЬШЕ, чем экран входа успевает подписаться,
 * и без буфера первый же вход терялся бы молча.
 */
@Singleton
class TelegramAuthBus @Inject constructor() {
    private val _payloads = MutableSharedFlow<TelegramLoginBody>(replay = 1)
    val payloads: SharedFlow<TelegramLoginBody> = _payloads.asSharedFlow()

    fun post(payload: TelegramLoginBody) {
        _payloads.tryEmit(payload)
    }

    /** Payload израсходован — иначе он переиграется при следующей подписке. */
    fun consume() {
        _payloads.resetReplayCache()
    }
}
