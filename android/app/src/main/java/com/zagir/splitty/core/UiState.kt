package com.zagir.splitty.core

import com.zagir.splitty.core.ui.UiText

/**
 * Единое UDF-состояние экрана для ViewModel + StateFlow:
 * Loading → Content / Error. Конвенция проекта — все экраны-списки
 * следующих агентов используют этот тип (аналог LoadState в iOS).
 */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>

    /**
     * [message] — ЧТО показать, а не готовая строка: экран разрешает её в текст
     * сам, в текущей локали. Литерал во ViewModel иначе уезжал бы на экран
     * по-русски при любом языке интерфейса.
     */
    data class Error(val message: UiText) : UiState<Nothing>

    data class Content<T>(val value: T) : UiState<T>
}
