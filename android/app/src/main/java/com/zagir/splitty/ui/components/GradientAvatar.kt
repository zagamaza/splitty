package com.zagir.splitty.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.layout.ContentScale
import com.zagir.splitty.core.model.User
import com.zagir.splitty.data.AvatarStore

// Порт ios/Splitty/Core/UserAvatarView.swift.

/**
 * Кеш аватаров Telegram для всех [GradientAvatar]; null (превью/тесты) —
 * аватары не грузятся, показывается градиент с инициалами.
 * Провайдится в MainActivity.
 */
val LocalAvatarStore = staticCompositionLocalOf<AvatarStore?> { null }

/**
 * Пастельные пары градиентов (светлый → чуть глубже), подобраны так, чтобы
 * белые инициалы читались; индекс выбирается по id пользователя.
 */
private val AvatarGradients: List<Pair<Color, Color>> = listOf(
    Color(0xFF5EC9A7) to Color(0xFF3AA98F), // мята
    Color(0xFF7FA8EC) to Color(0xFF5B87D8), // голубой
    Color(0xFFA78BDB) to Color(0xFF8B6FC5), // лаванда
    Color(0xFFE58BB0) to Color(0xFFD16A96), // розовый
    Color(0xFFEDA06E) to Color(0xFFDD8452), // персик
    Color(0xFFD9B45B) to Color(0xFFC49A3C), // песочный
    Color(0xFF6BBBDF) to Color(0xFF4A9EC7), // небо
    Color(0xFFE58B7E) to Color(0xFFD16C5D), // коралл
    Color(0xFF9DBE6C) to Color(0xFF82A650), // оливковый
    Color(0xFF8E9DBB) to Color(0xFF7183A6), // серо-синий
)

/**
 * Индекс палитры: биты id перемешиваются (SplitMix64) — иначе «круглые»
 * соседние id (100, 200, 300…) дают один и тот же градиент.
 * Обязан давать те же индексы, что и iOS UserAvatarView.gradientIndex.
 */
internal fun avatarGradientIndex(userId: Long, paletteSize: Int = AvatarGradients.size): Int {
    var x = userId.toULong()
    x = x xor (x shr 30)
    x *= 0xBF58_476D_1CE4_E5B9uL
    x = x xor (x shr 27)
    x *= 0x94D0_49BB_1331_11EBuL
    x = x xor (x shr 31)
    return (x % paletteSize.toULong()).toInt()
}

/** Инициалы: первые буквы первых двух слов displayName; пусто — «?». */
internal fun avatarInitials(displayName: String): String {
    val letters = displayName
        .split(' ')
        .filter { it.isNotBlank() }
        .take(2)
        .map { it.first() }
    return if (letters.isEmpty()) "?" else letters.joinToString("").uppercase()
}

/**
 * Аватар пользователя: круг с мягким градиентом и белыми инициалами.
 * Пастельная пара цветов детерминирована от [User.id] (как в iOS).
 */
@Composable
fun GradientAvatar(
    user: User,
    modifier: Modifier = Modifier,
    size: Dp = 40.dp,
) {
    val pair = AvatarGradients[avatarGradientIndex(user.id)]
    val store = LocalAvatarStore.current
    val avatar = store?.let {
        val images by it.images.collectAsState()
        images[user.id]
    }
    if (store != null) {
        LaunchedEffect(user.id) { store.request(user.id) }
    }
    Box(
        modifier = modifier
            .size(size)
            .clip(CircleShape)
            .background(
                Brush.linearGradient(colors = listOf(pair.first, pair.second))
            ),
        contentAlignment = Alignment.Center,
    ) {
        if (avatar != null) {
            Image(
                bitmap = avatar,
                contentDescription = user.displayName,
                contentScale = ContentScale.Crop,
                modifier = Modifier.size(size),
            )
        } else {
            Text(
                text = avatarInitials(user.displayName),
                color = Color.White,
                fontWeight = FontWeight.SemiBold,
                fontSize = with(androidx.compose.ui.platform.LocalDensity.current) {
                    (size * 0.4f).toSp()
                },
            )
        }
    }
}
