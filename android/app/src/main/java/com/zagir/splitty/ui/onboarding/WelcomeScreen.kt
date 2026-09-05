package com.zagir.splitty.ui.onboarding

import com.zagir.splitty.core.analytics.AnalyticsEvent
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.scale
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.theme.Splitty
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Разовое приветствие после первого входа. Порт iOS `WelcomeView`.
 *
 * Четыре экрана, и порядок не декоративный: что такое группа → как вносить
 * расход → кто сколько заплатил → сколько раз платить вам. Без третьего экрана
 * последний приходится принимать на веру: суммы переводов берутся из долей.
 *
 * Числа подобраны так, чтобы проверяться в уме: 600 на троих — по 200, 300 —
 * по 100. У Бори баланс ровно ноль, поэтому два перевода схлопываются в один.
 *
 * Иллюстрации живые: каждая начинает движение, когда её страница становится
 * активной, и повторяется. Статичная картинка читается как заглушка.
 */
@Composable
fun WelcomeScreen(
    onFinish: (createGroup: Boolean) -> Unit,
    /**
     * Куда сообщать о шагах. Колбэком, а не через Hilt: экран рендерится в
     * снимках Roborazzi, и зависимость от графа заставила бы их переснимать.
     */
    onEvent: (AnalyticsEvent) -> Unit = {},
) {
    val colors = Splitty.colors
    val pages = 4
    val pagerState = rememberPagerState(pageCount = { pages })
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) { onEvent(AnalyticsEvent.OnboardingStarted) }
    LaunchedEffect(pagerState.currentPage) {
        // Имена шагов общие со вторым клиентом: переименование страницы в коде
        // не должно тихо развести один шаг воронки на два.
        val step = when (pagerState.currentPage) {
            0 -> "group"
            1 -> "dictate"
            2 -> "who_paid"
            else -> "transfers"
        }
        onEvent(AnalyticsEvent.OnboardingStep(step))
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.bg)
            .padding(bottom = 20.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            TextButton(
                onClick = {
                    onEvent(AnalyticsEvent.OnboardingSkipped)
                    onFinish(false)
                },
                modifier = Modifier.testTag("welcome_skip"),
            ) {
                Text(stringResource(R.string.welcome_skip), fontSize = 16.sp, color = colors.inkSecondary)
            }
        }

        HorizontalPager(state = pagerState, modifier = Modifier.weight(1f)) { page ->
            val active = pagerState.currentPage == page
            when (page) {
                0 -> WelcomePage(R.string.welcome_1_title, R.string.welcome_1_body) { SharedBillArt(active) }
                1 -> WelcomePage(R.string.welcome_2_title, R.string.welcome_2_body) { DictationArt(active) }
                2 -> WelcomePage(R.string.welcome_3_title, R.string.welcome_3_body) { WhoPaidArt(active) }
                else -> WelcomePage(R.string.welcome_4_title, R.string.welcome_4_body) { TransfersArt(active) }
            }
        }

        Spacer(Modifier.height(16.dp))
        PageDots(count = pages, current = pagerState.currentPage)
        Spacer(Modifier.height(16.dp))

        val isLast = pagerState.currentPage == pages - 1
        PrimaryPillButton(
            text = stringResource(if (isLast) R.string.welcome_create else R.string.welcome_next),
            onClick = {
                if (isLast) {
                    onEvent(AnalyticsEvent.OnboardingCompleted)
                    onFinish(true)
                } else {
                    scope.launch { pagerState.animateScrollToPage(pagerState.currentPage + 1) }
                }
            },
            modifier = Modifier.padding(horizontal = 20.dp).testTag("welcome_primary"),
        )
    }
}

@Composable
private fun WelcomePage(titleRes: Int, bodyRes: Int, art: @Composable () -> Unit) {
    val colors = Splitty.colors
    Column(Modifier.fillMaxSize()) {
        // Иллюстрация забирает всё свободное место, текст прижат к низу:
        // маленькая картинка с пустотой под текстом читалась как заглушка.
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(horizontal = 16.dp)
                .clip(RoundedCornerShape(24.dp))
                .background(colors.accent.copy(alpha = 0.07f)),
        ) { art() }

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .padding(top = 22.dp, bottom = 2.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(
                text = stringResource(titleRes),
                fontSize = 26.sp,
                fontWeight = FontWeight.Bold,
                color = colors.ink,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(bodyRes),
                fontSize = 16.sp,
                lineHeight = 21.sp,
                color = colors.inkSecondary,
                textAlign = TextAlign.Center,
            )
        }
    }
}

