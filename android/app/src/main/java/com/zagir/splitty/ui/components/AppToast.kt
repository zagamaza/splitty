package com.zagir.splitty.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.ui.theme.Splitty
import kotlinx.coroutines.delay

// Порт ios/Splitty/Features/Expense/AddExpenseView.swift → toastView. Тост-
// подтверждение внизу («Саня — это Александр. Запомнил»): галочка, тёмная
// подложка ink, автогашение через 2.8с. Гасится и выполнением подсказки —
// вызывающий сбрасывает message раньше таймера.

private const val TOAST_AUTO_DISMISS_MS = 2_800L

/** Чистая визуальная «пилюля» тоста без таймера — удобно для снапшотов. */
@Composable
fun AppToastPill(text: String, modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Row(
        modifier = modifier
            .clip(RoundedCornerShape(16.dp))
            .background(colors.ink)
            .padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.CheckCircle,
            contentDescription = null,
            modifier = Modifier.size(16.dp),
            tint = colors.accent,
        )
        Text(
            text = text,
            fontSize = 14.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.bg,
        )
    }
}

/**
 * Оверлей-тост поверх контента: показывает непустой [message] снизу и сам гасит
 * его через 2.8с вызовом [onDismiss]. LaunchedEffect ключуется по тексту —
 * смена/сброс сообщения перезапускает (или отменяет) таймер, тост не съедает
 * ДРУГОЙ уже показанный. Хит-тесты не перехватывает.
 */
@Composable
fun AppToast(
    message: String?,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    bottomPadding: Dp = 90.dp,
) {
    LaunchedEffect(message) {
        if (message != null) {
            delay(TOAST_AUTO_DISMISS_MS)
            onDismiss()
        }
    }
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.BottomCenter) {
        AnimatedVisibility(
            visible = message != null,
            enter = fadeIn() + slideInVertically { it / 2 },
            exit = fadeOut() + slideOutVertically { it / 2 },
        ) {
            AppToastPill(
                text = message ?: "",
                modifier = Modifier.padding(horizontal = 20.dp).padding(bottom = bottomPadding),
            )
        }
    }
}
