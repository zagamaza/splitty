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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
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
import com.zagir.splitty.core.money.MINOR_FACTOR
import com.zagir.splitty.core.money.money
import com.zagir.splitty.core.money.moneyMinor
import com.zagir.splitty.ui.theme.Splitty
import kotlin.math.abs

// Компоненты дизайн-системы — порт ios/Splitty/Core/DesignSystem.swift.
// Все секции экранов — SurfaceCard на фоне Splitty.colors.bg; деньги —
// ТОЛЬКО через MoneyText/MoneyTotalsText.

// MARK: Карточка-поверхность

/**
 * Лист бумаги: фон `surface`, почти прямые углы (3dp), без мягкой тени.
 * Светлая тема — едва заметная подложка, тёмная — hairline-рамка, чтобы
 * бумага отделялась от стола. Аналог iOS `.surfaceCard()`.
 */
@Composable
fun SurfaceCard(
    modifier: Modifier = Modifier,
    padding: Dp = 16.dp,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = Splitty.colors
    if (LocalReceiptSheetSections.current) {
        // Секция общего листа: своя бумага и тень не нужны.
        Column(modifier = modifier.padding(padding), content = content)
        return
    }
    val shape = RoundedCornerShape(3.dp)
    val cardModifier = if (colors.isDark) {
        modifier
            .clip(shape)
            .background(colors.surface)
            .border(1.dp, colors.hairline, shape)
    } else {
        modifier
            .shadow(
                elevation = 1.dp,
                shape = shape,
                ambientColor = Color.Black.copy(alpha = 0.05f),
                spotColor = Color.Black.copy(alpha = 0.05f),
            )
            .clip(shape)
            .background(colors.surface)
    }
    Column(modifier = cardModifier.padding(padding), content = content)
}

// MARK: Кнопки

/**
 * Основное действие: прямоугольник во всю ширину (углы 3dp), высота 54dp,
 * заливка `accent`, текст `onAccent`, pressed — scale 0.98. Аналог iOS `.primaryPill`.
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
        shape = RoundedCornerShape(3.dp),
        colors = ButtonDefaults.buttonColors(
            containerColor = if (pressed) colors.accentPressed else colors.accent,
            contentColor = colors.onAccent,
            disabledContainerColor = colors.accent.copy(alpha = 0.45f),
            disabledContentColor = colors.onAccent,
        ),
        interactionSource = interactionSource,
    ) {
        Text(
            text = text,
            fontSize = 16.sp,
            fontWeight = FontWeight.SemiBold,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
    }
}

/**
 * Вторичная кнопка-чип: тонкая рамка, углы 3dp; [isSelected] — чернильная
 * рамка и подложка (выбор группы/участника, фильтры). Аналог iOS `.softChip(isSelected:)`.
 *
 * [enabled] = false гасит и клик, и текст: чип в роли действия («Отвязать»
 * последний способ входа) обязан выглядеть недоступным ДО запроса, а не
 * молча ничего не делать.
 */