@Composable
private fun PageDots(count: Int, current: Int) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(7.dp, Alignment.CenterHorizontally),
    ) {
        repeat(count) { index ->
            val width by animateDpAsState(if (index == current) 22.dp else 7.dp, label = "dot")
            Box(
                Modifier
                    .width(width)
                    .height(7.dp)
                    .clip(CircleShape)
                    .background(if (index == current) colors.accent else colors.hairline)
            )
        }
    }
}

// MARK: — общее

/** Надзаголовок: капслок задаём здесь, чтобы не держать его в каждом переводе. */
@Composable
private fun Eyebrow(text: String) {
    Text(
        text = text.uppercase(),
        fontSize = 12.sp,
        fontWeight = FontWeight.SemiBold,
        fontFamily = FontFamily.Monospace,
        color = Splitty.colors.inkSecondary,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun Avatar(letter: String, color: Color, size: Int = 46) {
    Box(
        modifier = Modifier.size(size.dp).clip(CircleShape).background(color),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            letter,
            fontSize = (size * 0.42f).sp,
            fontWeight = FontWeight.Bold,
            color = Color.White,
        )
    }
}

@Composable
private fun ArrowDown(modifier: Modifier = Modifier) {
    Text(
        "↓",
        fontSize = 20.sp,
        fontWeight = FontWeight.SemiBold,
        color = Splitty.colors.accent.copy(alpha = 0.45f),
        textAlign = TextAlign.Center,
        modifier = modifier.fillMaxWidth(),
    )
}

/** Пунктирная рамка: в Compose нет готовой, а сплошная читается как обычная карточка. */
private fun Modifier.dashedBorder(color: Color, radius: Dp, width: Dp = 1.5.dp) = drawBehind {
    val stroke = width.toPx()
    drawRoundRect(
        color = color,
        topLeft = Offset(stroke / 2, stroke / 2),
        size = Size(size.width - stroke, size.height - stroke),
        cornerRadius = CornerRadius(radius.toPx()),
        style = Stroke(
            width = stroke,
            pathEffect = PathEffect.dashPathEffect(floatArrayOf(6.dp.toPx(), 5.dp.toPx())),
        ),
    )
}

@Composable
private fun Card(
    modifier: Modifier = Modifier,
    radius: Dp = 18.dp,
    background: Color = Splitty.colors.surface,
    elevation: Dp = 3.dp,
    content: @Composable () -> Unit,
) {
    Box(
        modifier
            .shadow(elevation, RoundedCornerShape(radius))
            .clip(RoundedCornerShape(radius))
            .background(background)
    ) { content() }
}

/** Строка списка ровно тех же размеров, что в самом приложении. */
@Composable
private fun AppRow(
    initial: String?,
    avatarColor: Color,
    title: String,
    subtitle: String?,
    amount: String,
    amountColor: Color,
    titleColor: Color = Splitty.colors.ink,
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        if (initial != null) Avatar(initial, avatarColor)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = titleColor)
            if (subtitle != null) {
                Text(subtitle, fontSize = 13.sp, color = Splitty.colors.inkSecondary)
            }
        }
        Text(
            amount,
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            fontFamily = FontFamily.Monospace,
            color = amountColor,
        )
    }
}

// MARK: — экран 1: расходы падают в общий счёт

