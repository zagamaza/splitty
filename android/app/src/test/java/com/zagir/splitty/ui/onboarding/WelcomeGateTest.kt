package com.zagir.splitty.ui.onboarding

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** Правила показа приветствия. */
class WelcomeGateTest {

    @Test
    fun `shown to a new account without groups`() {
        assertTrue(shouldShowWelcome(hasSeen = false, groupCount = 0, hasPendingDeeplink = false))
    }

    /** Второй запуск: человек уже всё это читал. */
    @Test
    fun `not shown twice`() {
        assertFalse(shouldShowWelcome(hasSeen = true, groupCount = 0, hasPendingDeeplink = false))
    }

    /** У кого есть группы — объяснять нечего. */
    @Test
    fun `not shown when groups exist`() {
        assertFalse(shouldShowWelcome(hasSeen = false, groupCount = 2, hasPendingDeeplink = false))
    }

    /** Пришёл по ссылке приглашения — ведём в группу, а не в рассказ о продукте. */
    @Test
    fun `deeplink wins`() {
        assertFalse(shouldShowWelcome(hasSeen = false, groupCount = 0, hasPendingDeeplink = true))
    }

    /**
     * Закрыл — значит закрыл, даже если диск об этом ещё не знает.
     *
     * Ровно этот случай и ломал онбординг: `markWelcomeSeen` пишется
     * асинхронно, а список комнат перечитывается часто. Пока запись в пути,
     * `hasSeen` честно отдаёт `false`, и без защёлки приветствие возвращалось
     * поверх списка — а в аналитике появлялся ещё один onboarding_started.
     */
    @Test
    fun `handled wins over stale hasSeen`() {
        assertFalse(
            shouldShowWelcome(
                hasSeen = false,
                groupCount = 0,
                hasPendingDeeplink = false,
                alreadyHandled = true,
            ),
        )
    }

    /** Защёлка не подменяет остальные условия: без неё правило прежнее. */
    @Test
    fun `not handled keeps previous rule`() {
        assertTrue(
            shouldShowWelcome(
                hasSeen = false,
                groupCount = 0,
                hasPendingDeeplink = false,
                alreadyHandled = false,
            ),
        )
    }
}
