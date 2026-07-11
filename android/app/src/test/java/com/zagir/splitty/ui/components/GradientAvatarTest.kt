package com.zagir.splitty.ui.components

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/** SplitMix64-индекс палитры и инициалы — паритет с iOS UserAvatarView. */
class GradientAvatarTest {

    @Test
    fun `index is stable and within palette`() {
        for (id in listOf(0L, 1L, 42L, 100L, 200L, 123_456_789L, Long.MAX_VALUE)) {
            val index = avatarGradientIndex(id)
            assertTrue(index in 0..9, "id=$id index=$index")
            assertEquals(index, avatarGradientIndex(id)) // детерминизм
        }
    }

    @Test
    fun `round ids do not collapse to one gradient`() {
        val indices = listOf(100L, 200L, 300L, 400L, 500L).map { avatarGradientIndex(it) }
        assertTrue(indices.distinct().size > 1, "indices=$indices")
    }

    @Test
    fun `initials are first letters of first two words uppercased`() {
        assertEquals("ЗН", avatarInitials("Загир Нурмухаметов"))
        assertEquals("А", avatarInitials("Алмаз"))
        assertEquals("AB", avatarInitials("alpha beta gamma"))
        assertEquals("?", avatarInitials(""))
        assertEquals("?", avatarInitials("   "))
    }
}
