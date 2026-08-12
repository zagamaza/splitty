package com.zagir.splitty.ui.expense

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.ui.res.stringResource
import com.zagir.splitty.R
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.model.hasUnknown
import com.zagir.splitty.core.model.isSurcharge
import com.zagir.splitty.core.model.shareList
import com.zagir.splitty.core.money.money
import com.zagir.splitty.core.money.moneyRange
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.rememberReduceMotion
import com.zagir.splitty.ui.theme.Splitty
import kotlin.math.max
import kotlin.math.roundToInt

// Чек-карточка — порт ios/Splitty/Features/Expense/ReceiptView.swift.
// Перфорированные края (Canvas), пунктирные разделители строк, шапка
// «ПОЗИЦИИ · N», моноширинные суммы, стек аватарок с бейджами веса (×N) и
// замком фикс-суммы, подсказка деления у каждой строки, подвал
// Подытог → Сборы → Итого. Единый вид для детали операции (read-only) и формы
// добавления: передай колбэки — строки и правило сбора станут тапабельными,
// нераспознанные имена — красными пульсирующими чипами на сопоставление.

/**
 * @param onEditItem тап по строке позиции (индекс в исходном [items]); null — read-only.
 * @param onResolveUnknown тап по чипу нераспознанного имени (индекс позиции, имя); null — read-only.
 * @param onToggleSurchargeRule тап по правилу деления сбора (индекс); null — read-only.
 * @param highlightedIndices индексы строк, изменённых последней голосовой правкой —
 *   мягкая акцентная подсветка (гасится вью-моделью по таймеру).
 */
@Composable
fun ReceiptCard(
    items: List<OperationItem>,
    members: List<User>,
    currency: String,
    modifier: Modifier = Modifier,
    onEditItem: ((Int) -> Unit)? = null,
    onResolveUnknown: ((Int, String) -> Unit)? = null,
    onToggleSurchargeRule: ((Int) -> Unit)? = null,
    highlightedIndices: Set<Int> = emptySet(),
) {
    val colors = Splitty.colors
    // (исходный индекс, позиция) — колбэки работают по исходным индексам.
    val indexed = items.mapIndexed { index, item -> index to item }
    val itemsOnly = indexed.filter { !it.second.isSurcharge }
    val surcharges = indexed.filter { it.second.isSurcharge }
    val subtotal = itemsOnly.sumOf { it.second.price }
    val surchargeTotal = surcharges.sumOf { it.second.price }
    val hasPriceless = itemsOnly.any { it.second.price < 1 }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(4.dp)),
    ) {
        Perforation(top = true, paper = colors.receiptPaper, notch = colors.bg)
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .background(colors.receiptPaper)
                .padding(horizontal = 18.dp)
                .padding(top = 10.dp, bottom = 10.dp),
        ) {
            Header(count = itemsOnly.size)
            RowDivider()
            itemsOnly.forEachIndexed { pos, (index, item) ->
                if (pos != 0) RowDivider()
                ItemRow(
                    index = index,
                    item = item,
                    members = members,
                    currency = currency,
                    highlighted = highlightedIndices.contains(index),
                    onEditItem = onEditItem,
                    onResolveUnknown = onResolveUnknown,
                )
            }
            if (surcharges.isNotEmpty()) {
                RowDivider()
                FooterLine(stringResource(R.string.receipt_subtotal), subtotal, currency, total = false)
                surcharges.forEach { (index, item) ->
                    SurchargeRow(index, item, currency, onToggleSurchargeRule, onEditItem)
                }
            }
            TotalRule()
            // При позициях без цены итог неполный — «≥» честно говорит, что число
            // вырастет, когда цены будут указаны.
            FooterLine(
                title = stringResource(if (hasPriceless) R.string.receipt_total_at_least else R.string.receipt_total),
                amount = subtotal + surchargeTotal,
                currency = currency,
                total = true,
            )
        }
        Perforation(top = false, paper = colors.receiptPaper, notch = colors.bg)
    }
}

