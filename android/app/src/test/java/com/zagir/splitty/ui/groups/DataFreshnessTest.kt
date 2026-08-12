package com.zagir.splitty.ui.groups

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Свежесть показанных данных.
 *
 * Признак «данные из кеша» вычислялся и раньше, но никуда не попадал: человек
 * смотрел на старые суммы, ничего об этом не зная, — и «неправильный» баланс
 * выглядел как ошибка расчёта, а не как отсутствие связи.
 */
class DataFreshnessTest {

    @Test
    fun `fresh load remembers when the data came from the server`() {
        val fresh = DataFreshness(fromCache = false, updatedAt = Instant.parse("2026-08-12T10:00:00Z"))

        assertTrue(!fresh.fromCache)
        assertNotNull(fresh.updatedAt, "время обновления не сохранено — подписи будет нечего показать")
    }

    /**
     * Кеш после успешной загрузки НЕ стирает время: человеку важно знать, когда
     * суммы были верными, а не только что связи нет.
     */
    @Test
    fun `cache keeps the previous update time`() {
        val fresh = DataFreshness(fromCache = false, updatedAt = Instant.parse("2026-08-12T10:00:00Z"))
        val cached = fresh.copy(fromCache = true)

        assertTrue(cached.fromCache)
        assertNotNull(cached.updatedAt)
    }

    /** Первый запуск офлайн: времени обновления ещё нет, и это отдельный текст. */
    @Test
    fun `cold start offline has no update time`() {
        val cold = DataFreshness(fromCache = true)

        assertTrue(cold.fromCache)
        assertNull(cold.updatedAt)
    }
}
