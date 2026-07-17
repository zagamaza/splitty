package com.zagir.splitty.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.WifiOff
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.ui.theme.Splitty

// Порт ios/Splitty/Core/Components.swift → FailedStateView. Единое состояние
// «не удалось загрузить» с кнопкой «Повторить»: раньше по экранам жили 5 копий
// в двух стилях (кнопка Button vs чип SoftChip) — теперь один вид.

/**
 * Failed-состояние первичной загрузки: иконка «нет сети», заголовок, [message]
 * и чип «Повторить». Сам себя НЕ центрирует — вызывающий кладёт в Box/Column.
 */
@Composable
fun FailedState(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    Column(
        modifier = modifier.padding(horizontal = 32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.WifiOff,
            contentDescription = null,
            modifier = Modifier
                .padding(bottom = 4.dp)
                .size(40.dp),
            tint = colors.inkSecondary,
        )
        Text(
            text = stringResource(R.string.common_load_failed),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
        )
        Text(
            text = message,
            fontSize = 15.sp,
            color = colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
        SoftChip(text = stringResource(R.string.common_retry), onClick = onRetry)
    }
}
