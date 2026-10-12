package com.zagir.splitty.ui.theme

import androidx.compose.ui.graphics.Color
import kotlin.math.pow
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * Контраст текста «Общего чека» в обеих темах.
 *
 * Текст лежит либо на столе (`bg`), либо на бумаге (`surface`), и на обоих
 * мелкая подпись обязана давать 4.5:1. Стол темнее бумаги, поэтому проверка
 * только по бумаге пропустила бы серую подпись, нечитаемую на фоне экрана.
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

    private val themes = mapOf(
        "светлая" to splittyLightColorsForTest(),
        "тёмная" to splittyDarkColorsForTest(),
    )

    @Test
    fun `small text passes AA on table and paper`() {
        for ((theme, c) in themes) {
            val texts = mapOf(
                "ink" to c.ink, "inkSecondary" to c.inkSecondary, "accentText" to c.accentText,
                "negativeText" to c.negativeText, "positive" to c.positive, "stamp" to c.stamp,
            )
            for ((name, text) in texts) {
                for ((bgName, bg) in mapOf("bg" to c.bg, "surface" to c.surface)) {
                    val r = ratio(text, bg)
                    assertTrue(r >= 4.5, "$theme: $name на $bgName даёт $r:1 — мелкая подпись нечитаема")
                }
            }
        }
    }

    @Test
    fun `button label passes AA on action fill`() {
        for ((theme, c) in themes) {
            val r = ratio(c.onAccent, c.accent)
            assertTrue(r >= 4.5, "$theme: подпись основной кнопки даёт $r:1")
        }
    }
}