@Composable
private fun SharedBillArt(isActive: Boolean) {
    val colors = Splitty.colors
    // Суммы витрины — строки, а не числа: в локали это не рубли, и «₽» тут
    // дописать неоткуда. Итоги посчитаны заранее по той же причине.
    val slips = listOf(
        stringResource(R.string.welcome_slip_dinner) to stringResource(R.string.demo_sum_600),
        stringResource(R.string.welcome_slip_taxi) to stringResource(R.string.demo_sum_300),
        stringResource(R.string.welcome_slip_groceries) to stringResource(R.string.demo_sum_450),
        stringResource(R.string.welcome_slip_coffee) to stringResource(R.string.demo_sum_150),
    )
    val runningTotals = listOf(
        stringResource(R.string.demo_sum_600),
        stringResource(R.string.demo_sum_900),
        stringResource(R.string.demo_sum_1350),
        stringResource(R.string.demo_sum_1500),
    )
    var shown by remember { mutableIntStateOf(slips.size) }

    LaunchedEffect(isActive) {
        if (!isActive) {
            shown = slips.size
            return@LaunchedEffect
        }
        while (true) {
            shown = 0
            repeat(slips.size) {
                delay(600)
                shown = it + 1
            }
            // Собранный счёт держим долго: это и есть мысль экрана, а не
            // мельтешение появлений.
            delay(4500)
            shown = 0
            delay(700)
        }
    }

    // Композиция собрана компактно и стоит по центру: тянуть строки распорками
    // по всей карточке — значит порвать список на куски.
    Column(
        modifier = Modifier.fillMaxSize().padding(20.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Eyebrow(stringResource(R.string.welcome_bill_eyebrow))
        Spacer(Modifier.height(14.dp))

        slips.forEachIndexed { index, slip ->
            val visible = index < shown
            val alpha by animateFloatAsState(if (visible) 1f else 0f, label = "slipAlpha")
            val shift by animateFloatAsState(if (visible) 0f else -22f, label = "slipShift")
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .graphicsLayer {
                        this.alpha = alpha
                        translationY = shift * density
                    },
            ) {
                Row(
                    Modifier.fillMaxWidth().padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(slip.first, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
                    Spacer(Modifier.weight(1f))
                    Text(
                        slip.second,
                        fontSize = 17.sp,
                        fontWeight = FontWeight.SemiBold,
                        fontFamily = FontFamily.Monospace,
                        color = colors.ink,
                    )
                }
            }
            if (index < slips.lastIndex) Spacer(Modifier.height(10.dp))
        }

        ArrowDown(Modifier.padding(vertical = 14.dp))

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .dashedBorder(colors.accent.copy(alpha = 0.5f), 16.dp)
                .padding(vertical = 18.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(
                    stringResource(R.string.welcome_bill_tray),
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.accentText.copy(alpha = 0.8f),
                )
                Text(
                    runningTotals[shown.coerceIn(1, runningTotals.size) - 1],
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Bold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.accentText,
                )
            }
        }
    }
}

// MARK: — экран 2: запись, распознавание и мини-чек

private enum class DictationStage { RECORDING, PARSING, RECEIPT }

/**
 * Повторяет живой `RecordingOverlay` и оверлей распознавания с экрана расхода:
 * тот же микрофон 82 dp, та же волна 240×44, тот же счётчик и те же тексты.
 * Онбординг обещает ровно тот экран, который человек увидит.
 */
@Composable
private fun DictationArt(isActive: Boolean) {
    val colors = Splitty.colors
    // Фраза переводится целиком и режется на слова здесь: по отдельности «за»
    // и «с» перевести нельзя — в другом языке их может не быть вовсе.
    val phrase = stringResource(R.string.welcome_dictation_phrase).split(" ")
    var words by remember { mutableIntStateOf(phrase.size) }
    var seconds by remember { mutableIntStateOf(6) }
    var stage by remember { mutableStateOf(DictationStage.RECORDING) }
    val arc = remember { Animatable(0f) }

    LaunchedEffect(isActive) {
        if (!isActive) {
            words = phrase.size
            seconds = 6
            stage = DictationStage.RECORDING
            return@LaunchedEffect
        }
        while (true) {
            stage = DictationStage.RECORDING
            words = 0
            seconds = 0
            arc.snapTo(0f)
            launch { arc.animateTo(0.11f, tween(4500, easing = LinearEasing)) }
            repeat(phrase.size) {
                delay(450)
                words = it + 1
                if ((it + 1) % 2 == 0) seconds += 1
            }
            delay(700)
            stage = DictationStage.PARSING
            delay(1600)
            stage = DictationStage.RECEIPT
            delay(3800)
        }
    }

    // Фон один на все стадии: распознавание и чек проявляются поверх той же
    // тёмной записи, а не подменяют экран вспышкой света.
    Box(
        modifier = Modifier.fillMaxSize().background(Color(0xFF2B313A)),
        contentAlignment = Alignment.Center,
    ) {
        when (stage) {
            DictationStage.RECORDING -> RecordingStage(
                text = phrase.take(words).joinToString(" "),
                seconds = seconds,
                arcProgress = arc.value,
            )
            DictationStage.PARSING -> ParsingStage()
            DictationStage.RECEIPT -> Column(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 28.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Text(
                    "✓ " + stringResource(R.string.welcome_ready),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = Color.White.copy(alpha = 0.85f),
                )
                MiniReceipt()
            }
            else -> Unit
        }
    }
}

