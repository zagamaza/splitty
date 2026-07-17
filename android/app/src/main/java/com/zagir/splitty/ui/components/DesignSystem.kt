package com.zagir.splitty.ui.components

import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.core.model.CurrencySum
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.theme.Splitty
import kotlin.math.abs

// Компоненты дизайн-системы — порт ios/Splitty/Core/DesignSystem.swift.
// Все секции экранов — SurfaceCard на фоне Splitty.colors.bg; деньги —
// ТОЛЬКО через MoneyText/MoneyTotalsText.

// MARK: Карточка-поверхность

/**
 * Премиум-карточка: фон `surface`, скругление 20dp; светлая тема — мягкая
 * тень, тёмная — hairline-бордер без тени. Аналог iOS `.surfaceCard()`.
 */
@Composable
fun SurfaceCard(
    modifier: Modifier = Modifier,
    padding: Dp = 16.dp,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = Splitty.colors
    val shape = RoundedCornerShape(20.dp)
    val cardModifier = if (colors.isDark) {
        modifier
            .clip(shape)
            .background(colors.surface)
            .border(1.dp, colors.hairline, shape)
    } else {
        modifier
            .shadow(
                elevation = 6.dp,
                shape = shape,
                ambientColor = Color.Black.copy(alpha = 0.06f),
                spotColor = Color.Black.copy(alpha = 0.06f),
            )
            .clip(shape)
            .background(colors.surface)
    }
    Column(modifier = cardModifier.padding(padding), content = content)
}

// MARK: Кнопки

/**
 * Основной CTA: pill во всю ширину, высота 54dp, фон `accent`, белый
 * semibold-текст, pressed — scale 0.98. Аналог iOS `.primaryPill`.
 */
@Composable
fun PrimaryPillButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val colors = Splitty.colors
    val reduceMotion = rememberReduceMotion()
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val scale by animateFloatAsState(
        // reduce motion: без «прожатия» — как iOS `!reduceMotion && isPressed`.
        targetValue = if (pressed && !reduceMotion) 0.98f else 1f,
        animationSpec = spring(stiffness = Spring.StiffnessMedium),
        label = "pillScale",
    )
    Button(
        onClick = onClick,
        modifier = modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 54.dp)
            .scale(scale),
        enabled = enabled,
        shape = CircleShape,
        colors = ButtonDefaults.buttonColors(
            containerColor = if (pressed) colors.accentPressed else colors.accent,
            contentColor = Color.White,
            disabledContainerColor = colors.accent.copy(alpha = 0.45f),
            disabledContentColor = Color.White,
        ),
        interactionSource = interactionSource,
    ) {
        Text(text = text, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
    }
}

/**
 * Вторичная кнопка-чип: мягкая серая pill; [isSelected] — акцентная заливка
 * (для выбора группы/участника, фильтров). Аналог iOS `.softChip(isSelected:)`.
 */
