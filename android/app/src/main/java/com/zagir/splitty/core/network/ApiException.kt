package com.zagir.splitty.core.network

import androidx.annotation.StringRes
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import java.io.IOException
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * Единая ошибка работы с REST API.
 *
 * [message] идёт в логи и в исключение. Человеку показывается [uiText]: если
 * бэкенд прислал свой `message`, берём его (он уже на языке пользователя),
 * иначе — локальный ресурс по коду. Русских литералов здесь нет: класс живёт
 * в сетевом слое, Context туда не дотянуть.
 */
class ApiException(
    /** HTTP-статус; null для сетевых/клиентских ошибок. */
    val status: Int?,
    /** Машиночитаемый код (snake_case бэкенда или локальный transport/decoding/invalid_url). */
    val code: String,
    override val message: String,
    /** true — [message] пришёл от сервера, а не собран локально по коду. */
    val fromServer: Boolean = false,
    cause: Throwable? = null,
) : Exception(message, cause) {

    /**
     * Что показать человеку: текст сервера либо ресурс по коду.
     *
     * Статус подставляется ТОЛЬКО в «Ошибка сервера (%1$d)» — единственный
     * ресурс с аргументом. Совать его во все подряд нельзя: лишние аргументы
     * getString проглатывает молча, но два одинаковых по смыслу UiText
     * переставали быть равными, и это ловилось только тестом.
     */
    fun uiText(): UiText {
        if (fromServer) return UiText.Raw(message)
        val res = fallbackRes(code)
        return if (res == R.string.error_server_status) {
            UiText.res(res, status ?: 0)
        } else {
            UiText.res(res)
        }
    }

    /** true для 401 — сессию нужно сбросить (глобальный разлогин делает [AuthInterceptor]). */
    val isUnauthorized: Boolean get() = status == 401

    /**
     * true — `DELETE /me` упал уже ПОСЛЕ tombstone: аккаунт удалён, а чистка
     * его данных не завершена. От обычного `internal` (сбой ДО tombstone,
     * аккаунт цел и нетронут) отличается только кодом, и различие критично:
     * доделать чистку может лишь повторный запрос ЭТИМ ЖЕ токеном — маршрут
     * висит на `authDeleted` ровно ради повтора, а войти заново нельзя,
     * личности вычищены. См. [com.zagir.splitty.core.session.Session.purgePending].
     */
    val isPurgeIncomplete: Boolean get() = code == CODE_PURGE_INCOMPLETE

    companion object {
        const val CODE_TRANSPORT = "transport"
        const val CODE_DECODING = "decoding"
        const val CODE_INVALID_URL = "invalid_url"

        /** Сбой чистки ПОСЛЕ tombstone — см. [isPurgeIncomplete]. */
        const val CODE_PURGE_INCOMPLETE = "purge_incomplete"

        /**
         * Ресурс подстановочного текста по коду ошибки. Только он и нужен:
         * подставляется, когда тело ответа пустое и показывать нечего.
         * Единственный ресурс с аргументом — «Ошибка сервера (%1$d)».
         */
        @StringRes
        fun fallbackRes(code: String): Int = when (code) {
            "validation" -> R.string.error_validation
            "unauthorized", "invalid_code" -> R.string.error_unauthorized
            "forbidden" -> R.string.error_forbidden
            "not_found" -> R.string.error_not_found
            "conflict" -> R.string.error_conflict
            "too_large" -> R.string.error_too_large
            // AI-распознавание (parse): сервер обычно шлёт свой message,
            // но при пустом теле подставляем текст по коду.
            "unsupported_media" -> R.string.error_unsupported_media
            "rate_limited" -> R.string.error_rate_limited
            "ai_disabled" -> R.string.error_ai_disabled
            CODE_TRANSPORT -> R.string.error_no_internet
            CODE_DECODING -> R.string.error_decoding
            CODE_INVALID_URL -> R.string.error_invalid_url
            else -> R.string.error_server_status
        }
    }
}

/** Тело ошибки бэкенда: {"error": {"code": "...", "message": "..."}}. */
@Serializable
internal data class ErrorEnvelope(val error: Payload) {
    @Serializable
    data class Payload(val code: String = "", val message: String = "")
}

/** Разбирает не-2xx ответ бэкенда в [ApiException]. */
internal fun parseApiError(status: Int, errorBody: String?, json: Json): ApiException {
    val envelope = errorBody
        ?.takeIf { it.isNotBlank() }
        ?.let { runCatching { json.decodeFromString(ErrorEnvelope.serializer(), it) }.getOrNull() }
    val code = envelope?.error?.code.orEmpty()
    val serverMessage = envelope?.error?.message?.takeIf { it.isNotBlank() }
    return ApiException(
        status = status,
        code = code,
        // В логи — либо текст сервера, либо машинный код: человеку показывается uiText().
        message = serverMessage ?: "$code ($status)",
        fromServer = serverMessage != null,
    )
}

/**
 * Невалидный адрес сервера. Наследует IOException, потому что бросается из
 * OkHttp-интерцептора (другие типы там запрещены); адрес НЕ подменяется
 * дефолтным — пользователь должен увидеть ошибку.
 */
// Текст английский и технический: он идёт в лог и в message исключения,
// а человеку показывается R.string.error_invalid_url через ApiException.uiText().
class InvalidBaseUrlException : IOException("invalid base url")
