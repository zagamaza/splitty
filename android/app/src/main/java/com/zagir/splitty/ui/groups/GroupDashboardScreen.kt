package com.zagir.splitty.ui.groups

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ReceiptLong
import androidx.compose.material.icons.filled.BarChart
import androidx.compose.material.icons.outlined.AccountBalanceWallet
import androidx.compose.material.icons.outlined.Calculate
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PieChart
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.DailySum
import com.zagir.splitty.core.model.MemberSum
import com.zagir.splitty.core.model.MonthlySum
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyColors
import java.time.LocalDate
import kotlin.math.abs
import kotlin.math.max

// Дашборд «Итоги» v2 (паритет с iOS) БЕЗ сторонних библиотек — Canvas/Row.
// Состав: плитки 2×2 → «Динамика по месяцам» → «Траты по дням» → донат
// «Кто платил» → бары «Чья доля» → diverging «Баланс участников» →
// «По дням недели» → «Топ расходов». Правила дата-виза: текст только
// ink/inkSecondary, сетка тише данных; личный цвет участника — из
// chartCategorical по возрастанию user.id, один во всех графиках, палитра
// НЕ циклится (7-й и дальше — inkSecondary, в донате — «Прочие»).
// Все суммы — в statistics.currency; подготовка данных — GroupDashboardCharts.kt.

/** Личный цвет участника по индексу назначения; вне палитры — inkSecondary. */
private fun SplittyColors.memberColor(index: Int?): Color =
    index?.let { chartCategorical.getOrNull(it) } ?: inkSecondary

/**
 * Полноэкранный вариант дашборда «Итоги» (роут из экрана группы).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GroupDashboardScreen(
    roomId: String,
    onBack: () -> Unit,
    viewModel: GroupDashboardViewModel = hiltViewModel(),
) {
    LaunchedEffect(roomId) { viewModel.start(roomId) }
    val state by viewModel.statistics.collectAsStateWithLifecycle()
    val meId by viewModel.meId.collectAsStateWithLifecycle()
    val colors = Splitty.colors

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(R.string.totals_title),
                        fontWeight = FontWeight.Bold,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.common_back),
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                    navigationIconContentColor = colors.ink,
                ),
            )
        },
    ) { innerPadding ->
        GroupDashboardContent(
            state = state,
            meId = meId,
            onRetry = viewModel::retry,
            modifier = Modifier
                .padding(innerPadding)
                .fillMaxSize(),
        )
    }
}

/**
 * Дашборд bottom sheet'ом — открывается чипом «Итоги» экрана группы
 * (аналог iOS sheet GroupTotalsView).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun GroupDashboardSheet(roomId: String, onDismiss: () -> Unit) {
    val viewModel: GroupDashboardViewModel = hiltViewModel()
    LaunchedEffect(roomId) { viewModel.start(roomId) }
    val state by viewModel.statistics.collectAsStateWithLifecycle()
    val meId by viewModel.meId.collectAsStateWithLifecycle()
    val colors = Splitty.colors

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.bg,
    ) {
        Column(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.totals_title),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
            GroupDashboardContent(
                state = state,
                meId = meId,
                onRetry = viewModel::retry,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Содержимое дашборда: плитки, график по дням, бары участников, топ-5. */
