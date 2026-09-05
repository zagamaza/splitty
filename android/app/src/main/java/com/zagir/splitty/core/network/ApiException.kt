package com.zagir.splitty.core.network

import androidx.annotation.StringRes
import com.zagir.splitty.R
import com.zagir.splitty.core.model.AiQuota
import com.zagir.splitty.core.ui.UiText
import java.io.IOException
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * Единая ошибка работы с REST API.
 *
 * [message] идёт в логи и в исключение. Человеку показывается [uiText]: текст
 * сервера в паре с ресурсом по коду. Присланное конкретнее нашего, но приходит
 * только по-русски (`Accept-Language` бэкенд не смотрит), поэтому выбирает
 * между ними [UiText.Server] — в момент показа, по локали интерфейса.
 *
 * Русских литералов здесь нет: класс живёт в сетевом слое, Context туда
 * не дотянуть.
 */
class ApiException(
    /** HTTP-статус; null для сетевых/клиентских ошибок. */
    val status: Int?,
    /** Машиночитаемый код (snake_case бэкенда или локальный transport/decoding/invalid_url). */
    val code: String,
    override val message: String,
    /** true — [message] пришёл от сервера, а не собран локально по коду. */
    val fromServer: Boolean = false,
    /**
     * Остаток распознаваний из тела ошибки лимита; null у прочих ошибок.
     * Экрану оплаты нужно показать, что именно закончилось и когда обновится.
     */
    val quota: AiQuota? = null,
    cause: Throwable? = null,
) : Exception(message, cause) {

    /**
     * Суточная норма распознаваний исчерпана — показывается экран оплаты.
     *
     * Отдельный признак, а не «просто 429»: на минутный троттл человек видит
     * спокойный тост, а сюда — предложение заплатить. Пока причина была одна,
     * тыкнувший микрофон дважды подряд получал бы paywall.
     */
    val isAiQuotaExceeded: Boolean get() = code == "ai_quota_exceeded"

    /**
     * Чек оформлен на другой аккаунт Splitor.
     *
     * Тупик, из которого человек не выберется сам: деньги списаны, а Plus не
     * появится, пока он не войдёт в тот аккаунт.
     */
    val isReceiptBoundToOtherAccount: Boolean get() = code == "receipt_belongs_to_other_account"

    /**
     * Что показать человеку: текст сервера либо ресурс по коду.
     *
     * Статус подставляется ТОЛЬКО в «Ошибка сервера (%1$d)» — единственный
     * ресурс с аргументом. Совать его во все подряд нельзя: лишние аргументы
     * getString проглатывает молча, но два одинаковых по смыслу UiText
     * переставали быть равными, и это ловилось только тестом.
     */
    fun uiText(): UiText {
        val res = fallbackRes(code)
        val arg = if (res == R.string.error_server_status) status ?: 0 else null
        if (fromServer && message.isNotBlank()) {
            return UiText.Server(value = message, fallback = res, fallbackArg = arg)
        }
        return if (arg != null) UiText.res(res, arg) else UiText.res(res)
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

    /**
     * Ошибку стоит пережить в очереди неотправленных расходов: сеть не дошла
     * либо сервер ответил 5xx. 4xx сюда не входит — это отказ по данным, и
     * очередь его не исправит, она только спрятала бы его от человека.
     */
    val deservesOutbox: Boolean
        get() = code == CODE_TRANSPORT || (status ?: 0) >= 500

    companion object {
        const val CODE_TRANSPORT = "transport"
        const val CODE_DECODING = "decoding"
        const val CODE_INVALID_URL = "invalid_url"

        /** Сбой чистки ПОСЛЕ tombstone — см. [isPurgeIncomplete]. */
        const val CODE_PURGE_INCOMPLETE = "purge_incomplete"

        /**
         * Ресурс текста по коду ошибки — основной путь показа, а не запасной.
         * Список обязан покрывать ВСЕ коды бэкенда: код, которого здесь нет,
         * уводит человека на русский текст сервера. Единственный ресурс с
         * аргументом — «Ошибка сервера (%1$d)».
         */
        @StringRes
        fun fallbackRes(code: String): Int = when (code) {
            "validation" -> R.string.error_validation
            "internal" -> R.string.error_internal
            "unavailable" -> R.string.error_unavailable
            "unauthorized" -> R.string.error_unauthorized
            "invalid_code" -> R.string.error_invalid_code
            "forbidden" -> R.string.error_forbidden
            "not_found" -> R.string.error_not_found
            "conflict" -> R.string.error_conflict
            // Вход и профиль: у каждого отказа свой следующий шаг, и «Ошибка
            // сервера (409)» на месте «этот email уже зарегистрирован» отправляет
            // человека регистрироваться заново по кругу.
            "email_taken" -> R.string.error_email_taken
            "invalid_credentials" -> R.string.error_invalid_credentials
            "invalid_password" -> R.string.error_invalid_password
            "identity_taken" -> R.string.error_identity_taken
            "identity_already_linked" -> R.string.error_identity_already_linked
            "last_identity" -> R.string.error_last_identity
            "provider_rejected" -> R.string.error_provider_rejected
            "not_a_friend" -> R.string.error_not_a_friend
            // has_operations различает «себя» и «соседа», last_member — нет;
            // оба разбираются выше, в humanErrorText, здесь — общий случай.
            "has_operations" -> R.string.error_leave_has_operations
            "last_member" -> R.string.error_leave_last_member
            // Документ комнаты упёрся в потолок mongo: «что-то пошло не так»
            // означало бы, что человек жмёт «Сохранить» снова и снова
            "room_too_large" -> R.string.error_room_too_large
            "stale_operation" -> R.string.error_stale_operation
            "too_large" -> R.string.error_too_large
            // AI-распознавание (parse): сервер обычно шлёт свой message,
            // но при пустом теле подставляем текст по коду.
            "unsupported_media" -> R.string.error_unsupported_media
            "rate_limited" -> R.string.error_rate_limited
            "ai_quota_exceeded" -> R.string.error_ai_quota_exceeded
            "receipt_belongs_to_other_account" -> R.string.error_receipt_other_account
            "subscriptions_disabled" -> R.string.error_subscriptions_disabled
            "ai_disabled" -> R.string.error_ai_disabled
            CODE_TRANSPORT -> R.string.error_no_internet
            CODE_DECODING -> R.string.error_decoding
            CODE_INVALID_URL -> R.string.error_invalid_url
            else -> R.string.error_server_status
        }
    }
}

/**
 * Тело ошибки бэкенда: {"error": {"code": "...", "message": "..."}}.
 *
 * `quota` лежит РЯДОМ с конвертом, а не внутри него: форму `{"error":{...}}`
 * разбирают все сборки, включая 1.6, и трогать её нельзя.
 */
@Serializable
internal data class ErrorEnvelope(
    val error: Payload,
    val quota: AiQuota? = null,
) {
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
        quota = envelope?.quota,
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
