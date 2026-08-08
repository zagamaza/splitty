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
    fun `server side message wins over local fallback`() {
        // fromServer = true — бэкенд прислал свой текст, он уже на языке
        // пользователя, подменять его локальным ресурсом нельзя.
        val server = ApiException(
            status = 409,
            code = "conflict",
            message = "Действие сейчас невозможно",
            fromServer = true,
        )
        assertEquals(UiText.Raw("Действие сейчас невозможно"), humanErrorText(server))
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
