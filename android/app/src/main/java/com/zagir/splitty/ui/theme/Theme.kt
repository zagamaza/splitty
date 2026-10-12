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

// Семантические токены «Общего чека» — порт ios/Splitty/Core/Theme.swift.
// Туса — общий чек: тёплый стол `bg`, бумага `surface`, чернила `ink`. Цвет —
// только смысловой: оранжевое действие (`accent`), синий штамп (`stamp`), знак
// денег (`positive`/`negative`). Направление долга говорится словами.
// В экранах запрещены сырые hex — только Splitty.colors.* и MaterialTheme.

/** Набор семантических цветов текущей темы (light/dark). */
@Immutable
data class SplittyColors(
    /** Стол под чеком — фон всех экранов. */
    val bg: Color,
    /** Бумага: поверхность документа, карточек и листов. */
    val surface: Color,
    /** Бумага закрытого счёта (архив, неактивное). */
    val paperClosed: Color,
    /** Основной текст. */
    val ink: Color,
    /** Вторичный текст: подписи, даты, «расчёт», нулевые суммы. */
    val inkSecondary: Color,
    /** Действие: заливка основной кнопки, активные элементы. Текст на ней — [onAccent]. */
    val accent: Color,
    /** Текст и значки поверх [accent]. */
    val onAccent: Color,
    /** Позитив: «вам должны», «получено». */
    val positive: Color,
    /** Пунктир между частями чека. */
    val dash: Color,
    /** Жирная итоговая черта над «ВСЕГО». */
    val ruleStrong: Color,
    /** Синий штамп состояния («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК»). */
    val stamp: Color,
    /** Подложка записи голоса: тёмная лента под белым текстом расшифровки. */
    val recordingScrim: Color,
    /** Нажатое состояние действия. */
    val accentPressed: Color,
    /** Негатив: «вы должны», ошибки. */
    val negative: Color,
    /** Действие ДЛЯ ТЕКСТА мелким кеглем: на столе и бумаге ≥ 4.5:1. */
    val accentText: Color,
    /** Негатив ДЛЯ ТЕКСТА мелким кеглем. */
    val negativeText: Color,
    /** Тонкие линии: разделители строк, рамки полей. */
    val hairline: Color,
    /** Бумага чека — та же, что [surface]: весь документ одна поверхность. */
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

/** Палитры для тестов контраста (сами палитры приватны). */
internal fun splittyLightColorsForTest(): SplittyColors = LightColors
internal fun splittyDarkColorsForTest(): SplittyColors = DarkColors

private val LightColors = SplittyColors(
    bg = Color(0xFFDEDBD2),
    surface = Color(0xFFFFFDF4),
    paperClosed = Color(0xFFE9E8DD),
    ink = Color(0xFF262722),
    inkSecondary = Color(0xFF5E5E54),
    accent = Color(0xFFC4502D),
    onAccent = Color(0xFFFFFDF4),
    positive = Color(0xFF2E6B45),
    dash = Color(0xFFAAA999),
    ruleStrong = Color(0xFF2A2D24),
    stamp = Color(0xFF36579C),
    recordingScrim = Color(0xFF262722),
    accentPressed = Color(0xFFA6421F),
    negative = Color(0xFFA23B2A),
    accentText = Color(0xFF9E3F1D),
    negativeText = Color(0xFFA23B2A),
    hairline = Color(0xFFCBC9BC),
    receiptPaper = Color(0xFFFFFDF4),
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
    bg = Color(0xFF1A1B16),
    surface = Color(0xFF26271F),
    paperClosed = Color(0xFF202119),
    ink = Color(0xFFECEADF),
    inkSecondary = Color(0xFFA7A595),
    accent = Color(0xFFC4502D),
    onAccent = Color(0xFFFFFDF4),
    positive = Color(0xFF8CCB9F),
    dash = Color(0xFF5A5B50),
    ruleStrong = Color(0xFFECEADF),
    stamp = Color(0xFF8FA9E0),
    recordingScrim = Color(0xFF0F100C),
    accentPressed = Color(0xFFA6421F),
    negative = Color(0xFFF08A6E),
    accentText = Color(0xFFE57A55),
    negativeText = Color(0xFFF08A6E),
    hairline = Color(0xFF3A3B32),
    receiptPaper = Color(0xFF26271F),
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
        onPrimary = onAccent,
        primaryContainer = accentPressed,
        onPrimaryContainer = onAccent,
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
        onError = onAccent,
        outline = hairline,
        outlineVariant = hairline,
    )
} else {
    lightColorScheme(
        primary = accent,
        onPrimary = onAccent,
        primaryContainer = accentPressed,
        onPrimaryContainer = onAccent,
        secondary = inkSecondary,
        onSecondary = surface,
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
        onError = onAccent,
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
