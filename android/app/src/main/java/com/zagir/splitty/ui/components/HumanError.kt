package com.zagir.splitty.ui.components

import com.zagir.splitty.core.network.ApiException
import java.io.IOException
import java.io.InterruptedIOException
import java.net.SocketTimeoutException

// Порт ios/Splitty/Core/Components.swift → humanErrorText.
// Переводит сетевой/системный сбой в понятный текст: сырой message OkHttp/
// исключений в алертах пугает жаргоном. Таймаут — ОТДЕЛЬНАЯ ветка: «сервер
// долго не отвечает» вместо «нет сети» (parse-запрос с таймаутом 90с — частый
// случай, где важно не путать «нет интернета» и «ждём сервер»).

/** Человекочитаемый русский текст ошибки для алертов/failed-состояний. */
fun humanErrorText(error: Throwable): String {
    // Таймаут ловим до общей transport-ветки — в CODE_TRANSPORT он теряется,
    // но причина (IOException) проброшена в cause при построении ApiException.
    if (isTimeout(error) || isTimeout(error.cause)) {
        return "Сервер долго не отвечает. Попробуйте ещё раз"
    }
    (error as? ApiException)?.let { api ->
        return when (api.code) {
            ApiException.CODE_TRANSPORT ->
                "Нет соединения с интернетом. Проверьте сеть и попробуйте ещё раз"
            ApiException.CODE_DECODING ->
                "Не удалось обработать ответ сервера. Попробуйте ещё раз"
            // Серверный (тело error.message) и локальные тексты уже человеческие.
            else -> api.message
        }
    }
    if (error is IOException) {
        return "Нет соединения с интернетом. Проверьте сеть и попробуйте ещё раз"
    }
    return error.message?.takeIf { it.isNotBlank() }
        ?: "Что-то пошло не так. Попробуйте ещё раз"
}

/**
 * Текст ошибки привязки/отвязки способа входа (порт iOS `identityErrorText`).
 *
 * Коды сервера (`identity_taken`, `last_identity`) пользователю не показываем:
 * ему нужно объяснение и следующий шаг, а не идентификатор ошибки. Свой текст,
 * а не серверный `message`, потому что «что делать дальше» зависит от экрана:
 * здесь это «войдите через тот профиль» и «сначала привяжите другой способ».
 */
fun identityErrorText(error: Throwable): String {
    val api = error as? ApiException ?: return humanErrorText(error)
    return when {
        api.code == "identity_taken" ->
            "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него"
        api.code == "last_identity" ->
            "Нельзя отвязать единственный способ входа. Сначала привяжите другой"
        // 401 здесь — не протухшая сессия (её обрабатывает AuthInterceptor),
        // а отказ провайдера: подпись/срок/aud id-токена не сошлись.
        api.isUnauthorized -> "Не удалось подтвердить аккаунт. Попробуйте ещё раз"
        else -> humanErrorText(error)
    }
}

/** Отличает таймаут от прочих сетевых сбоев (SocketTimeout / прерванный I/O). */
private fun isTimeout(error: Throwable?): Boolean = when (error) {
    is SocketTimeoutException -> true
    is InterruptedIOException -> error.message?.contains("timeout", ignoreCase = true) == true
    else -> false
}
