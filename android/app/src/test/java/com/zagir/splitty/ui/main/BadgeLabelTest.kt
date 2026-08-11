package com.zagir.splitty.ui.main

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * Текст бейджа на табе «Уведомления».
 *
 * Сервер отдаёт точное число до 99, а 100 означает «больше 99». Правило зеркалит
 * iOS (MainTabView.badgeLabel, покрыт BadgeLabelTests) — до этого теста
 * android-половина контракта не проверялась ничем, и «100» на табе прошёл бы
 * зелёным как точный счёт.
 */
class BadgeLabelTest {

    @Test
    fun `no badge when nothing unread`() {
        assertNull(badgeLabel(0, "99+"))
        assertNull(badgeLabel(-1, "99+"))
    }

    @Test
    fun `exact count up to ceiling`() {
        assertEquals("1", badgeLabel(1, "99+"))
        assertEquals("99", badgeLabel(99, "99+"))
    }

    @Test
    fun `overflow marker instead of a number nobody counted`() {
        assertEquals("99+", badgeLabel(100, "99+"))
        assertEquals("99+", badgeLabel(500, "99+"))
    }
}
