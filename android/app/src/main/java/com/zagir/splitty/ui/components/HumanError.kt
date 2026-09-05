package com.zagir.splitty.ui.components

import com.zagir.splitty.R
import com.zagir.splitty.core.auth.GoogleSignInException
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.ui.UiText
import java.io.IOException
import java.io.InterruptedIOException
import java.net.SocketTimeoutException

// Порт ios/Splitty/Core/Components.swift → humanErrorText.
// Переводит сетевой/системный сбой в понятный текст: сырой message OkHttp/
// исключений в алертах пугает жаргоном. Таймаут — ОТДЕЛЬНАЯ ветка: «сервер
// долго не отвечает» вместо «нет сети» (parse-запрос с таймаутом 90с — частый
// случай, где важно не путать «нет интернета» и «ждём сервер»).
//
// Возвращается UiText, а не String: маппер зовут из ViewModel, где Context
// недоступен, а сама строка должна собираться в текущей локали уже на экране.

/**
 * Человекочитаемый текст ошибки для алертов/failed-состояний.
 *
 * [isSelf] различает «выхожу сам» и «убираю другого» — отказ `has_operations`
 * приходит на оба действия, но объяснять надо разное: «уберите СЕБЯ из расходов»
 * человеку, который убирает соседа, не говорит ничего. Сервер и iOS
 * (`leaveErrorText(_:isSelf:)`) это различают с самого начала.
 */
fun humanErrorText(error: Throwable, isSelf: Boolean = true): UiText {
    // Таймаут ловим до общей transport-ветки — в CODE_TRANSPORT он теряется,
    // но причина (IOException) проброшена в cause при построении ApiException.
    if (isTimeout(error) || isTimeout(error.cause)) {
        return UiText.res(R.string.error_timeout)
    }
    // Провайдер входа уже собрал свой текст — общий маппер не должен его терять.
    (error as? GoogleSignInException)?.let { return it.uiText }
    (error as? ApiException)?.let { api ->
        return when (api.code) {
            ApiException.CODE_TRANSPORT -> UiText.res(R.string.error_no_internet)
            ApiException.CODE_DECODING -> UiText.res(R.string.error_decoding)
            // Отказы выхода из группы — своим текстом: `message` сервера
            // всегда по-русски, а немцу с испанцем нужен их язык. Оба текста
            // обязаны объяснять выход наружу, иначе человек упрётся в стену.
            "has_operations" -> UiText.res(
                if (isSelf) R.string.error_leave_has_operations
                else R.string.error_remove_member_has_operations,
            )
            "last_member" -> UiText.res(R.string.error_leave_last_member)
            // Своя офлайн-заглушка (OutboxSyncer): её `message` мы пишем сами,
            // и по-русски — показывать его немцу нельзя.
            "unsupported" -> UiText.res(R.string.error_outbox_unsupported)
            // Текст сервера уже человеческий и на языке пользователя; при пустом
            // теле ApiException сам подставит ресурс по коду.
            else -> api.uiText()
        }
    }
    if (error is IOException) {
        return UiText.res(R.string.error_no_internet)
    }
    return error.message
        ?.takeIf { it.isNotBlank() }
        ?.let { UiText.Raw(it) }
        ?: UiText.res(R.string.error_generic)
}

/**
 * Текст ошибки привязки/отвязки способа входа (порт iOS `identityErrorText`).
 *
 * Коды сервера (`identity_taken`, `last_identity`) пользователю не показываем:
 * ему нужно объяснение и следующий шаг, а не идентификатор ошибки. Свой текст,
 * а не серверный `message`, потому что «что делать дальше» зависит от экрана:
 * здесь это «войдите через тот профиль» и «сначала привяжите другой способ».
 */
fun identityErrorText(error: Throwable): UiText {
    val api = error as? ApiException ?: return humanErrorText(error)
    return when {
        api.code == "identity_taken" -> UiText.res(R.string.error_identity_taken)
        // У аккаунта уже есть ДРУГАЯ личность этого провайдера. Сервер не
        // подменяет её молча: у Apple подмена оставила бы Splitty в списке
        // «Вход через Apple» прежнего Apple ID навсегда (отзывать нечем).
        api.code == "identity_already_linked" -> UiText.res(R.string.error_identity_already_linked)
        api.code == "last_identity" -> UiText.res(R.string.error_last_identity)
        api.code == "invalid_password" -> UiText.res(R.string.profile_password_invalid_current)
        // 400 provider_rejected — сервер не принял id-токен ПРОВАЙДЕРА
        // (подпись/срок/aud/nonce не сошлись). Отдельный код заведён затем,
        // чтобы не путать это с мёртвой сессией Splitty: 401 от /me/link/*
        // теперь означает ровно её и вызывает глобальный разлогин, а 400 —
        // нет. Пользователю разница не нужна: ему нужно «попробуйте ещё раз».
        api.code == "provider_rejected" -> UiText.res(R.string.error_provider_rejected)
        // 401 здесь — уже протухшая сессия Splitty: AuthInterceptor её сбросил
        // и нас вернёт на экран входа. Текст остаётся нейтральным — алерт
        // может успеть показаться поверх перехода.
        api.isUnauthorized -> UiText.res(R.string.error_provider_rejected)
        else -> humanErrorText(error)
    }
}

/** Отличает таймаут от прочих сетевых сбоев (SocketTimeout / прерванный I/O). */
private fun isTimeout(error: Throwable?): Boolean = when (error) {
    is SocketTimeoutException -> true
    is InterruptedIOException -> error.message?.contains("timeout", ignoreCase = true) == true
    else -> false
}
