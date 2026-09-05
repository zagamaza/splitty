package com.zagir.splitty.core.ui

import android.content.Context
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

/**
 * Текст для показа человеку: либо ссылка на ресурс, либо готовая строка.
 *
 * Нужен, потому что тексты рождаются в слоях без Context — ViewModel, репозиторий,
 * маппер ошибок, — а Context туда тащить нельзя: он привязан к конфигурации, и
 * ViewModel переживает смену локали, оставляя протухшую строку на экране.
 * Поэтому слой ниже возвращает ЧТО показать, а разрешает в текст уже Compose,
 * на каждой рекомпозиции и в текущей локали.
 *
 * [Raw] — не лазейка для литералов: он для текстов, пришедших снаружи и уже
 * локализованных.
 *
 * [Server] — текст из тела ошибки бэкенда. Он конкретнее нашего (под общим
 * кодом `forbidden` сервер объясняет, ЧТО именно нельзя), но приходит ТОЛЬКО
 * по-русски: `Accept-Language` бэкенд не смотрит. Поэтому язык решает, что
 * показать, и решает в момент показа — при создании ошибки локаль может быть
 * ещё другой.
 */
sealed interface UiText {

    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : UiText

    data class Raw(val value: String) : UiText

    data class Server(
        val value: String,
        @StringRes val fallback: Int,
        val fallbackArg: Int? = null,
    ) : UiText

    fun resolve(context: Context): String = when (this) {
        is Raw -> value
        is Res -> if (args.isEmpty()) {
            context.getString(id)
        } else {
            context.getString(id, *args.toTypedArray())
        }
        // Русскому интерфейсу — конкретный текст сервера, остальным — свой
        // перевод по коду: конкретность не стоит показанной кириллицы.
        is Server -> if (isRussian(context)) {
            value
        } else if (fallbackArg != null) {
            context.getString(fallback, fallbackArg)
        } else {
            context.getString(fallback)
        }
    }

    private fun isRussian(context: Context): Boolean {
        val locales = context.resources.configuration.locales
        return !locales.isEmpty && locales[0].language == "ru"
    }

    companion object {
        fun res(@StringRes id: Int, vararg args: Any): UiText = Res(id, args.toList())
    }
}

/** Разрешает [UiText] в строку в текущей локали композиции. */
@Composable
fun UiText.resolve(): String = resolve(LocalContext.current)
