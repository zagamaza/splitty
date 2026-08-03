package com.zagir.splitty.core.network

import java.io.IOException
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * Единая ошибка работы с REST API: [message] — человекочитаемо и по-русски
 * (для ошибок бэкенда — message из тела `{"error": {code, message}}`).
 */
class ApiException(
    /** HTTP-статус; null для сетевых/клиентских ошибок. */
    val status: Int?,
    /** Машиночитаемый код (snake_case бэкенда или локальный transport/decoding/invalid_url). */
    val code: String,
    override val message: String,
    cause: Throwable? = null,
) : Exception(message, cause) {

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

        fun fallbackMessage(status: Int, code: String): String = when (code) {
            "validation" -> "Некорректные данные"
            "unauthorized", "invalid_code" -> "Требуется вход"
            "forbidden" -> "Нет доступа"
            "not_found" -> "Не найдено"
            "conflict" -> "Действие сейчас невозможно"
            "too_large" -> "Слишком большой запрос"
            // AI-распознавание (parse): сервер обычно шлёт свой русский message,
            // но при пустом теле подставляем человекочитаемый fallback по коду.
            "unsupported_media" -> "Неподдерживаемый формат файла"
            "rate_limited" -> "Слишком много запросов. Попробуйте позже"
            "ai_disabled" -> "Распознавание сейчас недоступно"
            else -> "Ошибка сервера ($status)"
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
    val message = envelope?.error?.message
        ?.takeIf { it.isNotBlank() }
        ?: ApiException.fallbackMessage(status, code)
    return ApiException(status = status, code = code, message = message)
}

/**
 * Невалидный адрес сервера. Наследует IOException, потому что бросается из
 * OkHttp-интерцептора (другие типы там запрещены); адрес НЕ подменяется
 * дефолтным — пользователь должен увидеть ошибку.
 */
class InvalidBaseUrlException : IOException("Некорректный адрес сервера")
