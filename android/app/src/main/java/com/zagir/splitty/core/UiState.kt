package com.zagir.splitty.core

/**
 * Единое UDF-состояние экрана для ViewModel + StateFlow:
 * Loading → Content / Error. Конвенция проекта — все экраны-списки
 * следующих агентов используют этот тип (аналог LoadState в iOS).
 */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>

    /** [message] — человекочитаемо и по-русски (ApiException.message). */
    data class Error(val message: String) : UiState<Nothing>

    data class Content<T>(val value: T) : UiState<T>
}
