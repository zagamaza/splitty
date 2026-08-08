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
 * локализованных, прежде всего для `message` из тела ошибки бэкенда.
 */
sealed interface UiText {

    data class Res(@StringRes val id: Int, val args: List<Any> = emptyList()) : UiText

    data class Raw(val value: String) : UiText

    fun resolve(context: Context): String = when (this) {
        is Raw -> value
        is Res -> if (args.isEmpty()) {
            context.getString(id)
        } else {
            context.getString(id, *args.toTypedArray())
        }
    }

    companion object {
        fun res(@StringRes id: Int, vararg args: Any): UiText = Res(id, args.toList())
    }
}

/** Разрешает [UiText] в строку в текущей локали композиции. */
@Composable
fun UiText.resolve(): String = resolve(LocalContext.current)