@Composable
fun SoftChip(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    isSelected: Boolean = false,
) {
    val colors = Splitty.colors
    val reduceMotion = rememberReduceMotion()
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val scale by animateFloatAsState(
        targetValue = if (pressed && !reduceMotion) 0.98f else 1f,
        animationSpec = spring(stiffness = Spring.StiffnessMedium),
        label = "chipScale",
    )
    val background = when {
        isSelected -> colors.accent.copy(alpha = 0.14f)
        pressed -> colors.ink.copy(alpha = 0.12f)
        else -> colors.ink.copy(alpha = 0.06f)
    }
    Box(
        modifier = modifier
            .scale(scale)
            .clip(CircleShape)
            .background(background)
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick,
            )
            .padding(horizontal = 16.dp, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (isSelected) colors.accent else colors.ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

// MARK: Деньги

/** Семантическая роль суммы — определяет цвет [MoneyText]. */
enum class MoneyRole {
    /** По знаку: `> 0` — accent, `< 0` — negative, `0` — inkSecondary. */
    AUTO,

    /** «Вам должны»/получено — всегда accent. */
    POSITIVE,

    /** «Вы должны»/долг — всегда negative. */
    NEGATIVE,

    /** Обычная сумма без семантики долга («Всего потрачено») — ink. */
    NEUTRAL,
}

/**
 * Единственный способ показать сумму: tabular-цифры (без «пляски» при
 * изменении), семантическая окраска. Всегда рисует МОДУЛЬ суммы — знак
 * передаётся цветом/контекстом (единая конвенция проекта).
 * Порт iOS `MoneyText`.
 */
@Composable
fun MoneyText(
    amount: Int,
    modifier: Modifier = Modifier,
    role: MoneyRole = MoneyRole.AUTO,
    size: TextUnit = 17.sp,
    weight: FontWeight = FontWeight.SemiBold,
    currency: String = "RUB",
) {
    val colors = Splitty.colors
    val color = when (role) {
        MoneyRole.POSITIVE -> colors.accent
        MoneyRole.NEGATIVE -> colors.negative
        MoneyRole.NEUTRAL -> colors.ink
        MoneyRole.AUTO -> when {
            amount > 0 -> colors.accent
            amount < 0 -> colors.negative
            else -> colors.inkSecondary
        }
    }
    Text(
        text = money(abs(amount), currency),
        modifier = modifier,
        color = color,
        fontSize = size,
        fontWeight = weight,
        maxLines = 1,
        style = androidx.compose.ui.text.TextStyle(
            fontFeatureSettings = "tnum", // моноширинные цифры
        ),
    )
}

/**
 * MoneyText с анимацией смены значения — аналог iOS `.contentTransition(
 * .numericText())`: при изменении [amount] старое число уезжает, новое
 * въезжает (кроссфейд+сдвиг). Гейтится reduce motion — тогда меняется мгновенно.
 * Для «живых» сумм (итог позиций чека, баланс после платежа).
 */
@Composable
fun AnimatedMoneyText(
    amount: Int,
    modifier: Modifier = Modifier,
    role: MoneyRole = MoneyRole.AUTO,
    size: TextUnit = 17.sp,
    weight: FontWeight = FontWeight.SemiBold,
    currency: String = "RUB",
) {
    val reduceMotion = rememberReduceMotion()
    if (reduceMotion) {
        MoneyText(amount, modifier, role, size, weight, currency)
        return
    }
    androidx.compose.animation.AnimatedContent(
        targetState = amount,
        modifier = modifier,
        transitionSpec = {
            val up = targetState > initialState
            val enter = androidx.compose.animation.fadeIn() +
                androidx.compose.animation.slideInVertically { if (up) it else -it }
            val exit = androidx.compose.animation.fadeOut() +
                androidx.compose.animation.slideOutVertically { if (up) -it else it }
            enter togetherWith exit
        },
        label = "numericMoney",
    ) { value ->
        MoneyText(value, role = role, size = size, weight = weight, currency = currency)
    }
}

/**
 * Итог, где могут встретиться РАЗНЫЕ валюты (общий баланс, нетто друга):
 * суммы не складываются между валютами — основная валюта (наибольший |суммы|)
 * крупно, остальные — вторичной строкой мельче через «·». [totals] — уже
 * агрегированные aggregateByCurrency (пустой список — «0 ₽» серым: расчёт).
 * Порт iOS `MoneyTotalsText`.
 */
@Composable
fun MoneyTotalsText(
    totals: List<CurrencySum>,
    modifier: Modifier = Modifier,
    primarySize: TextUnit = 40.sp,
    secondarySize: TextUnit = 15.sp,
    horizontalAlignment: Alignment.Horizontal = Alignment.Start,
) {
    Column(
        modifier = modifier,
        horizontalAlignment = horizontalAlignment,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        val primary = totals.firstOrNull()
        if (primary != null) {
            MoneyText(primary.sum, size = primarySize, currency = primary.currency)
        } else {
            MoneyText(0, size = primarySize)
        }
        if (totals.size > 1) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                totals.drop(1).forEachIndexed { index, total ->
                    if (index > 0) {
                        Text(
                            text = "·",
                            fontSize = secondarySize,
                            fontWeight = FontWeight.SemiBold,
                            color = Splitty.colors.inkSecondary,
                        )
                    }
                    MoneyText(total.sum, size = secondarySize, currency = total.currency)
                }
            }
        }
    }
}

// MARK: Заголовок секции

/**
 * Заголовок секции: 13sp semibold, вторичный цвет, лёгкий kerning.
 * Регистр текста НЕ меняет. Аналог iOS `.sectionHeaderStyle()`.
 */
@Composable
fun SectionHeader(text: String, modifier: Modifier = Modifier, maxLines: Int = Int.MAX_VALUE) {
    Text(
        text = text,
        modifier = modifier,
        fontSize = 13.sp,
        fontWeight = FontWeight.SemiBold,
        color = Splitty.colors.inkSecondary,
        letterSpacing = 0.5.sp,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
    )
}
