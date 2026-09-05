package com.zagir.splitty.ui.components

import com.zagir.splitty.R
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.ui.UiText
import java.io.IOException
import java.net.SocketTimeoutException
import kotlin.test.assertEquals
import org.junit.Test

/**
 * humanErrorText — таймаут отдельной веткой, transport/decoding — свои тексты.
 *
 * Сравниваются ресурсы, а не строки: сами формулировки переведены на пять
 * языков и живут в strings.xml, а проверять здесь надо ВЫБОР ветки.
 */
class HumanErrorTextTest {

    @Test
    fun `timeout gets its own message, not offline`() {
        val direct = humanErrorText(SocketTimeoutException("timeout"))
        assertEquals(UiText.res(R.string.error_timeout), direct)
    }

    @Test
    fun `transport wrapping a timeout cause is still reported as timeout`() {
        // Репозиторий сворачивает IOException в CODE_TRANSPORT, но кладёт причину
        // в cause — таймаут не должен маскироваться под «нет интернета».
        val wrapped = ApiException(
            status = null,
            code = ApiException.CODE_TRANSPORT,
            message = "transport failure",
            cause = SocketTimeoutException("timeout"),
        )
        assertEquals(UiText.res(R.string.error_timeout), humanErrorText(wrapped))
    }

    @Test
    fun `plain transport error is offline copy`() {
        val transport = ApiException(null, ApiException.CODE_TRANSPORT, "transport failure")
        assertEquals(UiText.res(R.string.error_no_internet), humanErrorText(transport))
    }

    @Test
    fun `decoding error has a human copy`() {
        val decoding = ApiException(null, ApiException.CODE_DECODING, "raw")
        assertEquals(UiText.res(R.string.error_decoding), humanErrorText(decoding))
    }

    @Test
    fun `server text is paired with a translated fallback`() {
        // Присланный текст конкретнее нашего, но приходит только по-русски.
        // Поэтому наружу уходит пара «текст сервера + ресурс по коду», а язык
        // выбирает уже показ (см. UiTextServerLanguageTest).
        val server = ApiException(
            status = 409,
            code = "conflict",
            message = "Демонстрационный аккаунт трогать нельзя",
            fromServer = true,
        )
        assertEquals(
            UiText.Server("Демонстрационный аккаунт трогать нельзя", R.string.error_conflict),
            humanErrorText(server),
        )
    }

    @Test
    fun `unknown code keeps the status number as the fallback`() {
        // Кода клиент не знает, переводить нечем — нерусскому интерфейсу
        // достанется «Ошибка сервера (409)», и номер обязан быть подставлен.
        val server = ApiException(
            status = 409,
            code = "brand_new_code",
            message = "Так делать пока нельзя",
            fromServer = true,
        )
        assertEquals(
            UiText.Server("Так делать пока нельзя", R.string.error_server_status, 409),
            humanErrorText(server),
        )
    }

    @Test
    fun `every backend code resolves to a translated string`() {
        // Список снят с writeError() бэкенда. Код, выпавший из карты, молча
        // уводит человека на русский текст сервера — поймать это можно только
        // сверкой контракта, а не глазами на одном экране.
        val codes = listOf(
            "internal", "validation", "not_found", "unavailable", "unauthorized",
            "rate_limited", "conflict", "forbidden", "unsupported_media", "too_large",
            "identity_taken", "has_operations", "email_taken", "stale_operation",
            "provider_rejected", "not_a_friend", "last_member", "last_identity",
            "invalid_password", "invalid_credentials", "invalid_code",
            "identity_already_linked", "ai_disabled", "room_too_large",
            "ai_quota_exceeded", "receipt_belongs_to_other_account", "subscriptions_disabled",
        )
        val unmapped = codes.filter {
            ApiException.fallbackRes(it) == R.string.error_server_status
        }
        assertEquals(emptyList(), unmapped, "коды без своего текста: $unmapped")
    }

    @Test
    fun `leave refusals get their own localized copy, not the russian server text`() {
        // `message` сервера всегда по-русски: немцу с испанцем показали бы его.
        val hasOperations = ApiException(
            status = 409,
            code = "has_operations",
            message = "Сначала уберите себя из расходов",
            fromServer = true,
        )
        assertEquals(
            UiText.res(R.string.error_leave_has_operations),
            humanErrorText(hasOperations),
        )
        val lastMember = ApiException(
            status = 409,
            code = "last_member",
            message = "Вы последний участник",
            fromServer = true,
        )
        assertEquals(UiText.res(R.string.error_leave_last_member), humanErrorText(lastMember))
    }

    @Test
    fun `removing somebody else does not get the you-text`() {
        // Тот же код 409, но действие другое: «уберите СЕБЯ из расходов» человеку,
        // который убирает соседа, отправляет искать не те расходы. Сервер и iOS
        // (`leaveErrorText(_:isSelf:)`) различают эти два случая с самого начала.
        val hasOperations = ApiException(
            status = 409,
            code = "has_operations",
            message = "На участнике записаны расходы",
            fromServer = true,
        )
        assertEquals(
            UiText.res(R.string.error_remove_member_has_operations),
            humanErrorText(hasOperations, isSelf = false),
        )
    }

    @Test
    fun `empty server body falls back to a resource by code`() {
        val server = ApiException(status = 409, code = "conflict", message = "conflict (409)")
        assertEquals(UiText.res(R.string.error_conflict), humanErrorText(server))
    }

    @Test
    fun `unknown code falls back to the server-status resource with the code`() {
        // Единственный ресурс с аргументом — «Ошибка сервера (%1$d)».
        val server = ApiException(status = 500, code = "boom", message = "boom (500)")
        assertEquals(UiText.res(R.string.error_server_status, 500), humanErrorText(server))
    }

    @Test
    fun `bare io exception is offline copy`() {
        assertEquals(UiText.res(R.string.error_no_internet), humanErrorText(IOException("boom")))
    }
}
