package com.zagir.splitty.ui.groups

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty
import java.time.Instant
import java.time.LocalDate
import java.time.YearMonth
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

// Общие помощники экранов пакета ui/groups: аватар группы, русские даты,
// состояния загрузки/ошибки, hairline-разделитель, поле ввода шитов.

// MARK: Аватар группы

/**
 * Стабильный (между запусками) хэш id комнаты — задаёт пару градиента.
 * Обязан совпадать с iOS GroupAvatarView.stableId: полиномиальный хэш 31
 * по unicode-скалярам, обрезанный до неотрицательных 31 бита.
 */
internal fun groupAvatarSeed(roomId: String): Long =
    roomId.fold(0L) { acc, char -> (acc * 31 + char.code) and 0x7FFF_FFFFL }

/**
 * Круглый аватар группы: детерминированный пастельный градиент по хэшу
 * id комнаты и первая буква названия — через общий [GradientAvatar]
 * (тот же стиль, что у аватаров людей; порт iOS GroupAvatarView).
 */
@Composable
internal fun GroupAvatar(
    roomId: String,
    name: String,
    modifier: Modifier = Modifier,
    size: Dp = 44.dp,
) {
    GradientAvatar(
        user = User(id = groupAvatarSeed(roomId), username = null, displayName = name.take(1)),
        modifier = modifier,
        size = size,
    )
}

// MARK: Даты (порт ios/Splitty/Core/DateFmt.swift)

/** Форматтеры дат с русской локалью для экранов групп. */
internal object GroupsDateFmt {
    private val ru = Locale("ru")
    private val dayFormatter = DateTimeFormatter.ofPattern("d", ru)
    private val monthShortFormatter = DateTimeFormatter.ofPattern("MMM", ru)
    private val dayMonthFormatter = DateTimeFormatter.ofPattern("d MMM", ru)
    private val monthYearFormatter = DateTimeFormatter.ofPattern("LLLL yyyy", ru)
    private val fullDateFormatter = DateTimeFormatter.ofPattern("d MMMM yyyy", ru)
    private val monthNameFormatter = DateTimeFormatter.ofPattern("LLLL", ru)

    private fun zoned(instant: Instant) = instant.atZone(ZoneId.systemDefault())

    /** «5» — число месяца (колонка даты операции). */
    fun day(instant: Instant): String = dayFormatter.format(zoned(instant))

    /** «июл» — короткий месяц без точки (колонка даты операции). */
    fun monthShort(instant: Instant): String =
        monthShortFormatter.format(zoned(instant)).replace(".", "")

    /** «5 июл» — день и короткий месяц. */
    fun dayMonth(date: LocalDate): String =
        dayMonthFormatter.format(date).replace(".", "")

    /** «5 июл» — день и короткий месяц по моменту времени. */
    fun dayMonth(instant: Instant): String = dayMonth(zoned(instant).toLocalDate())

    /** «Июль 2026» — заголовок секции месяца. */
    fun monthYear(month: YearMonth): String {
        val raw = monthYearFormatter.format(month)
        return raw.replaceFirstChar { it.uppercase(ru) }
    }

    /** «5 июля 2026» — полная дата карточки операции. */
    fun fullDate(instant: Instant): String = fullDateFormatter.format(zoned(instant))

    /** «июль» — название текущего месяца (плитка «За июль»). */
    fun monthName(date: LocalDate = LocalDate.now()): String = monthNameFormatter.format(date)

    /** «февраль» — название месяца точки графика (аннотация «Динамики»). */
    fun monthName(month: YearMonth): String = monthNameFormatter.format(month)

    /** Фиксированные 3-буквенные русские месяцы — подписи оси «Динамики». */
    private val monthsShort3 = arrayOf(
        "янв", "фев", "мар", "апр", "май", "июн",
        "июл", "авг", "сен", "окт", "ноя", "дек",
    )

    /** «фев» — короткий месяц оси графика (ровно 3 буквы, без точек). */
    fun monthShort3(month: YearMonth): String = monthsShort3[month.monthValue - 1]

    /** «пн»…«вс» — подписи оси «По дням недели» (индекс 0 — понедельник). */
    val weekdaysShort = listOf("пн", "вт", "ср", "чт", "пт", "сб", "вс")

    /** Месяц операции в локальном времени (группировка по месяцам). */
    fun yearMonth(instant: Instant): YearMonth = YearMonth.from(zoned(instant))
}

/** «1 участник», «2 участника», «5 участников». */
@Composable
internal fun memberCountText(count: Int): String =
    pluralStringResource(R.plurals.groups_member_count, count, count)

// MARK: Состояния

/** Центрированный спиннер первичной загрузки. */
@Composable
internal fun GroupsLoading(modifier: Modifier = Modifier) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Splitty.colors.accent)
    }
}

/** Ошибка загрузки: иконка, заголовок, сообщение и чип «Повторить». */
@Composable
internal fun GroupsErrorState(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    FailedState(
        message = message,
        onRetry = onRetry,
        modifier = modifier.fillMaxWidth(),
    )
}

/** Ошибка на весь экран (внутри Scaffold-контента). */
@Composable
internal fun GroupsFullScreenError(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(modifier = modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        GroupsErrorState(message = message, onRetry = onRetry)
    }
}

/** Дружелюбное пустое состояние карточкой (вместо системного списка). */
@Composable
internal fun GroupsEmptyCard(
    icon: ImageVector,
    title: String,
    subtitle: String,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = modifier.fillMaxWidth(), padding = 24.dp) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = colors.inkSecondary,
                modifier = Modifier.size(36.dp),
            )
            Spacer(Modifier.height(10.dp))
            Text(
                text = title,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = subtitle,
                fontSize = 14.sp,
                color = colors.inkSecondary,
                textAlign = TextAlign.Center,
            )
        }
    }
}

// MARK: Мелкие детали

/** Тонкий разделитель между строками внутри карточки. */
@Composable
internal fun HairlineDivider(startIndent: Dp = 0.dp) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = startIndent)
            .height(1.dp)
            .background(Splitty.colors.hairline),
    )
}

/** Тихий chevron «вперёд» в конце интерактивной строки. */
@Composable
internal fun ChevronIcon(modifier: Modifier = Modifier) {
    Icon(
        imageVector = Icons.Filled.ChevronRight,
        contentDescription = null,
        tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
        modifier = modifier.size(18.dp),
    )
}

/** Единый алерт ошибки: показывается, пока [message] != null. */
@Composable
internal fun GroupsAlertDialog(message: String?, onDismiss: () -> Unit) {
    if (message == null) return
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.common_ok))
            }
        },
        title = { Text(stringResource(R.string.common_error_title)) },
        text = { Text(message) },
    )
}

/**
 * Поле ввода шитов группы: подложка цвета фона экрана + hairline-бордер
 * (тот же рисунок, что у поля экрана входа).
 */
@Composable
internal fun GroupsTextField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
) {
    val colors = Splitty.colors
    val shape = RoundedCornerShape(12.dp)
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier
            .fillMaxWidth()
            .background(colors.bg, shape)
            .border(1.dp, colors.hairline, shape)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        textStyle = TextStyle(color = colors.ink, fontSize = 17.sp),
        singleLine = true,
        cursorBrush = SolidColor(colors.accent),
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        decorationBox = { innerTextField ->
            Box {
                if (value.isEmpty()) {
                    Text(
                        text = placeholder,
                        fontSize = 17.sp,
                        color = colors.inkSecondary,
                        maxLines = 1,
                    )
                }
                innerTextField()
            }
        },
    )
}