@Composable
private fun RecordingStage(text: String, seconds: Int, arcProgress: Float) {
    val colors = Splitty.colors
    Column(
        modifier = Modifier.fillMaxSize().padding(horizontal = 24.dp, vertical = 22.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.weight(1f))

        // Окно расшифровки — 21sp, как в живом оверлее записи.
        Box(modifier = Modifier.height(96.dp).fillMaxWidth(), contentAlignment = Alignment.BottomCenter) {
            Text(
                text = text,
                fontSize = 21.sp,
                lineHeight = 27.sp,
                fontWeight = FontWeight.SemiBold,
                color = Color.White,
                textAlign = TextAlign.Center,
            )
        }

        Spacer(Modifier.height(16.dp))
        Waveform()

        Spacer(Modifier.height(12.dp))
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier
                    .size(8.dp)
                    .clip(CircleShape)
                    .background(colors.negative.copy(alpha = if (seconds % 2 == 0) 1f else 0.25f))
            )
            Text(
                String.format("0:%02d", seconds),
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                color = Color.White,
            )
        }

        Spacer(Modifier.height(12.dp))
        Text(
            stringResource(R.string.rec_status_recording_title),
            fontSize = 20.sp,
            fontWeight = FontWeight.Bold,
            color = Color.White,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            stringResource(R.string.welcome_rec_hint),
            fontSize = 13.sp,
            color = Color.White.copy(alpha = 0.75f),
            textAlign = TextAlign.Center,
        )

        Spacer(Modifier.weight(1f))
        MicButton(arcProgress = arcProgress)
    }
}

/** Стадия распознавания — те же строки, что в живом оверлее. */
@Composable
private fun ParsingStage() {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        CircularProgressIndicator(color = Color.White)
        Text(
            stringResource(R.string.expense_parsing),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = Color.White,
        )
        Text(
            stringResource(R.string.expense_parsing_subtitle),
            fontSize = 13.sp,
            color = Color.White.copy(alpha = 0.7f),
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(horizontal = 40.dp),
        )
    }
}

/** Микрофон 82 dp — ровно кнопка записи из формы расхода. */
@Composable
private fun MicButton(arcProgress: Float) {
    val colors = Splitty.colors
    val infinite = rememberInfiniteTransition(label = "mic")
    val pulse by infinite.animateFloat(
        initialValue = 1f,
        targetValue = 1.5f,
        animationSpec = infiniteRepeatable(tween(1400, easing = LinearEasing), RepeatMode.Restart),
        label = "pulse",
    )
    val pulseAlpha by infinite.animateFloat(
        initialValue = 0.8f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(1400, easing = LinearEasing), RepeatMode.Restart),
        label = "pulseAlpha",
    )

    Box(contentAlignment = Alignment.Center) {
        Box(
            Modifier
                .size(82.dp)
                .scale(pulse)
                .graphicsLayer { alpha = pulseAlpha }
                .drawBehind {
                    drawCircle(
                        color = colors.accent,
                        radius = size.minDimension / 2 - 1.dp.toPx(),
                        style = Stroke(width = 2.dp.toPx()),
                    )
                }
        )
        Canvas(Modifier.size(98.dp)) {
            val stroke = 4.dp.toPx()
            drawCircle(
                color = Color.White.copy(alpha = 0.16f),
                radius = size.minDimension / 2 - stroke / 2,
                style = Stroke(width = stroke),
            )
            drawArc(
                color = Color.White.copy(alpha = 0.9f),
                startAngle = -90f,
                sweepAngle = 360f * arcProgress,
                useCenter = false,
                topLeft = Offset(stroke / 2, stroke / 2),
                size = Size(size.width - stroke, size.height - stroke),
                style = Stroke(width = stroke, cap = StrokeCap.Round),
            )
        }
        Box(
            modifier = Modifier.size(82.dp).clip(CircleShape).background(colors.accent),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Filled.Mic,
                contentDescription = null,
                tint = Color.White,
                modifier = Modifier.size(34.dp),
            )
        }
    }
}