@Composable
internal fun GroupDashboardContent(
    state: UiState<Statistics>,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    meId: Long? = null,
) {
    when (state) {
        UiState.Loading -> Box(
            modifier = modifier.padding(vertical = 56.dp),
            contentAlignment = Alignment.Center,
        ) {
            CircularProgressIndicator(color = Splitty.colors.accent)
        }

        is UiState.Error -> Box(modifier = modifier, contentAlignment = Alignment.Center) {
            GroupsErrorState(message = state.message, onRetry = onRetry)
        }

        is UiState.Content -> {
            val stats = state.value
            if (stats.totalSpent == 0 && stats.topOperations.isEmpty()) {
                // Пустая комната: дружелюбное состояние вместо нулевых графиков.
                Box(modifier = modifier, contentAlignment = Alignment.Center) {
                    DashboardEmptyState()
                }
            } else {
                Column(
                    modifier = modifier
                        .verticalScroll(rememberScrollState())
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    StatTiles(stats)
                    MyTiles(stats, meId)
                    DailySpendingCard(byDay = stats.byDay, currency = stats.currency)
                    // Личные цвета участников — единые для доната и «Чьей доли».
                    val colorIndices = remember(stats) {
                        memberColorIndices(
                            (stats.paidByMember + stats.shareByMember).map { it.user.id }
                        )
                    }
                    val paid = remember(stats) { preparedMemberBars(stats.paidByMember) }
                    if (paid.isNotEmpty()) {
                        WhoPaidDonutCard(
                            bars = paid,
                            colorIndices = colorIndices,
                            totalSpent = stats.totalSpent,
                            currency = stats.currency,
                        )
                    }
                    val shares = remember(stats) { preparedMemberBars(stats.shareByMember) }
                    if (shares.isNotEmpty()) {
                        MemberBarsCard(
                            title = stringResource(R.string.totals_whose_share),
                            bars = shares,
                            currency = stats.currency,
                            colorIndices = colorIndices,
                        )
                    }
                    val nets = remember(stats) {
                        memberNetBalances(stats.paidByMember, stats.shareByMember)
                    }
                    if (nets.isNotEmpty()) {
                        MemberBalanceCard(nets = nets, currency = stats.currency)
                    }
                    val weekdays = remember(stats) { weekdayTotals(stats.byDay) }
                    if (weekdays.any { it > 0 }) {
                        WeekdayCard(totals = weekdays, currency = stats.currency)
                    }
                    if (stats.topOperations.isNotEmpty()) {
                        TopOperationsCard(stats)
                    }
                }
            }
        }
    }
}

@Composable
private fun DashboardEmptyState() {
    val colors = Splitty.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            imageVector = Icons.Filled.BarChart,
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = stringResource(R.string.totals_empty_title),
            fontSize = 16.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
        )
        Spacer(Modifier.height(4.dp))
        Text(
            text = stringResource(R.string.totals_empty_subtitle),
            fontSize = 14.sp,
            color = colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
    }
}

// MARK: - Стат-плитки 2×2

/**
 * Личные плитки: «Я заплатил» (донор операций) и «Моя доля» (по хранимым
 * долям получателей) — ответ на «сколько я потратил именно в этой тусе».
 * Не показываются, пока профиль не загружен или обе суммы нулевые.
 */
@Composable
private fun MyTiles(stats: Statistics, meId: Long?) {
    meId ?: return
    val paid = stats.paidByMember.firstOrNull { it.user.id == meId }?.sum ?: 0
    val share = stats.shareByMember.firstOrNull { it.user.id == meId }?.sum ?: 0
    if (paid == 0 && share == 0) return
    val colors = Splitty.colors
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        StatTile(
            title = stringResource(R.string.totals_i_paid),
            icon = Icons.Outlined.Person,
            iconTint = colors.accent,
            modifier = Modifier.weight(1f),
        ) {
            MoneyText(
                paid,
                role = MoneyRole.NEUTRAL,
                size = 28.sp,
                currency = stats.currency,
                minScale = 0.55f,
            )
        }
        StatTile(
            title = stringResource(R.string.totals_my_share),
            icon = Icons.Outlined.PieChart,
            iconTint = colors.accent,
            modifier = Modifier.weight(1f),
        ) {
            MoneyText(
                share,
                role = MoneyRole.NEUTRAL,
                size = 28.sp,
                currency = stats.currency,
                minScale = 0.55f,
            )
        }
    }
}

/**
 * Плитки 2×2: «Всего потрачено», «За <месяц>», «Операций», «Средний чек»
 * (totalSpent / operationCount; 0 при пустоте). Иконка у заголовка —
 * декоративная, цвета chartCategorical по порядку плиток.
 */