// MARK: header

@Composable
private fun Header(count: Int) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.receipt_items_header),
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.4.sp,
            color = colors.ink.copy(alpha = 0.6f),
        )
        Spacer(Modifier.weight(1f))
        Text(
            text = stringResource(R.string.receipt_items_count, count),
            fontSize = 11.sp,
            fontFamily = FontFamily.Monospace,
            color = colors.ink.copy(alpha = 0.6f),
        )
    }
}

// MARK: rows

@Composable
private fun ItemRow(
    index: Int,
    item: OperationItem,
    members: List<User>,
    currency: String,
    highlighted: Boolean,
    onEditItem: ((Int) -> Unit)?,
    onResolveUnknown: ((Int, String) -> Unit)?,
) {
    val colors = Splitty.colors
    var rowModifier = Modifier
        .fillMaxWidth()
    if (onEditItem != null) {
        rowModifier = rowModifier.clickable { onEditItem(index) }
    }
    // Подсветка изменённой строки: акцентный фон со скруглением, лёгкий внутренний
    // отступ — зеркало iOS (`Color.accent.opacity(0.12)`).
    val bgModifier = if (highlighted) {
        Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(colors.accent.copy(alpha = 0.12f))
            .padding(horizontal = 8.dp)
    } else {
        Modifier
    }
    Column(
        modifier = rowModifier.then(bgModifier).padding(vertical = 13.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = item.name.ifEmpty { stringResource(R.string.receipt_item_fallback) },
                fontSize = 16.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (item.qty > 1) {
                Spacer(Modifier.width(8.dp))
                Text(
                    text = "×${item.qty}",
                    fontSize = 12.5.sp,
                    fontFamily = FontFamily.Monospace,
                    color = colors.inkSecondary,
                )
            }
            Spacer(Modifier.weight(1f))
            Spacer(Modifier.width(8.dp))
            if (item.price < 1) {
                // Цена не определена (модель услышала блюдо, но не цену): метка ведёт
                // в шит позиции. В интерактиве мягко пульсирует, чтобы глаз нашёл ответ.
                DashedCapsuleChip(
                    text = stringResource(R.string.receipt_price_missing),
                    pulsing = onEditItem != null,
                )
            } else {
                Text(
                    text = money(item.price, currency),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.SemiBold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.ink,
                )
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            AvatarStack(index, item, members, onResolveUnknown)
            Spacer(Modifier.weight(1f))
            Spacer(Modifier.width(8.dp))
            Text(
                text = shareHintText(shareHint(item), currency),
                fontSize = 11.5.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.ink.copy(alpha = 0.6f),
                textAlign = TextAlign.End,
            )
        }
    }
}

@Composable
private fun AvatarStack(
    index: Int,
    item: OperationItem,
    members: List<User>,
    onResolveUnknown: ((Int, String) -> Unit)?,
) {
    val even = isEven(item)
    // С бейджами (веса/фиксы) аватарки не внахлёст — иначе бейдж перекрывается
    // соседней аватаркой и вес не читается.
    val hasBadges = !even || item.shareList.any { it.amount != null }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Row(horizontalArrangement = Arrangement.spacedBy(if (hasBadges) 3.dp else (-7).dp)) {
            item.shareList.forEach { share ->
                Box(contentAlignment = Alignment.BottomEnd) {
                    Avatar(share.userId, members)
                    when {
                        share.amount != null -> Badge(accent = true) {
                            Icon(
                                imageVector = Icons.Filled.Lock,
                                contentDescription = null,
                                tint = Color.White,
                                modifier = Modifier.size(9.dp),
                            )
                        }
                        !even -> Badge {
                            Text(
                                text = "${share.weight}",
                                fontSize = 9.sp,
                                fontWeight = FontWeight.Bold,
                                fontFamily = FontFamily.Monospace,
                                color = Color.White,
                            )
                        }
                    }
                }
            }
        }
        (item.unknown ?: emptyList()).forEach { name ->
            Spacer(Modifier.width(6.dp))
            DashedCapsuleChip(
                text = "$name ?",
                pulsing = onResolveUnknown != null,
                modifier = if (onResolveUnknown != null) {
                    Modifier.clickable { onResolveUnknown(index, name) }
                } else {
                    Modifier
                },
            )
        }
    }
}