@Composable
private fun Waveform() {
    val base = listOf(8, 20, 30, 15, 24, 34, 12, 22, 28, 16, 9, 26, 14, 32, 18)
    var phase by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        // Волна живая, но нарочно неспешная: экран объясняет, а не пляшет.
        while (true) {
            delay(150)
            phase++
        }
    }
    Row(
        modifier = Modifier.size(width = 240.dp, height = 44.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterHorizontally),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        base.indices.forEach { index ->
            val height by animateDpAsState(base[(index + phase) % base.size].dp, label = "bar")
            Box(
                Modifier
                    .width(4.dp)
                    .height(height)
                    .clip(CircleShape)
                    .background(Color.White.copy(alpha = 0.92f))
            )
        }
    }
}

/** Мини-чек — уменьшенный ReceiptCard с экрана расхода. */
@Composable
private fun MiniReceipt(modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(4.dp))
            .background(colors.receiptPaper)
            .padding(16.dp),
    ) {
        Row(Modifier.fillMaxWidth()) {
            Text(
                stringResource(R.string.welcome_rcpt_items),
                fontSize = 12.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.inkSecondary,
            )
            Spacer(Modifier.weight(1f))
            Text(
                stringResource(R.string.welcome_rcpt_count),
                fontSize = 12.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.inkSecondary,
            )
        }
        ReceiptItem(
            stringResource(R.string.welcome_rcpt_pizza),
            stringResource(R.string.demo_sum_800),
            stringResource(R.string.welcome_each_400x2),
        )
        ReceiptItem(
            stringResource(R.string.welcome_rcpt_cola),
            stringResource(R.string.demo_sum_200),
            stringResource(R.string.welcome_each_100x2),
        )
        Spacer(Modifier.height(10.dp))
        Box(Modifier.fillMaxWidth().height(1.5.dp).background(colors.ink))
        Spacer(Modifier.height(10.dp))
        Row(Modifier.fillMaxWidth()) {
            Text(
                stringResource(R.string.welcome_rcpt_total),
                fontSize = 16.sp,
                fontWeight = FontWeight.Bold,
                color = colors.ink,
            )
            Spacer(Modifier.weight(1f))
            Text(
                stringResource(R.string.demo_sum_1000),
                fontSize = 17.sp,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                color = colors.ink,
            )
        }
    }
}

@Composable
private fun ReceiptItem(name: String, sum: String, each: String) {
    val colors = Splitty.colors
    Spacer(Modifier.height(9.dp))
    Box(Modifier.fillMaxWidth().height(1.dp).background(colors.hairline))
    Spacer(Modifier.height(9.dp))
    Row(Modifier.fillMaxWidth()) {
        Text(name, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
        Spacer(Modifier.weight(1f))
        Text(
            sum,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            fontFamily = FontFamily.Monospace,
            color = colors.ink,
        )
    }
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Avatar(stringResource(R.string.welcome_avatar_you), colors.accent, size = 20)
        Spacer(Modifier.width(2.dp))
        Avatar(stringResource(R.string.welcome_avatar_sanya), colors.chartCategorical[1], size = 20)
        Spacer(Modifier.weight(1f))
        Text(each, fontSize = 12.sp, fontFamily = FontFamily.Monospace, color = colors.inkSecondary)
    }
}

// MARK: — экран 3: кто за что заплатил