@Composable
private fun StatTiles(stats: Statistics) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            StatTile(
                title = stringResource(R.string.totals_total_spent),
                icon = Icons.Outlined.AccountBalanceWallet,
                iconTint = colors.accent,
                modifier = Modifier.weight(1f),
            ) {
                MoneyText(
                    stats.totalSpent,
                    role = MoneyRole.NEUTRAL,
                    size = 28.sp,
                    currency = stats.currency,
                    minScale = 0.55f,
                )
            }
            StatTile(
                title = stringResource(R.string.totals_for_month, GroupsDateFmt.monthName()),
                icon = Icons.Outlined.CalendarMonth,
                iconTint = colors.accent,
                modifier = Modifier.weight(1f),
            ) {
                MoneyText(
                    stats.monthSpent,
                    role = MoneyRole.NEUTRAL,
                    size = 28.sp,
                    currency = stats.currency,
                    minScale = 0.55f,
                )
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            StatTile(
                title = stringResource(R.string.totals_operation_count),
                icon = Icons.AutoMirrored.Outlined.ReceiptLong,
                iconTint = colors.accent,
                modifier = Modifier.weight(1f),
            ) {
                // Счётчик — не деньги: обычное число теми же tabular-цифрами.
                Text(
                    text = stats.operationCount.toString(),
                    fontSize = 28.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                    maxLines = 1,
                    style = TextStyle(fontFeatureSettings = "tnum"),
                )
            }
            StatTile(
                title = stringResource(R.string.totals_avg_check),
                icon = Icons.Outlined.Calculate,
                iconTint = colors.accent,
                modifier = Modifier.weight(1f),
            ) {
                MoneyText(
                    averageCheck(stats.totalSpent, stats.operationCount),
                    role = MoneyRole.NEUTRAL,
                    size = 28.sp,
                    currency = stats.currency,
                    minScale = 0.55f,
                )
            }
        }
    }
}

@Composable
private fun StatTile(
    title: String,
    icon: ImageVector,
    iconTint: Color,
    modifier: Modifier = Modifier,
    value: @Composable () -> Unit,
) {
    SurfaceCard(modifier = modifier, padding = 16.dp) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null, // декоративная
                tint = iconTint,
                modifier = Modifier.size(14.dp),
            )
            SectionHeader(title, maxLines = 1)
        }
        Spacer(Modifier.height(6.dp))
        value()
    }
}

// MARK: - Общий бейдж-аннотация графиков

/** Аннотация выбранного бара: «5 июл — 1 200 ₽» на карточке-подложке. */
@Composable
private fun ChartAnnotationBadge(label: String, value: String) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(colors.bg)
            .border(1.dp, colors.hairline, RoundedCornerShape(8.dp))
            .padding(horizontal = 8.dp, vertical = 5.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            fontSize = 12.sp,
            color = colors.inkSecondary,
        )
        Text(
            text = value,
            fontSize = 12.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
    }
}

// MARK: - «Траты по дням» (Canvas, 30 дней)

/** Точка графика: день и сумма трат (дни без трат — нули). */
internal data class DayPoint(val date: LocalDate, val sum: Int)

/**
 * Ряд из ровно 30 дней (по сегодняшний): дни без трат = 0;
 * даты бэкенда «2026-07-05», нераспознанные строки пропускаются.
 */
internal fun lastThirtyDays(
    byDay: List<DailySum>,
    today: LocalDate = LocalDate.now(),
): List<DayPoint> {
    val sums = HashMap<LocalDate, Int>()
    for (daily in byDay) {
        val date = runCatching { LocalDate.parse(daily.date) }.getOrNull() ?: continue
        sums[date] = (sums[date] ?: 0) + daily.sum
    }
    return (29 downTo 0).map { offset ->
        val date = today.minusDays(offset.toLong())
        DayPoint(date = date, sum = sums[date] ?: 0)
    }
}

/**
 * Столбиковый график трат за 30 дней: бары chartAccent со скруглением 4dp,
 * тихая hairline-сетка, подписи дат каждые 7 дней; тап по бару — аннотация
 * «дата — сумма» (повторный тап снимает выбор).
 */
