package com.zagir.splitty.push

import android.util.Log
import com.google.firebase.messaging.FirebaseMessaging
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.di.ApplicationScope
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Регистрирует FCM-токен устройства на бэкенде надёжно (не fire-and-forget):
 * - логин → регистрируем текущий токен;
 * - ротация токена (onNewToken) → перерегистрируем;
 * - неуспех → сбрасываем метку и повторяем при следующем логине/старте
 *   приложения (start() зовётся из SplittyApp.onCreate).
 *
 * lastRegistered гасит лишние сетевые вызовы (токен FCM меняется редко). Ошибки
 * не роняют вход/старт — доставка пушей best-effort.
 *
 * `open` (класс и [unregisterCurrent]) — шов для тестов экрана «Профиль»:
 * настоящая отвязка идёт через `FirebaseMessaging.getInstance()`, который в
 * JVM-тестах не поднимается и молча падает. Без подменяемой отвязки проверка
 * «повтор удаления НЕ ходит в `DELETE /me/devices`» проходила бы и на сломанном
 * коде — запроса там нет по совершенно другой причине.
 */
@Singleton
open class PushTokenRegistrar @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    @ApplicationScope private val scope: CoroutineScope,
) {
    @Volatile
    private var lastRegistered: String? = null

    /** Наблюдение за сессией на всё время жизни приложения. */
    fun start() {
        scope.launch {
            sessionStore.state
                .map { it?.isAuthenticated == true }
                .distinctUntilChanged()
                .collect { loggedIn ->
                    if (loggedIn) registerCurrentToken() else lastRegistered = null
                }
        }
    }

    /** Ротация FCM-токена (SplittyMessagingService.onNewToken). */
    fun onTokenRefreshed(token: String) {
        lastRegistered = null
        if (sessionStore.currentToken() != null) {
            scope.launch { send(token) }
        }
    }

    /** Отвязать токен ПЕРЕД logout (пока JWT ещё валиден). */
    open fun unregisterCurrent() {
        FirebaseMessaging.getInstance().token.addOnSuccessListener { token ->
            lastRegistered = null
            if (!token.isNullOrEmpty()) {
                scope.launch { runCatching { repository.unregisterDevice(token) } }
            }
        }
    }

    private fun registerCurrentToken() {
        FirebaseMessaging.getInstance().token.addOnSuccessListener { token ->
            if (!token.isNullOrEmpty()) {
                scope.launch { send(token) }
            }
        }
    }

    private suspend fun send(token: String) {
        if (token == lastRegistered) return
        runCatching { repository.registerDevice(token) }
            .onSuccess { lastRegistered = token }
            .onFailure { Log.w(TAG, "register device failed — retry on next login/start", it) }
    }

    private companion object {
        const val TAG = "PushToken"
    }
}
