package com.zagir.splitty.core.ui

import android.content.Context
import android.content.res.Configuration
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import java.util.Locale
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Что человек РЕАЛЬНО читает в ошибке от сервера, на каждом языке.
 *
 * Бэкенд отвечает только по-русски: `Accept-Language` он не читает, а тексты в
 * `writeError` — русские литералы. Показывать их немцу нельзя, но и терять
 * нельзя: под общим кодом `forbidden` сервер объясняет, ЧТО именно запрещено,
 * а наш ресурс скажет лишь «Нет доступа». Отсюда развилка по языку — и
 * проверять её надо на настоящем resolve, а не на равенстве объектов: пара
 * собирается верно и при сломанной развилке.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class UiTextServerLanguageTest {

    private val serverText = "Демонстрационный аккаунт удалить нельзя"

    private fun context(language: String): Context {
        val base = ApplicationProvider.getApplicationContext<Context>()
        val configuration = Configuration(base.resources.configuration)
        configuration.setLocale(Locale(language))
        return base.createConfigurationContext(configuration)
    }

    @Test
    fun `russian interface keeps the specific server text`() {
        val text = UiText.Server(serverText, R.string.error_forbidden).resolve(context("ru"))
        assertEquals(serverText, text)
    }

    @Test
    fun `other languages read their own translation instead`() {
        for (language in listOf("en", "de", "es", "fr")) {
            val text = UiText.Server(serverText, R.string.error_forbidden).resolve(context(language))
            assertFalse(text == serverText, "$language: показан русский текст сервера")
            assertFalse(
                text.any { it in 'А'..'я' },
                "$language: в тексте осталась кириллица — «$text»",
            )
        }
    }

    @Test
    fun `unknown code falls back to the status number, not to russian`() {
        val german = context("de")
        val text = UiText.Server("так делать пока нельзя", R.string.error_server_status, 409)
            .resolve(german)
        assertEquals(german.getString(R.string.error_server_status, 409), text)
    }
}