@Composable
private fun DailySpendingCard(byDay: List<DailySum>, currency: String) {
    val colors = Splitty.colors
    val points = remember(byDay) { lastThirtyDays(byDay) }
    var selectedIndex by remember(byDay) { mutableStateOf<Int?>(null) }
    val textMeasurer = rememberTextMeasurer()
    val labelStyle = TextStyle(fontSize = 11.sp, color = colors.inkSecondary)
    val allZero = points.all { it.sum == 0 }

    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(stringResource(R.string.totals_by_day))
        Spacer(Modifier.height(8.dp))

        // Аннотация выбранного бара; высота зарезервирована — layout не прыгает.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(30.dp),
            contentAlignment = Alignment.Center,
        ) {
            selectedIndex?.let { index ->
                val point = points[index]
                ChartAnnotationBadge(
                    label = "${GroupsDateFmt.dayMonth(point.date)} —",
                    value = money(point.sum, currency),
                )
            }
        }
        Spacer(Modifier.height(4.dp))

        val dailyChartLabel = stringResource(R.string.totals_chart_daily_a11y)
        Box {
            Canvas(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(176.dp)
                    .semantics { contentDescription = dailyChartLabel }
                    .pointerInput(points) {
                        detectTapGestures { offset ->
                            val slot = size.width / points.size.toFloat()
                            val index = (offset.x / slot).toInt().coerceIn(0, points.size - 1)
                            selectedIndex = if (selectedIndex == index) null else index
                        }
                    },
            ) {
                val labelArea = 18.dp.toPx()
                val chartHeight = size.height - labelArea
                val maxSum = max(points.maxOf { it.sum }, 1)

                // Сетка тише данных: hairline-линии на 0, ⅓, ⅔ и полной высоте.
                for (step in 0..3) {
                    val y = chartHeight * (1f - step / 3f)
                    drawLine(
                        color = colors.hairline,
                        start = Offset(0f, y),
                        end = Offset(size.width, y),
                        strokeWidth = 1f,
                    )
                }

                val slotWidth = size.width / points.size
                val barWidth = slotWidth * 0.55f
                points.forEachIndexed { index, point ->
                    if (point.sum <= 0) return@forEachIndexed
                    val barHeight = chartHeight * point.sum / maxSum
                    drawRoundRect(
                        color = colors.chartAccent,
                        topLeft = Offset(
                            x = slotWidth * index + (slotWidth - barWidth) / 2f,
                            y = chartHeight - barHeight,
                        ),
                        size = Size(barWidth, barHeight),
                        cornerRadius = CornerRadius(4.dp.toPx()),
                    )
                }

                // Тонкая линия выбора над баром аннотации.
                selectedIndex?.let { index ->
                    val x = slotWidth * index + slotWidth / 2f
                    drawLine(
                        color = colors.inkSecondary.copy(alpha = 0.3f),
                        start = Offset(x, 0f),
                        end = Offset(x, chartHeight),
                        strokeWidth = 1.dp.toPx(),
                    )
                }

                // Подписи дат каждые 7 дней (11sp, вторичный цвет — ось тише данных).
                for (index in points.indices step 7) {
                    val layout = textMeasurer.measure(
                        AnnotatedString(GroupsDateFmt.dayMonth(points[index].date)),
                        labelStyle,
                    )
                    val centerX = slotWidth * index + slotWidth / 2f
                    val textX = (centerX - layout.size.width / 2f)
                        .coerceIn(0f, max(0f, size.width - layout.size.width))
                    drawText(layout, topLeft = Offset(textX, chartHeight + 4.dp.toPx()))
                }
            }
            if (allZero) {
                Text(
                    text = stringResource(R.string.totals_no_recent_spending),
                    fontSize = 13.sp,
                    color = colors.inkSecondary,
                    modifier = Modifier.align(Alignment.Center),
                )
            }
        }
    }
}

// MARK: - «Кто платил» (донат + легенда)

/**
 * Донат «Кто платил»: кольцо drawArc stroke-шириной 26dp с зазором 1.5°
 * между сегментами, личные цвета участников; >6 плательщиков — топ-5 +
 * серый сегмент «Прочие». В центре — totalSpent; легенда обязательна:
 * точка 10dp, имя (ink), сумма и процент (inkSecondary), по убыванию.
 */
