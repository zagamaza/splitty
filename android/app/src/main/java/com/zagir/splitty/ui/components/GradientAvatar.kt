package com.zagir.splitty.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.zagir.splitty.ui.theme.Splitty
import androidx.compose.ui.layout.ContentScale
import com.zagir.splitty.core.model.User
import com.zagir.splitty.data.AvatarStore

// Порт ios/Splitty/Core/UserAvatarView.swift.

/**
 * Кеш аватаров Telegram для всех [GradientAvatar]; null (превью/тесты) —
 * аватары не грузятся, показываются инициалы.
 * Провайдится в MainActivity.
 */
val LocalAvatarStore = staticCompositionLocalOf<AvatarStore?> { null }

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
 * Аватар пользователя: бумажный круг с инициалами чернилами (как в iOS):
 * цвет в чеке остаётся за действием и штампом.
 */
@Composable
fun GradientAvatar(
    user: User,
    modifier: Modifier = Modifier,
    size: Dp = 40.dp,
    /**
     * Грузить ли фото профиля по [User.id]. false — для НЕчеловеческих аватаров
     * (группа): её id это хэш строки, он попадает в диапазон настоящих telegram
     * id, и запрос по нему рисовал бы группе фото ПОСТОРОННЕГО человека.
     * Так же гейтит загрузку iOS (`UserAvatarView.avatarUserId = nil`).
     */
    loadsPhoto: Boolean = true,
    /** Готовая картинка (фото группы из своего хранилища) — вместо инициалов. */
    photo: ImageBitmap? = null,
) {
    val colors = Splitty.colors
    val store = LocalAvatarStore.current
    val avatar = photo ?: store?.takeIf { loadsPhoto }?.let {
        val images by it.images.collectAsState()
        images[user.id]
    }
    if (store != null && loadsPhoto) {
        LaunchedEffect(user.id) { store.request(user.id) }
    }
    Box(
        modifier = modifier
            .size(size)
            .clip(CircleShape)
            .background(colors.paperClosed)
            .border(1.dp, colors.hairline, CircleShape),
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
                color = colors.ink,
                fontWeight = FontWeight.SemiBold,
                fontSize = with(androidx.compose.ui.platform.LocalDensity.current) {
                    (size * 0.38f).toSp()
                },
            )
        }
    }
}
