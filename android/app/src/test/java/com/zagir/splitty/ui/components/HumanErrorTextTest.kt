package com.zagir.splitty.ui.components

import com.zagir.splitty.core.network.ApiException
import java.io.IOException
import java.net.SocketTimeoutException
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import org.junit.Test

/** humanErrorText — таймаут отдельной веткой, transport/decoding — свои тексты. */
class HumanErrorTextTest {

    @Test
    fun `timeout gets its own message, not offline`() {
        val direct = humanErrorText(SocketTimeoutException("timeout"))
        assertEquals("Сервер долго не отвечает. Попробуйте ещё раз", direct)
    }

    @Test
    fun `transport wrapping a timeout cause is still reported as timeout`() {
        // Репозиторий сворачивает IOException в CODE_TRANSPORT, но кладёт причину
        // в cause — таймаут не должен маскироваться под «нет интернета».
        val wrapped = ApiException(
            status = null,
            code = ApiException.CODE_TRANSPORT,
            message = "Нет соединения с сервером",
            cause = SocketTimeoutException("timeout"),
        )
        assertEquals("Сервер долго не отвечает. Попробуйте ещё раз", humanErrorText(wrapped))
    }

    @Test
    fun `plain transport error is offline copy`() {
        val transport = ApiException(null, ApiException.CODE_TRANSPORT, "нет сети")
        assertTrue(humanErrorText(transport).startsWith("Нет соединения с интернетом"))
    }

    @Test
    fun `decoding error has a human copy`() {
        val decoding = ApiException(null, ApiException.CODE_DECODING, "raw")
        assertTrue(humanErrorText(decoding).startsWith("Не удалось обработать ответ сервера"))
    }

    @Test
    fun `server side api error keeps its human message`() {
        val server = ApiException(status = 409, code = "conflict", message = "Действие сейчас невозможно")
        assertEquals("Действие сейчас невозможно", humanErrorText(server))
    }

    @Test
    fun `bare io exception is offline copy`() {
        assertTrue(humanErrorText(IOException("boom")).startsWith("Нет соединения с интернетом"))
    }
}