@Composable
private fun Avatar(id: Long, members: List<User>) {
    val colors = Splitty.colors
    val user = members.firstOrNull { it.id == id }
    if (user != null) {
        GradientAvatar(
            user = user,
            size = 26.dp,
            modifier = Modifier.border(2.dp, colors.receiptPaper, CircleShape),
        )
    } else {
        Box(
            modifier = Modifier
                .size(26.dp)
                .clip(CircleShape)
                .background(colors.inkSecondary.copy(alpha = 0.25f)),
        )
    }
}

@Composable
private fun Badge(accent: Boolean = false, content: @Composable () -> Unit) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .offset(x = 3.dp, y = 2.dp)
            .size(15.dp)
            .clip(CircleShape)
            .background(if (accent) colors.accent else colors.ink)
            .border(1.5.dp, colors.receiptPaper, CircleShape),
        contentAlignment = Alignment.Center,
        content = { content() },
    )
}

// MARK: surcharges

@Composable
private fun SurchargeRow(
    index: Int,
    item: OperationItem,
    currency: String,
    onToggle: ((Int) -> Unit)?,
    onEditItem: ((Int) -> Unit)?,
) {
    val colors = Splitty.colors
    // Тап открывает ItemSheet — без него сбор с нулевой ценой нельзя было ни
    // отредактировать, ни удалить: derivedShares возвращал null, сохранение
    // блокировалось «доли не сходятся», и выхода из этого состояния не было.
    var rowModifier = Modifier
        .fillMaxWidth()
        .padding(vertical = 9.dp)
    if (onEditItem != null) {
        rowModifier = rowModifier.clickable { onEditItem(index) }
    }
    Column(
        modifier = rowModifier,
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = item.name.ifEmpty { stringResource(R.string.receipt_surcharge_fallback) },
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
            )
            if (item.percent != null) {
                Spacer(Modifier.width(6.dp))
                Text(
                    text = "${item.percent}%",
                    fontSize = 12.5.sp,
                    fontFamily = FontFamily.Monospace,
                    color = colors.ink.copy(alpha = 0.6f),
                )
            }
            Spacer(Modifier.weight(1f))
            Text(
                text = money(item.price, currency),
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                fontFamily = FontFamily.Monospace,
                color = colors.ink,
            )
        }
        SurchargeRule(index, item, onToggle)
    }
}

/**
 * Правило деления сбора. Интерактивно — чип-переключатель
 * «⇄ Пропорционально / Поровну»; read-only — тихая подпись.
 */
@Composable
private fun SurchargeRule(index: Int, item: OperationItem, onToggle: ((Int) -> Unit)?) {
    val colors = Splitty.colors
    val equally = item.split == OperationItem.SPLIT_EQUALLY
    if (onToggle != null) {
        Row(
            modifier = Modifier
                .clip(CircleShape)
                .background(colors.ink.copy(alpha = 0.05f))
                .border(1.dp, colors.ink.copy(alpha = 0.12f), CircleShape)
                .clickable { onToggle(index) }
                .padding(horizontal = 10.dp, vertical = 5.dp)
                .wrapContentWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(
                imageVector = Icons.Filled.SwapHoriz,
                contentDescription = null,
                tint = colors.inkSecondary,
                modifier = Modifier.size(12.dp),
            )
            Text(
                text = stringResource(if (equally) R.string.receipt_surcharge_equally else R.string.receipt_surcharge_proportional),
                fontSize = 11.5.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
            )
        }
    } else {
        Text(
            text = stringResource(if (equally) R.string.receipt_surcharge_equally_short else R.string.receipt_surcharge_proportional_short),
            fontSize = 11.5.sp,
            fontFamily = FontFamily.Monospace,
            color = colors.ink.copy(alpha = 0.6f),
        )
    }
}

