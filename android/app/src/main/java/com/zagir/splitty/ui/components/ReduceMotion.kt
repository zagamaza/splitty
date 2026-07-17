package com.zagir.splitty.ui.components

import android.provider.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

// Аналог iOS `@Environment(\.accessibilityReduceMotion)`. У Android нет прямого
// «reduce motion» — общесистемный сигнал «выключить анимации» это Animator
// Duration Scale = 0 (Настройки для разработчиков / доступность на части OEM).
// Тем же флагом гейтятся spring/scale в DS, нуджи и numericText MoneyText.

/** Чистая проверка: масштаб длительности аниматора 0 → анимации выключены. */
fun isReduceMotion(animatorDurationScale: Float): Boolean = animatorDurationScale == 0f

/**
 * true, если пользователь выключил анимации (Animator Duration Scale = 0).
 * Читаем один раз на композицию: смена настройки требует пересборки экрана —
 * приемлемо (флаг меняют редко, при отладке).
 */
@Composable
fun rememberReduceMotion(): Boolean {
    val context = LocalContext.current
    return remember(context) {
        val scale = Settings.Global.getFloat(
            context.contentResolver,
            Settings.Global.ANIMATOR_DURATION_SCALE,
            1f,
        )
        isReduceMotion(scale)
    }
}
