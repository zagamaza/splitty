package com.zagir.splitty.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Outline
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.PathOperation
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.drawOutline
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.material3.Text
import com.zagir.splitty.ui.theme.Splitty
import java.util.Locale

// Элементы «Общего чека» — порт ios/Splitty/Core/ReceiptComponents.swift:
// бумага с перфорированным краем, пунктир, строка позиции, итог, штамп,
// текстовая ссылка, реквизит. Референс: docs/design/receipt/receipt/*.html.

// MARK: Реквизит

/**
 * Можно ли печатать реквизит капсом с разрядкой. У японского, китайского и
 * корейского регистра нет, а разрядка рвёт иероглифы.
 */
object ReceiptCaps {
    fun allowsCaps(languageCode: String?): Boolean =
        languageCode?.lowercase(Locale.ROOT) !in setOf("ja", "zh", "ko")
}

/** Печатный реквизит чека: «ПОЗИЦИЯ / КТО ПЛАТИЛ», «ВАШ ОСТАТОК», даты. */
@Composable
fun ReceiptLabel(
    text: String,
    modifier: Modifier = Modifier,
    color: Color = Splitty.colors.inkSecondary,
    fontSize: TextUnit = 11.sp,
    textAlign: TextAlign? = null,
) {
    val locale = LocalConfiguration.current.locales[0]
    val caps = ReceiptCaps.allowsCaps(locale.language)
    Text(
        text = if (caps) text.uppercase(locale) else text,
        modifier = modifier,
        color = color,
        fontSize = fontSize,
        fontWeight = FontWeight.Medium,
        fontFamily = FontFamily.Monospace,
        letterSpacing = if (caps) 1.sp else 0.sp,
        textAlign = textAlign,
    )
}

/**
 * Крупный системный шрифт: строки «подпись — сумма» в нём не помещаются в одну
 * линию и складываются в столбик (паритет iOS `isAccessibilitySize`).
 */
@Composable
@ReadOnlyComposable
fun isLargeFontScale(): Boolean = androidx.compose.ui.platform.LocalDensity.current.fontScale >= 1.5f

// MARK: Бумага

/** Нижний край чека — мелкая пила, как у оторванной ленты. */
class PerforatedEdgeShape(private val tooth: Dp = 6.dp) : Shape {
    override fun createOutline(size: Size, layoutDirection: LayoutDirection, density: Density): Outline {
        val t = with(density) { tooth.toPx() }
        if (t <= 0f) return Outline.Rectangle(Rect(Offset.Zero, size))
        val bottom = size.height - t
        val path = Path().apply {
            moveTo(0f, 0f)
            lineTo(size.width, 0f)
            lineTo(size.width, bottom)
            var x = size.width
            var down = true
            while (x > 0f) {
                val next = maxOf(0f, x - t)
                lineTo(next, if (down) size.height else bottom)
                down = !down
                x = next
            }
            lineTo(0f, bottom)
            close()
        }
        return Outline.Generic(path)
    }
}

/**
 * Лист чека: бумага во всю ширину, поля [padding], перфорированный низ.
 * Край — фон, в семантику не попадает.
 */
@Composable
fun ReceiptPaper(
    modifier: Modifier = Modifier,
    padding: Dp = 22.dp,
    perforated: Boolean = true,
    paper: Color = Splitty.colors.surface,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = Splitty.colors
    val tooth = if (perforated) 6.dp else 0.dp
    val shape = PerforatedEdgeShape(tooth)
    Column(
        modifier = modifier
            .fillMaxWidth()
            .drawBehind {
                val outline = shape.createOutline(size, layoutDirection, this)
                if (!colors.isDark) {
                    translate(top = 2.dp.toPx()) { drawOutline(outline, Color.Black.copy(alpha = 0.05f)) }
                }
                drawOutline(outline, paper)
                if (colors.isDark) drawOutline(outline, colors.hairline, style = Stroke(1.dp.toPx()))
            }
            .padding(start = padding, end = padding, top = padding, bottom = padding + tooth),
        content = content,
    )
}

/**
 * Строка длинного ленивого списка на общем листе: фон бумаги и поля по бокам.
 * Лист собирается из строк — LazyColumn не умеет фон на весь контент, — а
 * низ закрывает [ReceiptPaperEnd].
 */
fun Modifier.onReceiptPaper(paper: Color, horizontal: Dp = 18.dp): Modifier =
    this.fillMaxWidth().background(paper).padding(horizontal = horizontal)

/** Верх листа из строк: поле бумаги. */
@Composable
fun ReceiptPaperStart(height: Dp = 12.dp) {
    Box(Modifier.fillMaxWidth().height(height).background(Splitty.colors.surface))
}

/** Низ листа из строк: поле бумаги и перфорированный край. */
@Composable
fun ReceiptPaperEnd(height: Dp = 26.dp) {
    val paper = Splitty.colors.surface
    Box(
        Modifier
            .fillMaxWidth()
            .height(height)
            .drawBehind { drawOutline(PerforatedEdgeShape().createOutline(size, layoutDirection, this), paper) }
    )
}