@Composable
private fun WhoPaidDonutCard(
    bars: List<MemberBar>,
    colorIndices: Map<Long, Int>,
    totalSpent: Int,
    currency: String,
) {
    val colors = Splitty.colors
    val (visible, othersSum) = remember(bars) { foldDonutBars(bars) }
    val total = max(bars.sumOf { it.sum }, 1)
    val othersLabel = stringResource(R.string.totals_others)
    // Сегменты доната и строки легенды — один список (подпись, сумма, цвет).
    val segments = remember(bars, colors) {
        buildList {
            visible.forEach { bar ->
                add(Triple(bar.label, bar.sum, colors.memberColor(colorIndices[bar.id])))
            }
            if (othersSum > 0) add(Triple(othersLabel, othersSum, colors.inkSecondary))
        }
    }

    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(stringResource(R.string.totals_who_paid))
        Spacer(Modifier.height(12.dp))
        val donutChartLabel = stringResource(R.string.totals_chart_donut_a11y)
        Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
            Box(modifier = Modifier.size(180.dp), contentAlignment = Alignment.Center) {
                Canvas(
                    modifier = Modifier
                        .fillMaxSize()
                        .semantics { contentDescription = donutChartLabel },
                ) {
                    val stroke = 26.dp.toPx()
                    val diameter = size.minDimension - stroke
                    val topLeft = Offset(
                        (size.width - diameter) / 2f,
                        (size.height - diameter) / 2f,
                    )
                    val arcSize = Size(diameter, diameter)
                    // Зазор 1.5° разделяет сегменты (у одного сегмента не нужен).
                    val gap = if (segments.size > 1) 1.5f else 0f
                    val sweepTotal = 360f - gap * segments.size
                    var start = -90f + gap / 2f
                    segments.forEach { (_, sum, color) ->
                        val sweep = max(sweepTotal * sum / total, 0.5f)
                        drawArc(
                            color = color,
                            startAngle = start,
                            sweepAngle = sweep,
                            useCenter = false,
                            topLeft = topLeft,
                            size = arcSize,
                            style = Stroke(width = stroke),
                        )
                        start += sweep + gap
                    }
                }
                MoneyText(
                    totalSpent,
                    role = MoneyRole.NEUTRAL,
                    size = 18.sp,
                    currency = currency,
                )
            }
        }
        Spacer(Modifier.height(14.dp))
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            segments.forEach { (label, sum, color) ->
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(
                        modifier = Modifier
                            .size(10.dp)
                            .clip(CircleShape)
                            .background(color),
                    )
                    Text(
                        text = label,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.Medium,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        text = "${money(sum, currency)} · ${percentOf(sum, total)} %",
                        fontSize = 12.sp,
                        color = colors.inkSecondary,
                        maxLines = 1,
                        style = TextStyle(fontFeatureSettings = "tnum"),
                    )
                }
            }
        }
    }
}

// MARK: - «Чья доля» (горизонтальные бары личных цветов)

/** Строка графика: подпись участника (уникальная) и сумма. */
internal data class MemberBar(val id: Long, val label: String, val sum: Int)

/**
 * Готовит бары: убирает нули, сортирует по убыванию, делает подписи
 * уникальными (тёзки получают « (2)»). Пустой список — секция скрывается.
 */
internal fun preparedMemberBars(members: List<MemberSum>): List<MemberBar> {
    val sorted = members
        .filter { it.sum != 0 }
        .sortedByDescending { it.sum }
    val seen = mutableMapOf<String, Int>()
    return sorted.map { member ->
        val name = member.user.displayName
        val count = (seen[name] ?: 0) + 1
        seen[name] = count
        MemberBar(
            id = member.user.id,
            label = if (count > 1) "$name ($count)" else name,
            sum = member.sum,
        )
    }
}

