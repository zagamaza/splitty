package com.zagir.splitty.ui

import com.zagir.splitty.push.PendingPushRoute
import com.zagir.splitty.push.PushRoute
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * Кому достанется тап по пушу.
 *
 * Намерение живёт до входа (тап при протухшей сессии — обычное дело), а войти
 * на устройстве может уже ДРУГОЙ человек. Без проверки владельца его уносило бы
 * в чужую группу — то же правило, что у приглашения по ссылке
 * (`PendingJoinStore.reconcileOwner`).
 */
class AppRootPushRouteTest {

    private val route = PushRoute.Operation("room-1", "op-1")

    @Test
    fun `own intent is delivered`() {
        assertEquals(route, pendingPushRouteFor(PendingPushRoute(route, ownerId = 7L), userId = 7L))
    }

    @Test
    fun `intent of another account is dropped`() {
        assertNull(pendingPushRouteFor(PendingPushRoute(route, ownerId = 7L), userId = 8L))
    }

    /**
     * Владелец неизвестен — холодный старт по тапу: активити просыпается
     * раньше, чем прочитана сессия, и сравнивать не с чем. Намерение обязано
     * доехать, иначе тап по пушу из убитого приложения не делает ничего.
     */
    @Test
    fun `intent without a known owner is delivered`() {
        assertEquals(route, pendingPushRouteFor(PendingPushRoute(route, ownerId = null), userId = 7L))
    }

    /** Профиль ещё не прочитан — это не «другой аккаунт». */
    @Test
    fun `unknown current user does not drop the intent`() {
        assertEquals(route, pendingPushRouteFor(PendingPushRoute(route, ownerId = 7L), userId = null))
    }

    @Test
    fun `nothing pending leads nowhere`() {
        assertNull(pendingPushRouteFor(null, userId = 7L))
    }
}
