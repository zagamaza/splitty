package com.zagir.splitty.ui.onboarding

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** Правила показа приветствия. */
class WelcomeGateTest {

    @Test
    fun `shown to a fresh install`() {
        assertTrue(shouldShowIntro(hasSeen = false, hasPendingDeeplink = false))
    }

    /** Второй запуск: эта установка всё это уже читала. */
    @Test
    fun `not shown twice`() {
        assertFalse(shouldShowIntro(hasSeen = true, hasPendingDeeplink = false))
    }

    /**
     * Пришёл по ссылке приглашения — ведём в тусу, а не в рассказ о продукте.
     *
     * Показать приглашённому четыре страницы вместо тусы, в которую его
     * позвали, значит потерять переход.
     */
    @Test
    fun `deeplink wins`() {
        assertFalse(shouldShowIntro(hasSeen = false, hasPendingDeeplink = true))
    }

    /**
     * Намерение побеждает и тогда, когда приветствие уже на экране.
     *
     * Решение не защёлкивается: гейт — чистая функция от текущего состояния, и
     * появившееся намерение закрывает приветствие само. Свежий тап по ссылке
     * пишется на диск асинхронно, и на холодном старте первый кадр рисуется
     * раньше ответа DataStore — поэтому у намерения есть ещё и синхронный
     * источник, `PendingJoinStore.arrivedInProcess`.
     */
    @Test
    fun `deeplink arriving later closes intro`() {
        assertTrue(shouldShowIntro(hasSeen = false, hasPendingDeeplink = false))
        assertFalse(shouldShowIntro(hasSeen = false, hasPendingDeeplink = true))
    }
}
