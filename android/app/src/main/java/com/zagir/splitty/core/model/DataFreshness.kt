package com.zagir.splitty.core.model

import java.time.Instant

/**
 * Свежесть показанных данных: пришли ли они из офлайн-кеша и когда последний
 * раз обновлялись с сервера.
 */
data class DataFreshness(
    val fromCache: Boolean = false,
    val updatedAt: Instant? = null,
)