// MARK: footer

@Composable
private fun FooterLine(title: String, amount: Long, currency: String, total: Boolean) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = if (total) 12.dp else 7.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            fontSize = if (total) 12.sp else 12.sp,
            fontWeight = if (total) FontWeight.Bold else FontWeight.SemiBold,
            letterSpacing = if (total) 1.2.sp else 0.4.sp,
            color = if (total) colors.ink else colors.inkSecondary,
        )
        Spacer(Modifier.weight(1f))
        Text(
            text = money(amount, currency),
            fontSize = if (total) 19.sp else 14.sp,
            fontWeight = if (total) FontWeight.Bold else FontWeight.SemiBold,
            fontFamily = FontFamily.Monospace,
            color = colors.ink,
        )
    }
}

// MARK: separators

@Composable
private fun RowDivider() {
    val colors = Splitty.colors
    val stroke = colors.ink.copy(alpha = 0.14f)
    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(1.dp),
    ) {
        drawLine(
            color = stroke,
            start = Offset(0f, size.height / 2f),
            end = Offset(size.width, size.height / 2f),
            strokeWidth = size.height,
            pathEffect = PathEffect.dashPathEffect(
                floatArrayOf(3.dp.toPx(), 4.dp.toPx()),
            ),
        )
    }
}

@Composable
private fun TotalRule() {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .padding(top = 8.dp)
            .fillMaxWidth()
            .height(2.dp)
            .background(colors.ink),
    )
}

/**
 * Перфорированный (зубчатый) край чека: полоса цвета бумаги с полукруглыми
 * вырезами цвета фона по внешней кромке. Порт iOS `Perforation`.
 */
@Composable
private fun Perforation(top: Boolean, paper: Color, notch: Color) {
    val d = 11.dp
    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(d / 2)
            .background(paper),
    ) {
        val dPx = d.toPx()
        val count = max(1, (size.width / dPx).roundToInt())
        val used = count * dPx
        val startX = (size.width - used) / 2f + dPx / 2f
        // Вырезы центрированы на внешней кромке полосы: у верхней — сверху (y=0),
        // у нижней — снизу (y=height); ровно половина круга «надкусывает» бумагу.
        val cy = if (top) 0f else size.height
        for (i in 0 until count) {
            drawCircle(color = notch, radius = dPx / 2f, center = Offset(startX + i * dPx, cy))
        }
    }
}

/**
 * Красный пунктирный чип-капсула («цена?» / «Саня ?»). Мягко пульсирует, когда
 * [pulsing] — требует ответа в интерактивном режиме; уважает reduce motion.
 */
@Composable
private fun DashedCapsuleChip(
    text: String,
    pulsing: Boolean,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    val reduceMotion = rememberReduceMotion()
    val alpha = if (pulsing && !reduceMotion) {
        val transition = rememberInfiniteTransition(label = "pulse")
        val value by transition.animateFloat(
            initialValue = 1f,
            targetValue = 0.55f,
            animationSpec = infiniteRepeatable(
                animation = tween(1000),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "pulseAlpha",
        )
        value
    } else {
        1f
    }
    Box(
        modifier = modifier
            .alpha(alpha)
            .clip(CircleShape)
            .background(colors.negative.copy(alpha = 0.1f))
            .dashedCapsuleBorder(colors.negative)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(
            text = text,
            fontSize = 11.5.sp,
            fontWeight = FontWeight.Bold,
            color = colors.negative,
        )
    }
}

/** Пунктирная обводка-капсула (штрих 3, пробел 3) — рамка красных чипов. */
private fun Modifier.dashedCapsuleBorder(color: Color): Modifier =
    drawWithContent {
        drawContent()
        val r = size.height / 2f
        drawRoundRect(
            color = color,
            cornerRadius = CornerRadius(r, r),
            style = Stroke(
                width = 1.dp.toPx(),
                pathEffect = PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 3.dp.toPx())),
            ),
        )
    }

