package com.zagir.splitty.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.layout
import androidx.compose.ui.unit.dp
import com.zagir.splitty.ui.theme.Splitty
import kotlin.math.roundToInt

// Порт ios/Splitty/Features/Expense/AddExpenseView.swift → NudgeHighlight.
// Встряска поля + акцентная рамка при попытке недоступного действия (тап по
// заблокированной кнопке без выбранной группы). Тряска гейтится «reduce
// motion» (animator duration scale = 0) — как в iOS `reduceMotion ? 0 : phase`;
// рамка-вспышка остаётся всегда (это подсказка, а не декор).

/** Фазы смещения по X (в dp-эквиваленте px через density не нужен — мелкие). */
private val NUDGE_PHASES = listOf(-9f, 8f, -6f, 4f, 0f)

/**
 * [trigger] — счётчик: любое его изменение (кроме стартового 0) запускает
 * один цикл встряски+рамки. Держите его в состоянии VM/экрана и инкрементьте
 * на заблокированный тап.
 */
@Composable
fun Modifier.nudgeHighlight(trigger: Int): Modifier {
    val reduceMotion = rememberReduceMotion()
    val accent = Splitty.colors.accent
    val offsetX = remember { Animatable(0f) }
    val borderAlpha = remember { Animatable(0f) }

    LaunchedEffect(trigger) {
        if (trigger == 0) return@LaunchedEffect
        borderAlpha.snapTo(1f)
        offsetX.snapTo(0f)
        for (phase in NUDGE_PHASES) {
            val target = if (reduceMotion) 0f else phase
            offsetX.animateTo(target, tween(durationMillis = 90))
        }
        borderAlpha.animateTo(0f, tween(durationMillis = 120))
    }

    return this
        .layout { measurable, constraints ->
            val placeable = measurable.measure(constraints)
            layout(placeable.width, placeable.height) {
                placeable.placeRelative(offsetX.value.roundToInt(), 0)
            }
        }
        .border(
            width = 2.dp,
            color = accent.copy(alpha = borderAlpha.value),
            shape = RoundedCornerShape(20.dp),
        )
}
