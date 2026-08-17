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
}