@Composable
private fun WhoPaidArt(isActive: Boolean) {
    val colors = Splitty.colors
    var shown by remember { mutableIntStateOf(3) }

    LaunchedEffect(isActive) {
        if (!isActive) {
            shown = 3
            return@LaunchedEffect
        }
        while (true) {
            shown = 0
            repeat(3) {
                // Каждую карточку надо успеть прочитать: суммы здесь и есть
                // содержание экрана.
                delay(if (it == 2) 1400 else 1100)
                shown = it + 1
            }
            delay(5000)
            shown = 0
            delay(700)
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(18.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Eyebrow(stringResource(R.string.welcome_paid_eyebrow))
        Spacer(Modifier.height(14.dp))

        PaidCard(
            initial = stringResource(R.string.welcome_avatar_anya),
            who = stringResource(R.string.welcome_paid_anya),
            what = stringResource(R.string.welcome_for_dinner),
            sum = stringResource(R.string.demo_sum_600),
            share = stringResource(R.string.welcome_each_200),
            color = colors.accent,
            visible = shown > 0,
        )
        Spacer(Modifier.height(12.dp))
        PaidCard(
            initial = stringResource(R.string.welcome_avatar_borya),
            who = stringResource(R.string.welcome_paid_borya),
            what = stringResource(R.string.welcome_for_taxi),
            sum = stringResource(R.string.demo_sum_300),
            share = stringResource(R.string.welcome_each_100),
            color = colors.chartCategorical[2],
            visible = shown > 1,
        )

        ArrowDown(
            Modifier
                .padding(vertical = 12.dp)
                .graphicsLayer { alpha = if (shown > 2) 1f else 0f }
        )

        val summaryAlpha by animateFloatAsState(if (shown > 2) 1f else 0f, label = "summary")
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .graphicsLayer { alpha = summaryAlpha }
                .clip(RoundedCornerShape(18.dp))
                .background(colors.accent.copy(alpha = 0.11f))
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                Avatar(stringResource(R.string.welcome_avatar_you), colors.inkSecondary)
                Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text(
                        stringResource(R.string.welcome_you_paid_nothing),
                        fontSize = 16.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = colors.ink,
                    )
                    Text(
                        stringResource(R.string.welcome_your_share),
                        fontSize = 13.sp,
                        color = colors.inkSecondary,
                    )
                }
                Spacer(Modifier.weight(1f))
                Text(
                    stringResource(R.string.demo_sum_300),
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.accentText,
                )
            }
            Box(Modifier.fillMaxWidth().height(1.dp).background(colors.accent.copy(alpha = 0.25f)))
            // Сумма долей выписана, чтобы 300 ₽ можно было проверить в уме.
            Text(
                stringResource(R.string.welcome_share_math),
                fontSize = 13.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.accentText.copy(alpha = 0.75f),
            )
        }
    }
}

@Composable
private fun PaidCard(
    initial: String,
    who: String,
    what: String,
    sum: String,
    share: String,
    color: Color,
    visible: Boolean,
) {
    val colors = Splitty.colors
    val alpha by animateFloatAsState(if (visible) 1f else 0f, label = "paidAlpha")
    Card(modifier = Modifier.fillMaxWidth().graphicsLayer { this.alpha = alpha }) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                Avatar(initial, color)
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text(who, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
                    Text(what, fontSize = 13.sp, color = colors.inkSecondary)
                }
                Text(
                    sum,
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.ink,
                )
            }
            Box(Modifier.fillMaxWidth().height(1.dp).background(colors.hairline))
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    stringResource(R.string.welcome_split_three),
                    fontSize = 13.sp,
                    color = colors.inkSecondary,
                )
                Box(
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(colors.accent.copy(alpha = 0.14f))
                        .padding(horizontal = 10.dp, vertical = 5.dp),
                ) {
                    Text(
                        share,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.SemiBold,
                        fontFamily = FontFamily.Monospace,
                        color = colors.accentText,
                    )
                }
            }
        }
    }
}

// MARK: — экран 4: сколько раз платить

/**
 * Сравнение «Без Splitty / Со Splitty». Темп задаёт человек — но один раз
 * переключатель срабатывает сам, иначе половина людей не поймёт, что тут есть
 * что нажать, и увидит только состояние «без».
 */
