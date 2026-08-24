package com.zagir.splitty.ui.paywall

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.ui.theme.Splitty

/**
 * Счётчик суточного лимита — пять отрывных корешков чека.
 *
 * Сигнатурный элемент экрана оплаты и единственное место, где он позволяет себе
 * быть заметным. Смысл не в украшении: корешок — это ровно одно распознавание,
 * израсходованные оторваны по перфорации, и сколько осталось видно без чтения
 * цифр. Материал взят из мира приложения (`receiptPaper` — та же «бумага чека»,
 * что у карточки расхода), а не придуман для этого экрана.
 *
 * Порт iOS `ReceiptStubsView` — оформление обеих платформ обязано совпадать.
 */
@Composable
fun ReceiptStubs(
    limit: Int,
    used: Int,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    val remaining = (limit - used).coerceAtLeast(0)
    val caption = if (remaining == 0) {
        stringResource(R.string.plus_stubs_none)
    } else {
        stringResource(R.string.plus_stubs_left, remaining, limit)
    }

    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.semantics { contentDescription = caption },
        ) {
            repeat(limit.coerceAtLeast(1)) { index ->
                ReceiptStub(
                    isTornOff = index < used,
                    paper = colors.receiptPaper,
                    accent = colors.accent,
                    hairline = colors.hairline,
                    ink = colors.ink,
                )
            }
        }
        Text(
            text = caption,
            color = colors.inkSecondary,
            fontSize = 14.sp,
            textAlign = TextAlign.Center,
        )
    }
}

/**
 * Один корешок. Оторванный — с рваным краем и без заливки.
 *
 * Зигзаг рисуется с фиксированным шагом, а не случайным: экран перерисовывается
 * на каждое изменение остатка, и «дрожащая» бумага читалась бы как дефект.
 */
@Composable
private fun ReceiptStub(
    isTornOff: Boolean,
    paper: Color,
    accent: Color,
    hairline: Color,
    ink: Color,
) {
    Canvas(modifier = Modifier.size(width = 26.dp, height = 40.dp)) {
        val corner = 4.dp.toPx()
        val tearHeight = 5.dp.toPx()
        val tearStep = 5.dp.toPx()
        val w = size.width
        val h = size.height

        val path = Path().apply {
            moveTo(0f, corner)
            quadraticBezierTo(0f, 0f, corner, 0f)
            lineTo(w - corner, 0f)
            quadraticBezierTo(w, 0f, w, corner)

            if (isTornOff) {
                lineTo(w, h - tearHeight)
                var x = w
                var up = false
                while (x > 0f) {
                    val nextX = (x - tearStep).coerceAtLeast(0f)
                    lineTo(nextX, if (up) h - tearHeight else h)
                    up = !up
                    x = nextX
                }
                lineTo(0f, corner)
            } else {
                lineTo(w, h - corner)
                quadraticBezierTo(w, h, w - corner, h)
                lineTo(corner, h)
                quadraticBezierTo(0f, h, 0f, h - corner)
            }
            close()
        }

        drawPath(path, color = if (isTornOff) ink.copy(alpha = 0.07f) else paper)
        drawPath(
            path,
            color = if (isTornOff) hairline else accent.copy(alpha = 0.35f),
            style = Stroke(width = 1.dp.toPx()),
        )
    }
}