/**
 * Горизонтальные бары по участникам: заливка — ЛИЧНЫЙ цвет участника
 * (chartCategorical по user.id, вне палитры — inkSecondary), имя слева (ink),
 * заполнение по доле от максимума, сумма справа direct-label (inkSecondary);
 * идентичность несут те же цвета, что в донате.
 */
@Composable
private fun MemberBarsCard(
    title: String,
    bars: List<MemberBar>,
    currency: String,
    colorIndices: Map<Long, Int>,
) {
    val colors = Splitty.colors
    val maxSum = max(bars.maxOf { it.sum }, 1)
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(title)
        Spacer(Modifier.height(12.dp))
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            bars.forEach { bar ->
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = bar.label,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.Medium,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.width(92.dp),
                    )
                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .height(16.dp),
                        contentAlignment = Alignment.CenterStart,
                    ) {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth(
                                    fraction = (bar.sum.toFloat() / maxSum).coerceIn(0.02f, 1f)
                                )
                                .fillMaxHeight()
                                .clip(RoundedCornerShape(4.dp))
                                .background(colors.memberColor(colorIndices[bar.id])),
                        )
                    }
                    Text(
                        text = money(bar.sum, currency),
                        fontSize = 12.sp,
                        fontWeight = FontWeight.Medium,
                        color = colors.inkSecondary,
                        maxLines = 1,
                        style = TextStyle(fontFeatureSettings = "tnum"),
                    )
                }
            }
        }
    }
}

// MARK: - «Баланс участников» (diverging-бары)

/**
 * Diverging-бары net-балансов (net = заплатил − его доля) от ОБЩЕЙ нулевой
 * оси: вправо chartAccent (вложил больше доли), влево negative («проел»
 * больше, чем платил). Имя слева, сумма справа MoneyText AUTO (знак —
 * цветом); фиксированные ширины колонок держат ось на одной вертикали.
 */
@Composable
private fun MemberBalanceCard(nets: List<MemberNetBar>, currency: String) {
    val colors = Splitty.colors
    val maxAbs = max(nets.maxOf { abs(it.net) }, 1)
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(stringResource(R.string.totals_member_balance))
        Spacer(Modifier.height(12.dp))
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            nets.forEach { bar ->
                // Имя и сумма строки уже текстовые, а бар — чистая декорация,
                // поэтому строка склеивается в один элемент «Имя, сумма»
                // (вместо общей подписи на весь график, как в iOS).
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .semantics(mergeDescendants = true) {},
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = bar.label,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.Medium,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.width(92.dp),
                    )
                    Canvas(
                        modifier = Modifier
                            .weight(1f)
                            .height(16.dp),
                    ) {
                        val center = size.width / 2f
                        // Общая нулевая ось: чуть выходит за бар — читается
                        // сквозной вертикалью через все строки.
                        drawLine(
                            color = colors.hairline,
                            start = Offset(center, -2.dp.toPx()),
                            end = Offset(center, size.height + 2.dp.toPx()),
                            strokeWidth = 1.dp.toPx(),
                        )
                        if (bar.net != 0) {
                            val length = max(
                                size.width / 2f * abs(bar.net) / maxAbs,
                                2.dp.toPx(),
                            )
                            drawRoundRect(
                                color = if (bar.net > 0) colors.chartAccent else colors.negative,
                                topLeft = Offset(
                                    x = if (bar.net > 0) center else center - length,
                                    y = 0f,
                                ),
                                size = Size(length, size.height),
                                cornerRadius = CornerRadius(4.dp.toPx()),
                            )
                        }
                    }
                    Box(
                        modifier = Modifier.width(84.dp),
                        contentAlignment = Alignment.CenterEnd,
                    ) {
                        MoneyText(
                            bar.net,
                            role = MoneyRole.AUTO,
                            size = 12.sp,
                            weight = FontWeight.Medium,
                            currency = currency,
                        )
                    }
                }
            }
        }
    }
}

// MARK: - «По дням недели» (Canvas, 7 колонок)

/**
 * Агрегация трат по дням недели пн…вс: 7 колонок chartAccent, максимум
 * выделен полным цветом и direct-label суммы (остальные — приглушённые,
 * подписи без чисел — правило selective direct labels).
 */