@Composable
private fun TransfersArt(isActive: Boolean) {
    val colors = Splitty.colors
    var withSplitty by remember { mutableStateOf(false) }
    var touched by remember { mutableStateOf(false) }

    LaunchedEffect(isActive) {
        if (!isActive || touched) return@LaunchedEffect
        withSplitty = false
        // Пауза длиннее: сначала надо прочитать «без», иначе переключение
        // случится раньше, чем человек понял, что сравнивают.
        delay(2800)
        if (!touched) withSplitty = true
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(18.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Card(modifier = Modifier.fillMaxWidth()) {
            Row(
                Modifier.fillMaxWidth().padding(16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    stringResource(R.string.welcome_evening_share),
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                )
                Spacer(Modifier.weight(1f))
                Text(
                    stringResource(R.string.demo_sum_300),
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.ink,
                )
            }
        }

        Spacer(Modifier.height(14.dp))
        Eyebrow(stringResource(R.string.welcome_you_transfer))
        Spacer(Modifier.height(12.dp))

        Card(modifier = Modifier.fillMaxWidth()) {
            AppRow(
                initial = stringResource(R.string.welcome_avatar_anya),
                avatarColor = colors.accent,
                title = stringResource(R.string.welcome_to_anya),
                subtitle = stringResource(
                    if (withSplitty) R.string.welcome_anya_note_with else R.string.welcome_for_dinner
                ),
                amount = if (withSplitty) {
                    stringResource(R.string.demo_sum_300)
                } else {
                    stringResource(R.string.demo_sum_200)
                },
                amountColor = if (withSplitty) colors.accentText else colors.negative,
            )
        }

        Spacer(Modifier.height(10.dp))

        // Место под вторую строку занято в обоих состояниях: слева перевод,
        // справа объяснение, почему его больше нет. Иначе при переключении низ
        // экрана прыгает, а половина карточки пустует.
        Box(Modifier.fillMaxWidth().height(122.dp), contentAlignment = Alignment.TopStart) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (!withSplitty) {
                    Card(modifier = Modifier.fillMaxWidth()) {
                        AppRow(
                            initial = stringResource(R.string.welcome_avatar_borya),
                            avatarColor = colors.chartCategorical[2],
                            title = stringResource(R.string.welcome_to_borya),
                            subtitle = stringResource(R.string.welcome_for_taxi),
                            amount = stringResource(R.string.demo_sum_100),
                            amountColor = colors.negative,
                        )
                    }
                    SideNote(stringResource(R.string.welcome_note_without), colors.negative)
                } else {
                    Box(
                        Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(18.dp))
                            .background(colors.accent.copy(alpha = 0.12f))
                    ) {
                        // Боря заплатил ровно свою долю: его баланс ноль, поэтому
                        // строка «вам переводить» исчезает — это и есть сведение.
                        AppRow(
                            initial = stringResource(R.string.welcome_avatar_borya),
                            avatarColor = colors.chartCategorical[2].copy(alpha = 0.35f),
                            title = stringResource(R.string.welcome_to_borya),
                            subtitle = stringResource(R.string.welcome_settled),
                            amount = stringResource(R.string.demo_sum_0),
                            amountColor = colors.accentText,
                            titleColor = colors.inkSecondary,
                        )
                    }
                    SideNote(stringResource(R.string.welcome_note_with), colors.accentText)
                }
            }
        }

        Spacer(Modifier.height(12.dp))

        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background((if (withSplitty) colors.accent else colors.negative).copy(alpha = 0.1f))
                .padding(vertical = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            repeat(2) { index ->
                val filled = if (withSplitty) index == 0 else true
                Box(
                    Modifier
                        .size(width = 11.dp, height = 15.dp)
                        .clip(RoundedCornerShape(2.5.dp))
                        .background(
                            when {
                                !filled -> colors.hairline
                                withSplitty -> colors.accent
                                else -> colors.negative
                            }
                        )
                )
            }
            Text(
                stringResource(if (withSplitty) R.string.welcome_one_transfer else R.string.welcome_two_transfers),
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = if (withSplitty) colors.accentText else colors.negative,
            )
        }

        Spacer(Modifier.height(12.dp))

        CompareSegment(withSplitty = withSplitty) {
            touched = true
            withSplitty = it
        }
    }
}

@Composable
private fun SideNote(text: String, color: Color) {
    Text(text, fontSize = 13.sp, color = color, modifier = Modifier.fillMaxWidth())
}

@Composable
private fun CompareSegment(withSplitty: Boolean, onChange: (Boolean) -> Unit) {
    val colors = Splitty.colors
    // Свой сегмент, а не системный: системный тянет чужую типографику и серую
    // заливку — рядом с карточками приложения он выглядит вставленным извне.
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(CircleShape)
            .background(colors.ink.copy(alpha = 0.06f))
            .padding(4.dp)
            .testTag("welcome_compare"),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        SegmentHalf(stringResource(R.string.welcome_without), !withSplitty) { onChange(false) }
        SegmentHalf(stringResource(R.string.welcome_with), withSplitty) { onChange(true) }
    }
}

@Composable
private fun RowScope.SegmentHalf(text: String, selected: Boolean, onClick: () -> Unit) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .weight(1f)
            .clip(CircleShape)
            .background(if (selected) colors.accent else Color.Transparent)
            .clickable(onClick = onClick)
            .padding(vertical = 11.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (selected) Color.White else colors.inkSecondary,
        )
    }
}
