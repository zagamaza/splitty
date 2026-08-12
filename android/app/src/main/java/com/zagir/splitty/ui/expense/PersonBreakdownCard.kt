package com.zagir.splitty.ui.expense

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.ui.res.stringResource
import com.zagir.splitty.R
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.core.model.PersonShare
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty
import kotlin.math.max

// «С кого сколько» — порт ios/Splitty/Features/Expense/ReceiptView.swift
// (PersonBreakdownCard). Разбивка itemized-черновика по людям: аватар, имя,
// тонкий бар доли от максимума, сумма и «+N ₽ сбор». Подвал — «Сумма
// распределена полностью» (доли уже сходятся с чеком до рубля — зеркало сервера).

@Composable
fun PersonBreakdownCard(
    shares: List<PersonShare>,
    members: List<User>,
    currency: String,
    modifier: Modifier = Modifier,
    meId: Long? = null,
) {
    val colors = Splitty.colors
    val maxTotal = max(shares.maxOfOrNull { it.total } ?: 1, 1)
    SurfaceCard(modifier = modifier.fillMaxWidth()) {
        SectionHeader(text = stringResource(R.string.breakdown_title), modifier = Modifier.padding(bottom = 6.dp))
        shares.forEachIndexed { index, share ->
            if (index > 0) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(1.dp)
                        .background(colors.hairline),
                )
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 9.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                val user = members.firstOrNull { it.id == share.userId }
                if (user != null) {
                    GradientAvatar(user = user, size = 36.dp)
                } else {
                    Box(
                        modifier = Modifier
                            .size(36.dp)
                            .clip(CircleShape)
                            .background(colors.inkSecondary.copy(alpha = 0.25f)),
                    )
                }
                Column(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    Text(
                        text = personName(share.userId, members, meId),
                        fontSize = 15.sp,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    ShareBar(fraction = share.total.toFloat() / maxTotal.toFloat())
                }
                Spacer(Modifier.width(8.dp))
                Column(horizontalAlignment = Alignment.End) {
                    MoneyText(share.total, role = MoneyRole.NEUTRAL, size = 15.sp, currency = currency)
                    if (share.surchargePart > 0) {
                        Text(
                            text = stringResource(R.string.breakdown_surcharge_part, money(share.surchargePart, currency)),
                            fontSize = 11.sp,
                            fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                            color = colors.inkSecondary,
                        )
                    }
                }
            }
        }
        BalancedLine()
    }
}

/** Тонкий бар доли участника относительно наибольшей доли. */
@Composable
private fun ShareBar(fraction: Float) {
    val colors = Splitty.colors
    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(3.dp),
    ) {
        val h = size.height
        drawRoundRect(
            color = colors.ink.copy(alpha = 0.06f),
            cornerRadius = androidx.compose.ui.geometry.CornerRadius(h / 2f, h / 2f),
        )
        val w = max(size.width * fraction.coerceIn(0f, 1f), 4f)
        drawRoundRect(
            color = colors.accent,
            topLeft = Offset(0f, 0f),
            size = Size(w, h),
            cornerRadius = androidx.compose.ui.geometry.CornerRadius(h / 2f, h / 2f),
        )
    }
}

@Composable
private fun BalancedLine() {
    val colors = Splitty.colors
    Column(modifier = Modifier.fillMaxWidth().padding(top = 6.dp)) {
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(1.dp)
                .background(colors.hairline),
        )
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 10.dp),
            horizontalArrangement = Arrangement.End,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Filled.Check,
                contentDescription = null,
                tint = colors.accent,
                modifier = Modifier.size(11.dp),
            )
            Spacer(Modifier.width(6.dp))
            Text(
                text = stringResource(R.string.breakdown_allocated),
                fontSize = 12.5.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.accent,
            )
        }
    }
}

@Composable
private fun personName(id: Long, members: List<User>, meId: Long?): String {
    val user = members.firstOrNull { it.id == id } ?: return "…"
    return if (id == meId) stringResource(R.string.member_you_suffix, user.displayName) else user.displayName
}
