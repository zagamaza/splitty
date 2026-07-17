package com.zagir.splitty.ui.components

import kotlin.test.assertFalse
import kotlin.test.assertTrue
import org.junit.Test

/** «Reduce motion» на Android = Animator Duration Scale == 0 (и только 0). */
class ReduceMotionTest {

    @Test
    fun `zero scale means reduce motion`() {
        assertTrue(isReduceMotion(0f))
    }

    @Test
    fun `normal and slowed scales are not reduce motion`() {
        assertFalse(isReduceMotion(1f))
        assertFalse(isReduceMotion(0.5f))
        assertFalse(isReduceMotion(2f))
    }
}
