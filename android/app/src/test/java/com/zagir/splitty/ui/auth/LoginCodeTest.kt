package com.zagir.splitty.ui.auth

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** Нормализация одноразового кода входа (порт iOS LoginCodeTests). */
class LoginCodeTest {

    @Test
    fun `normalize strips whitespace and uppercases`() {
        assertEquals("ABCD2345", LoginCode.normalize(" abcd 2345\n"))
        assertEquals("ABCD2345", LoginCode.normalize("ABCD2345"))
        assertEquals("", LoginCode.normalize("  \t "))
    }

    @Test
    fun `isValid requires min length after normalization`() {
        assertTrue(LoginCode.isValid("abcd23"))
        assertTrue(LoginCode.isValid(" ab cd 23 45 "))
        assertFalse(LoginCode.isValid("abc12"))
        assertFalse(LoginCode.isValid(""))
    }
}
