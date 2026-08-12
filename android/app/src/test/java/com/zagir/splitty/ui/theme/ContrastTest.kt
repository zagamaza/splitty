package com.zagir.splitty.ui.theme

import androidx.compose.ui.graphics.Color
import kotlin.math.pow
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * Контраст текста в светлой теме.
 *
 * Акцентный изумруд на белом даёт 3.39:1. Крупной сумме и заливке кнопки этого
 * достаточно, а подписи в 12–15 sp — нет: минимум 4.5:1. Разница видна не всем
 * и не всегда: на солнце и на дешёвой матрице такая подпись просто пропадает.
 */
class ContrastTest {

    private fun luminance(color: Color): Double {
        fun channel(c: Float): Double {
            val v = c.toDouble()
            return if (v <= 0.03928) v / 12.92 else ((v + 0.055) / 1.055).pow(2.4)
        }
        return 0.2126 * channel(color.red) + 0.7152 * channel(color.green) + 0.0722 * channel(color.blue)
    }

    private fun ratio(a: Color, b: Color): Double {
        val la = luminance(a)
        val lb = luminance(b)
        return (maxOf(la, lb) + 0.05) / (minOf(la, lb) + 0.05)
    }

    private val light = splittyLightColorsForTest()
    private val white = Color(0xFFFFFFFF)

    @Test
    fun `text tokens pass AA on white surface`() {
        val accent = ratio(light.accentText, white)
        val negative = ratio(light.negativeText, white)
        assertTrue(accent >= 4.5, "акцентный текст даёт $accent:1 — мелкая подпись нечитаема")
        assertTrue(negative >= 4.5, "негативный текст даёт $negative:1 — мелкая подпись нечитаема")
    }

    /**
     * Крупные суммы и заливка кнопки остаются на прежнем акценте: смысл токена
     * в том, чтобы поменять цвет ТОЛЬКО там, где кегль мелкий.
     */
    @Test
    fun `plain accent stays as it was`() {
        assertTrue(
            ratio(light.accent, white) < 4.5,
            "цвет акцента изменился — проверьте, что это осознанно",
        )
    }
}
