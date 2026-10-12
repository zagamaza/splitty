package com.zagir.splitty.ui.components

import kotlin.test.Test
import kotlin.test.assertEquals

/** Инициалы аватара — паритет с iOS UserAvatarView. */
class GradientAvatarTest {

    @Test
    fun `initials are first letters of first two words uppercased`() {
        assertEquals("ЗН", avatarInitials("Загир Нурмухаметов"))
        assertEquals("А", avatarInitials("Алмаз"))
        assertEquals("AB", avatarInitials("alpha beta gamma"))
        assertEquals("?", avatarInitials(""))
        assertEquals("?", avatarInitials("   "))
    }
}
