package com.zagir.splitty.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

// Семантические токены премиум-палитры «финтех-минимализм» — порт
// ios/Splitty/Core/Theme.swift. 90% интерфейса — нейтральные bg/surface/ink;
// цвет (accent/negative) — только смысловой: CTA, суммы, активные состояния.
// В экранах запрещены сырые hex — только Splitty.colors.* и MaterialTheme.

/** Набор семантических цветов текущей темы (light/dark). */
@Immutable
data class SplittyColors(
    /** Фон всех экранов (вместо системного). */
    val bg: Color,
    /** Фон карточек и «плавающих» поверхностей (см. SurfaceCard). */
    val surface: Color,
    /** Основной текст. */
    val ink: Color,
    /** Вторичный текст: подписи, даты, «расчёт», нулевые суммы. */
    val inkSecondary: Color,
    /** Акцент (изумруд): CTA, позитивные суммы («вам должны»), активный таб. */
    val accent: Color,
    /** Нажатое состояние акцента (тёмный изумруд): pressed CTA, градиенты. */
    val accentPressed: Color,
    /** Негатив (приглушённый коралл): долги, «вы должны». */
    val negative: Color,
    /**
     * Акцент ДЛЯ ТЕКСТА мелким кеглем.
     *
     * [accent] на белом даёт 3.39:1 — этого хватает крупной сумме и заливке
     * кнопки, но не подписи в 12–15 sp: минимум для такого текста 4.5:1.
     * В тёмной теме совпадает с [accent] — там контраст и так проходит.
     */
    val accentText: Color,
    /** Негатив ДЛЯ ТЕКСТА мелким кеглем: [negative] на белом даёт 3.79:1. */
    val negativeText: Color,
    /** Тонкие разделители внутри карточек и hairline-бордеры в тёмной теме. */
    val hairline: Color,
    /**
     * Бумага чек-карточки (ReceiptCard): чуть тёплый off-white в light и
     * приподнятый над surface оттенок в dark — «настоящая» квитанция, а не
     * обычная карточка. Порт iOS `Color.receiptPaper`.
     */
    val receiptPaper: Color,
    /**
     * Заливка баров графиков (дашборд «Итоги»). Отдельный от UI-акцента цвет
     * данных: в dark — валидированный для заливок на тёмной поверхности
     * #0EA97A (UI-accent #34D399 для баров НЕ использовать).
     */
    val chartAccent: Color,
    /**
     * Категориальная палитра участников (дашборд «Итоги»). Порядок ФИКСИРОВАН,
     * пары light/dark валидированы для заливок на surface. Цвет назначается по
     * возрастанию user.id (см. memberColorIndices); один человек — один цвет во
     * всех графиках. Седьмой участник и дальше — inkSecondary: палитру НЕ циклим.
     */
    val chartCategorical: List<Color>,
    val isDark: Boolean,
)

/** Светлая палитра для тестов контраста (сама палитра приватна). */
internal fun splittyLightColorsForTest(): SplittyColors = LightColors

private val LightColors = SplittyColors(
    bg = Color(0xFFF6F7F9),
    surface = Color(0xFFFFFFFF),
    ink = Color(0xFF101828),
    inkSecondary = Color(0xFF667085),
    accent = Color(0xFF0E9F6E),
    accentPressed = Color(0xFF0B7C56),
    negative = Color(0xFFDC5A2E),
    accentText = Color(0xFF0A6E4C),
    negativeText = Color(0xFFAF3F1C),
    hairline = Color(0xFFEAECF0),
    receiptPaper = Color(0xFFFDFCF9),
    chartAccent = Color(0xFF0E9F6E),
    chartCategorical = listOf(
        Color(0xFF0E9F6E), // 1 изумруд
        Color(0xFFD97706), // 2 янтарь
        Color(0xFF2F6FE4), // 3 синий
        Color(0xFFDB2777), // 4 розовый
        Color(0xFF0891B2), // 5 циан
        Color(0xFF8B5CF6), // 6 фиолетовый
    ),
    isDark = false,
)

private val DarkColors = SplittyColors(
    bg = Color(0xFF0C0F13),
    surface = Color(0xFF171C23),
    ink = Color(0xFFF2F4F7),
    inkSecondary = Color(0xFF98A2B3),
    accent = Color(0xFF34D399),
    accentPressed = Color(0xFF2BB985),
    negative = Color(0xFFFB923C),
    accentText = Color(0xFF34D399),
    negativeText = Color(0xFFFB923C),
    hairline = Color(0xFF232A33),
    receiptPaper = Color(0xFF1B2129),
    chartAccent = Color(0xFF0EA97A),
    chartCategorical = listOf(
        Color(0xFF0EA97A), // 1 изумруд
        Color(0xFFC77D08), // 2 янтарь
        Color(0xFF4478DB), // 3 синий
        Color(0xFFC94E7F), // 4 розовый
        Color(0xFF0E8FA8), // 5 циан
        Color(0xFF8E6BE0), // 6 фиолетовый
    ),
    isDark = true,
)

private val LocalSplittyColors = staticCompositionLocalOf { LightColors }

/** Точка доступа к токенам: `Splitty.colors.accent` и т.п. */
object Splitty {
    val colors: SplittyColors
        @Composable
        @ReadOnlyComposable
        get() = LocalSplittyColors.current
}

/** M3 ColorScheme из токенов — для системных компонентов (поля, диалоги…). */
private fun SplittyColors.toColorScheme() = if (isDark) {
    darkColorScheme(
        primary = accent,
        onPrimary = Color.White,
        primaryContainer = accentPressed,
        onPrimaryContainer = Color.White,
        secondary = inkSecondary,
        onSecondary = bg,
        background = bg,
        onBackground = ink,
        surface = surface,
        onSurface = ink,
        surfaceVariant = surface,
        onSurfaceVariant = inkSecondary,
        surfaceContainer = surface,
        surfaceContainerLow = surface,
        surfaceContainerHigh = surface,
        surfaceContainerHighest = surface,
        error = negative,
        onError = Color.White,
        outline = hairline,
        outlineVariant = hairline,
    )
} else {
    lightColorScheme(
        primary = accent,
        onPrimary = Color.White,
        primaryContainer = accentPressed,
        onPrimaryContainer = Color.White,
        secondary = inkSecondary,
        onSecondary = Color.White,
        background = bg,
        onBackground = ink,
        surface = surface,
        onSurface = ink,
        surfaceVariant = surface,
        onSurfaceVariant = inkSecondary,
        surfaceContainer = surface,
        surfaceContainerLow = surface,
        surfaceContainerHigh = surface,
        surfaceContainerHighest = surface,
        error = negative,
        onError = Color.White,
        outline = hairline,
        outlineVariant = hairline,
    )
}

/**
 * Тема приложения: семантические токены + M3 ColorScheme из них.
 * Dynamic color (Material You) СОЗНАТЕЛЬНО выключен — палитра фиксированная,
 * как в iOS-приложении.
 */
@Composable
fun SplittyTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val colors = if (darkTheme) DarkColors else LightColors
    CompositionLocalProvider(LocalSplittyColors provides colors) {
        MaterialTheme(
            colorScheme = colors.toColorScheme(),
            content = content,
        )
    }
}