@Composable
private fun WeekdayCard(totals: List<Int>, currency: String) {
    val colors = Splitty.colors
    val textMeasurer = rememberTextMeasurer()
    val labelStyle = TextStyle(fontSize = 11.sp, color = colors.inkSecondary)
    val valueStyle = TextStyle(
        fontSize = 11.sp,
        fontWeight = FontWeight.SemiBold,
        color = colors.ink,
        fontFeatureSettings = "tnum",
    )
    val maxSum = max(totals.max(), 1)
    val maxIndex = totals.indexOf(totals.max())

    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(stringResource(R.string.totals_by_weekday))
        Spacer(Modifier.height(8.dp))
        val weekdayChartLabel = stringResource(R.string.totals_chart_weekday_a11y)
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(150.dp)
                .semantics { contentDescription = weekdayChartLabel },
        ) {
            val labelArea = 18.dp.toPx()
            val valueArea = 20.dp.toPx() // запас сверху под direct-label максимума
            val chartBottom = size.height - labelArea
            val chartHeight = chartBottom - valueArea

            // Сетка тише данных: hairline-линии на 0, ⅓, ⅔ и полной высоте.
            for (step in 0..3) {
                val y = chartBottom - chartHeight * step / 3f
                drawLine(
                    color = colors.hairline,
                    start = Offset(0f, y),
                    end = Offset(size.width, y),
                    strokeWidth = 1f,
                )
            }

            val slotWidth = size.width / totals.size
            val barWidth = slotWidth * 0.5f
            totals.forEachIndexed { index, sum ->
                val centerX = slotWidth * index + slotWidth / 2f
                if (sum > 0) {
                    val barHeight = chartHeight * sum / maxSum
                    drawRoundRect(
                        color = if (index == maxIndex) {
                            colors.chartAccent
                        } else {
                            colors.chartAccent.copy(alpha = 0.45f)
                        },
                        topLeft = Offset(centerX - barWidth / 2f, chartBottom - barHeight),
                        size = Size(barWidth, barHeight),
                        cornerRadius = CornerRadius(4.dp.toPx()),
                    )
                }
                val layout = textMeasurer.measure(
                    AnnotatedString(GroupsDateFmt.weekdaysShort[index]),
                    labelStyle,
                )
                drawText(
                    layout,
                    topLeft = Offset(centerX - layout.size.width / 2f, chartBottom + 4.dp.toPx()),
                )
            }

            // Direct-label максимума над самым высоким баром.
            val maxLayout = textMeasurer.measure(
                AnnotatedString(money(maxSum, currency)),
                valueStyle,
            )
            val maxCenterX = slotWidth * maxIndex + slotWidth / 2f
            val textX = (maxCenterX - maxLayout.size.width / 2f)
                .coerceIn(0f, max(0f, size.width - maxLayout.size.width))
            drawText(
                maxLayout,
                topLeft = Offset(textX, valueArea - maxLayout.size.height - 3.dp.toPx()),
            )
        }
    }
}

// MARK: - Топ расходов

/** Карточный список топ-5 расходов по сумме. */
@Composable
private fun TopOperationsCard(stats: Statistics) {
    val colors = Splitty.colors
    val top = stats.topOperations.take(5)
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 16.dp) {
        SectionHeader(stringResource(R.string.totals_top_operations))
        Spacer(Modifier.height(4.dp))
        top.forEachIndexed { index, operation ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    Text(
                        text = operation.description.ifEmpty {
                            stringResource(R.string.group_op_fallback)
                        },
                        fontSize = 15.sp,
                        fontWeight = FontWeight.Medium,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = "${operation.donor.displayName} · " +
                            GroupsDateFmt.dayMonth(operation.createdAt),
                        fontSize = 12.sp,
                        color = colors.inkSecondary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                MoneyText(
                    operation.sum,
                    role = MoneyRole.NEUTRAL,
                    size = 15.sp,
                    currency = stats.currency,
                )
            }
            if (index < top.lastIndex) {
                HairlineDivider()
            }
        }
    }
}
