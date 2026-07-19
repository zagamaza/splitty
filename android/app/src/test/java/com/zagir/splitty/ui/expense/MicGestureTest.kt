package com.zagir.splitty.ui.expense

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Пороги жеста hold-to-talk (Task 12): замок вверх, отмена влево, автостоп по
 * лимиту, отсечка случайного тапа. Эталон — iOS AddExpenseView (те же −70 px
 * и 24 000 байт), поэтому кейсы дословно повторяют его ветвления.
 */
class MicGestureTest {

    // --- Замок (свайп вверх) ---

    @Test
    fun `lock triggers on upward swipe past threshold`() {
        assertTrue(micLockTriggered(dx = 0f, dy = -71f))
        assertTrue(micLockTriggered(dx = 0f, dy = -200f))
    }

    @Test
    fun `lock does not trigger before threshold`() {
        assertFalse(micLockTriggered(dx = 0f, dy = -70f))
        assertFalse(micLockTriggered(dx = 0f, dy = -12f))
        assertFalse(micLockTriggered(dx = 0f, dy = 0f))
    }

    @Test
    fun `downward swipe never locks`() {
        assertFalse(micLockTriggered(dx = 0f, dy = 120f))
    }

    // Диагональ влево-вверх: побеждает та ось, которая доминирует. Иначе один
    // и тот же жест то замыкал бы запись, то отменял — самая обидная ошибка.
    @Test
    fun `diagonal up-left locks only when vertical dominates`() {
        assertTrue(micLockTriggered(dx = -40f, dy = -90f))
        assertFalse(micLockTriggered(dx = -120f, dy = -90f))
    }

    // --- Отмена (свайп влево) ---

    @Test
    fun `cancel triggers on left swipe past threshold`() {
        assertTrue(micCancelTriggered(dx = -71f, dy = 0f))
        assertTrue(micCancelTriggered(dx = -300f, dy = 10f))
    }

    @Test
    fun `cancel does not trigger before threshold or to the right`() {
        assertFalse(micCancelTriggered(dx = -70f, dy = 0f))
        assertFalse(micCancelTriggered(dx = 90f, dy = 0f))
    }

    @Test
    fun `diagonal up-left cancels only when horizontal dominates`() {
        assertTrue(micCancelTriggered(dx = -120f, dy = -90f))
        assertFalse(micCancelTriggered(dx = -90f, dy = -120f))
    }

    // Ровно диагональ 45°: замок выигрывает (>= против >), а отмена — нет.
    // Взаимоисключающие ветки: одновременно закрепить и отменить нельзя.
    @Test
    fun `exact diagonal locks and does not cancel`() {
        assertTrue(micLockTriggered(dx = -90f, dy = -90f))
        assertFalse(micCancelTriggered(dx = -90f, dy = -90f))
    }

    // --- Прогрессы для анимаций ---

    // Допуск: деление даёт -0.0 при нулевом смещении — знак нуля тут не значим.
    private val eps = 1e-6f

    @Test
    fun `lock progress ramps to one at threshold and clamps`() {
        assertEquals(0f, micLockProgress(0f), eps)
        assertEquals(0.5f, micLockProgress(-35f), eps)
        assertEquals(1f, micLockProgress(-70f), eps)
        assertEquals(1f, micLockProgress(-400f), eps)
        assertEquals(0f, micLockProgress(50f), eps)
    }

    @Test
    fun `cancel progress ramps to one at threshold and clamps`() {
        assertEquals(0f, micCancelProgress(0f), eps)
        assertEquals(0.5f, micCancelProgress(-35f), eps)
        assertEquals(1f, micCancelProgress(-70f), eps)
        assertEquals(1f, micCancelProgress(-400f), eps)
        assertEquals(0f, micCancelProgress(50f), eps)
    }

    // --- Автостоп по лимиту записи ---

    @Test
    fun `record times out at the limit`() {
        assertFalse(recordTimedOut(0))
        assertFalse(recordTimedOut(59))
        assertTrue(recordTimedOut(RECORD_LIMIT_SECONDS))
        assertTrue(recordTimedOut(RECORD_LIMIT_SECONDS + 5))
    }

    // --- Случайный тап вместо удержания ---

    @Test
    fun `short tap detected below the minimum wav size`() {
        assertTrue(isShortTap(0))
        assertTrue(isShortTap(SHORT_TAP_MIN_BYTES - 1))
        assertFalse(isShortTap(SHORT_TAP_MIN_BYTES))
        assertFalse(isShortTap(320_000))
    }

    // --- Длительность для экрана «Записано» ---

    @Test
    fun `recorded seconds derived from wav size and never zero`() {
        assertEquals(1, recordedSeconds(0))
        assertEquals(1, recordedSeconds(31_999))
        assertEquals(1, recordedSeconds(32_000))
        assertEquals(5, recordedSeconds(160_000))
        assertEquals(60, recordedSeconds(1_920_000))
    }
}