@Composable
fun SoftChip(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    isSelected: Boolean = false,
    enabled: Boolean = true,
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
        isSelected -> colors.ink.copy(alpha = 0.1f)
        pressed && enabled -> colors.ink.copy(alpha = 0.08f)
        else -> Color.Transparent
    }
    val contentColor = when {
        !enabled -> colors.inkSecondary.copy(alpha = 0.5f)
        else -> colors.ink
    }
    val chipShape = RoundedCornerShape(3.dp)
    Box(
        modifier = modifier
            .scale(scale)
            .clip(chipShape)
            .background(background)
            .border(1.dp, if (isSelected) colors.ink else colors.hairline, chipShape)
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                enabled = enabled,
                onClick = onClick,
            )
            .padding(horizontal = 14.dp, vertical = 9.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = contentColor,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

// MARK: Деньги

/** Семантическая роль суммы — определяет цвет [MoneyText]. */
enum class MoneyRole {
    /** По знаку: `> 0` — positive, `< 0` — negative, `0` — inkSecondary. */
    AUTO,

    /** «Вам должны»/получено — всегда positive. */
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
    amount: Long,
    modifier: Modifier = Modifier,
    role: MoneyRole = MoneyRole.AUTO,
    size: TextUnit = 17.sp,
    weight: FontWeight = FontWeight.SemiBold,
    currency: String = "RUB",
    /**
     * Точная величина в минорных единицах; null — вызывающий её не знает, и
     * показывается целое.
     */
    exactMinor: Long? = null,
    /**
     * Минимальный масштаб шрифта при нехватке ширины (аналог iOS
     * `.minimumScaleFactor`): 1f — не ужимать (по умолчанию); <1f — крупная
     * сумма в узкой плитке уменьшается вместо обрезки. Значение = нижняя граница.
     */
    minScale: Float = 1f,
) {
    val colors = Splitty.colors
    val color = when (role) {
        MoneyRole.POSITIVE -> colors.positive
        MoneyRole.NEGATIVE -> colors.negative
        MoneyRole.NEUTRAL -> colors.ink
        // Знак берётся у ТОЧНОЙ величины: у округлённой долг 0,25 читается
        // нулём, и честный плюс красился бы «в расчёте».
        MoneyRole.AUTO -> when {
            (exactMinor ?: (amount * MINOR_FACTOR)) > 0 -> colors.positive
            (exactMinor ?: (amount * MINOR_FACTOR)) < 0 -> colors.negative
            else -> colors.inkSecondary
        }
    }
    // Ужимаем размер только когда об этом попросили (minScale<1f): при
    // переполнении строки уменьшаем шрифт до нижней границы size*minScale.
    var scaledSize by remember(amount, currency, size) { mutableStateOf(size) }
    Text(
        text = moneyMinor(abs(exactMinor ?: (amount * MINOR_FACTOR)), currency),
        modifier = modifier,
        color = color,
        fontSize = scaledSize,
        fontWeight = weight,
        maxLines = 1,
        softWrap = false,
        overflow = TextOverflow.Clip,
        onTextLayout = { result ->
            if (minScale < 1f && result.hasVisualOverflow && scaledSize > size * minScale) {
                scaledSize = scaledSize * 0.92f
            }
        },
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
    amount: Long,
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
            MoneyText(
                primary.sum,
                exactMinor = primary.exactMinor,
                size = primarySize,
                currency = primary.currency,
            )
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
                    MoneyText(
                        total.sum,
                        exactMinor = total.exactMinor,
                        size = secondarySize,
                        currency = total.currency,
                    )
                }
            }
        }
    }
}

// MARK: Заголовок секции

/**
 * Заголовок секции — печатный реквизит: моноширинный 12sp, вторичный цвет.
 * Регистр текста НЕ меняет; капс — у [ReceiptLabel]. Аналог iOS `.sectionHeaderStyle()`.
 */
@Composable
fun SectionHeader(text: String, modifier: Modifier = Modifier, maxLines: Int = Int.MAX_VALUE) {
    if (LocalReceiptSheetSections.current) {
        // Заголовок стоит с отступом 4 от края секции, строки — 16: здесь
        // добираем до строк, а пунктир отделяет раздел от предыдущего.
        Column(modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            ReceiptDivider()
            SectionHeaderText(text, Modifier.padding(start = 12.dp), maxLines)
        }
    } else {
        SectionHeaderText(text, modifier, maxLines)
    }
}

@Composable
private fun SectionHeaderText(text: String, modifier: Modifier, maxLines: Int) {
    Text(
        text = text,
        modifier = modifier,
        fontSize = 12.sp,
        fontWeight = FontWeight.Medium,
        fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
        color = Splitty.colors.inkSecondary,
        letterSpacing = 0.3.sp,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
    )
}

/** true — внутри [ReceiptSheetSections]: секции без своей бумаги, заголовки с пунктиром. */
val LocalReceiptSheetSections = staticCompositionLocalOf { false }

/**
 * Секции экрана настроек — разделами ОДНОГО листа чека: карточки секций
 * теряют свою бумагу, заголовки получают пунктир сверху и выравнивание по
 * строкам. Строки внутри секций уже имеют поля 16, поэтому у листа 6
 * (паритет iOS `receiptSheetSections()`).
 */
@Composable
fun ReceiptSheetSections(
    modifier: Modifier = Modifier,
    spacing: Dp = 16.dp,
    content: @Composable ColumnScope.() -> Unit,
) {
    ReceiptPaper(modifier = modifier, padding = 6.dp) {
        CompositionLocalProvider(LocalReceiptSheetSections provides true) {
            Column(verticalArrangement = Arrangement.spacedBy(spacing), content = content)
        }
    }
}
