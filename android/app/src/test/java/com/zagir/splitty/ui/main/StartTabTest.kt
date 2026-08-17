package com.zagir.splitty.ui.main

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Стартовая вкладка.
 *
 * Новый аккаунт открывался на «Друзьях» — разделе, где у него по определению
 * пусто, и первой фразой приложения было «Пока нет друзей». Группы — то, ради
 * чего человек пришёл.
 */
class StartTabTest {

    @Test
    fun `app opens on groups`() {
        assertEquals(MainRoutes.GROUPS, MainRoutes.START, "приложение снова открывается не на группах")
    }
}