/** Форма корешка: прямоугольник с полукруглыми вырезами в нижних углах. */
class ReceiptStubShape(private val notch: Dp = 7.dp) : Shape {
    override fun createOutline(size: Size, layoutDirection: LayoutDirection, density: Density): Outline {
        val n = with(density) { notch.toPx() }
        val rect = Path().apply { addRect(Rect(Offset.Zero, size)) }
        val notches = Path().apply {
            addOval(Rect(Offset(-n, size.height - n), Size(n * 2, n * 2)))
            addOval(Rect(Offset(size.width - n, size.height - n), Size(n * 2, n * 2)))
        }
        return Outline.Generic(Path.combine(PathOperation.Difference, rect, notches))
    }
}

/** Корешок счёта в списке: [closed] — бумага закрытого счёта. */
@Composable
fun ReceiptStub(
    modifier: Modifier = Modifier,
    closed: Boolean = false,
    padding: Dp = 20.dp,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = Splitty.colors
    val shape = ReceiptStubShape()
    val paper = if (closed) colors.paperClosed else colors.surface
    Column(
        modifier = modifier
            .fillMaxWidth()
            .drawBehind {
                drawOutline(shape.createOutline(size, layoutDirection, this), paper)
                val y = size.height - 0.5.dp.toPx()
                drawLine(
                    color = colors.dash,
                    start = Offset(8.dp.toPx(), y),
                    end = Offset(size.width - 8.dp.toPx(), y),
                    strokeWidth = 1.dp.toPx(),
                    pathEffect = PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 3.dp.toPx())),
                )
            }
            .padding(padding),
        content = content,
    )
}

// MARK: Линии

enum class ReceiptDividerStyle { DASHED, TOTAL, HAIR }

/** Разделитель внутри чека; декор, в семантику не попадает. */
@Composable
fun ReceiptDivider(modifier: Modifier = Modifier, style: ReceiptDividerStyle = ReceiptDividerStyle.DASHED) {
    val colors = Splitty.colors
    when (style) {
        ReceiptDividerStyle.DASHED -> Box(
            modifier
                .fillMaxWidth()
                .height(1.dp)
                .clearAndSetSemantics {}
                .drawBehind {
                    drawLine(
                        color = colors.dash,
                        start = Offset(0f, size.height / 2),
                        end = Offset(size.width, size.height / 2),
                        strokeWidth = 1.dp.toPx(),
                        pathEffect = PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 3.dp.toPx())),
                    )
                },
        )
        ReceiptDividerStyle.TOTAL -> Box(
            modifier.fillMaxWidth().height(2.dp).background(colors.ruleStrong).clearAndSetSemantics {},
        )
        ReceiptDividerStyle.HAIR -> Box(
            modifier.fillMaxWidth().height(1.dp).background(colors.hairline).clearAndSetSemantics {},
        )
    }
}

// MARK: Строки

/** Позиция чека: слева название и мелкая подпись, справа [trailing]. */
@Composable
fun ReceiptLine(
    title: String,
    modifier: Modifier = Modifier,
    caption: String? = null,
    trailing: @Composable () -> Unit = {},
) {
    val colors = Splitty.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(vertical = 10.dp)
            .semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(text = title, fontSize = 15.sp, color = colors.ink)
            if (!caption.isNullOrEmpty()) {
                Spacer(Modifier.height(4.dp))
                Text(text = caption, fontSize = 12.sp, color = colors.inkSecondary)
            }
        }
        trailing()
    }
}

/** Итог чека: жирная черта, реквизит слева, крупная сумма справа. */
@Composable
fun ReceiptTotal(label: String, modifier: Modifier = Modifier, amount: @Composable () -> Unit) {
    Column(modifier.fillMaxWidth()) {
        ReceiptDivider(style = ReceiptDividerStyle.TOTAL)
        Spacer(Modifier.height(14.dp))
        Row(
            modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
            verticalAlignment = Alignment.Bottom,
        ) {
            ReceiptLabel(label, Modifier.weight(1f))
            amount()
        }
    }
}

// MARK: Штамп

/**
 * Синий штамп истинного состояния («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК»). Рамка и
 * поворот — декор; текст читается один раз как обычная подпись.
 */
@Composable
fun StatusStamp(text: String, modifier: Modifier = Modifier, angle: Float = -6f) {
    val colors = Splitty.colors
    Box(modifier = modifier.padding(vertical = 6.dp).rotate(angle)) {
        Text(
            text = text,
            modifier = Modifier
                .border(2.dp, colors.stamp, RectangleShape)
                .padding(horizontal = 12.dp, vertical = 7.dp),
            color = colors.stamp,
            fontSize = 12.sp,
            fontWeight = FontWeight.SemiBold,
            fontFamily = FontFamily.Monospace,
            letterSpacing = 1.sp,
            textAlign = TextAlign.Center,
        )
    }
}

// MARK: Текстовая ссылка

/** Вторичное действие чека: подчёркнутый текст («Все балансы», «Рассчитаться ↗»). */
@Composable
fun TextLinkButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    fontSize: TextUnit = 14.sp,
) {
    val colors = Splitty.colors
    Box(
        modifier = modifier
            .defaultMinSize(minHeight = 44.dp)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.CenterStart,
    ) {
        Text(
            text = text,
            color = colors.ink.copy(alpha = if (enabled) 1f else 0.4f),
            fontSize = fontSize,
            fontWeight = FontWeight.Medium,
            textDecoration = TextDecoration.Underline,
        )
    }
}