// MARK: helpers (pure — под JVM-тест)

/** true — все весовые доли равны (нет фикс-сумм с разным весом): аватарки без бейджей. */
internal fun isEven(item: OperationItem): Boolean {
    val ws = item.shareList.filter { it.amount == null }.map { it.weight }
    val f = ws.firstOrNull() ?: return true
    return ws.none { it != f }
}

/**
 * Подсказка деления строки: «целиком» / «по 33–34 ₽ × 3» / «точные суммы» /
 * «укажите цену» / «кто это — выберите». Зеркало iOS `shareHint`.
 *
 * Возвращает ТИП, а не готовый текст: текст зависит от языка приложения, а
 * правило деления — нет. Раньше строки были вшиты по-русски, и человек с
 * английским интерфейсом читал подсказки чека на чужом языке.
 */
sealed interface ShareHint {
    /** Подсказки нет (нет долей и цены). */
    object None : ShareHint
    /** Есть нераспознанные имена — сначала сопоставить. */
    object Unknown : ShareHint
    /** Доли есть, цены нет. */
    object NoPrice : ShareHint
    /** Один человек — вся строка на него. */
    object Whole : ShareHint
    /** Часть суммы зафиксирована, остальное делится поровну. */
    data class FixedThenEven(val fixed: Long) : ShareHint
    /** Все доли заданы точными суммами. */
    object ExactAmounts : ShareHint
    /** Поровну между [people]. */
    data class PerPerson(val price: Long, val people: Int) : ShareHint
    /** По весам: [units] «штук» цены. */
    data class PerUnit(val price: Long, val units: Int) : ShareHint
}

internal fun shareHint(item: OperationItem): ShareHint {
    if (item.hasUnknown) return ShareHint.Unknown
    val n = item.shareList.size
    if (item.price < 1) return if (n > 0) ShareHint.NoPrice else ShareHint.None
    if (n == 0) return ShareHint.None
    if (n == 1) return ShareHint.Whole
    if (item.shareList.any { it.amount != null }) {
        val fixed = item.shareList.sumOf { it.amount ?: 0 }
        val weighted = item.shareList.count { it.amount == null }
        return if (weighted > 0) ShareHint.FixedThenEven(fixed) else ShareHint.ExactAmounts
    }
    if (isEven(item)) return ShareHint.PerPerson(item.price, n)
    return ShareHint.PerUnit(item.price, item.shareList.sumOf { it.weight })
}

/** Текст подсказки на языке приложения. */
@Composable
internal fun shareHintText(hint: ShareHint, currency: String): String = when (hint) {
    ShareHint.None -> ""
    ShareHint.Unknown -> stringResource(R.string.receipt_hint_unknown)
    ShareHint.NoPrice -> stringResource(R.string.receipt_hint_no_price)
    ShareHint.Whole -> stringResource(R.string.receipt_hint_whole)
    is ShareHint.FixedThenEven ->
        stringResource(R.string.receipt_hint_fixed_then_even, money(hint.fixed, currency))
    ShareHint.ExactAmounts -> stringResource(R.string.receipt_hint_exact)
    is ShareHint.PerPerson ->
        stringResource(
            R.string.receipt_hint_per_person,
            perPersonText(hint.price, hint.people, currency),
            hint.people,
        )
    is ShareHint.PerUnit ->
        stringResource(
            R.string.receipt_hint_per_unit,
            hint.units,
            perPersonText(hint.price, hint.units, currency),
        )
}

/**
 * «По сколько с носа»: при неделящейся нацело цене — честный диапазон «33–34 ₽»
 * (иначе «по 33 ₽ × 3» не сходится с итогом строки 100 ₽). Зеркало iOS.
 */
internal fun perPersonText(price: Long, parts: Int, currency: String): String {
    val n = max(1, parts)
    val base = price / n
    return if (price % n == 0L) money(base, currency) else moneyRange(base, base + 1, currency)
}
