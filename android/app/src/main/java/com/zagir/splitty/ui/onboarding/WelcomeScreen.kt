package com.zagir.splitty.ui.onboarding

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
fun WelcomeScreen(onFinish: (createGroup: Boolean) -> Unit) {
    val colors = Splitty.colors
    val pages = 4
    val pagerState = rememberPagerState(pageCount = { pages })
    val scope = rememberCoroutineScope()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.bg)
            .padding(bottom = 20.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            TextButton(onClick = { onFinish(false) }, modifier = Modifier.testTag("welcome_skip")) {
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
                if (isLast) onFinish(true)
                else scope.launch { pagerState.animateScrollToPage(pagerState.currentPage + 1) }
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
        fontSize = 13.5.sp,
        fontWeight = FontWeight.SemiBold,
        fontFamily = FontFamily.Monospace,
        color = Splitty.colors.inkSecondary,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun Avatar(letter: String, color: Color, size: Int = 48) {
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
private fun ArrowDown() {
    Text(
        "↓",
        fontSize = 26.sp,
        fontWeight = FontWeight.SemiBold,
        color = Splitty.colors.accent.copy(alpha = 0.45f),
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}

/** Пунктирная рамка: в Compose нет готовой, а сплошная читается как обычная карточка. */
private fun Modifier.dashedBorder(color: Color, radius: Dp, width: Dp = 2.dp) = drawBehind {
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
    radius: Dp = 20.dp,
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

// MARK: — экран 1: расходы падают в общий счёт

@Composable
private fun SharedBillArt(isActive: Boolean) {
    val colors = Splitty.colors
    val slips = listOf("Ужин" to 600, "Такси" to 300, "Продукты" to 450)
    var shown by remember { mutableIntStateOf(slips.size) }

    LaunchedEffect(isActive) {
        if (!isActive) {
            shown = slips.size
            return@LaunchedEffect
        }
        while (true) {
            shown = 0
            repeat(slips.size) {
                delay(320)
                shown = it + 1
            }
            delay(1900)
            shown = 0
            delay(400)
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(20.dp)) {
        Eyebrow(stringResource(R.string.welcome_bill_eyebrow))

        Spacer(Modifier.height(20.dp))

        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            slips.forEachIndexed { index, slip ->
                val visible = index < shown
                val alpha by animateFloatAsState(if (visible) 1f else 0f, label = "slipAlpha")
                val shift by animateFloatAsState(if (visible) 0f else -26f, label = "slipShift")
                Card(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(80.dp)
                        .graphicsLayer {
                            this.alpha = alpha
                            translationY = shift * density
                        },
                    radius = 18.dp,
                ) {
                    Row(
                        Modifier.fillMaxSize().padding(horizontal = 20.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(slip.first, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
                        Spacer(Modifier.weight(1f))
                        Text(
                            "${slip.second} ₽",
                            fontSize = 21.sp,
                            fontWeight = FontWeight.Bold,
                            fontFamily = FontFamily.Monospace,
                            color = colors.ink,
                        )
                    }
                }
            }
        }

        Spacer(Modifier.weight(1f))
        ArrowDown()
        Spacer(Modifier.weight(1f))

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(104.dp)
                .dashedBorder(colors.accent.copy(alpha = 0.55f), 20.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(
                    stringResource(R.string.welcome_bill_tray),
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.accentText,
                )
                Text(
                    "${slips.take(shown).sumOf { it.second }} ₽",
                    fontSize = 30.sp,
                    fontWeight = FontWeight.Bold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.accentText,
                )
            }
        }
    }
}

// MARK: — экран 2: запись голоса и мини-чек

@Composable
private fun DictationArt(isActive: Boolean) {
    val colors = Splitty.colors
    val phrase = listOf("пицца", "за", "восемьсот", "и", "кола", "за", "двести", "пополам", "с", "Саней")
    var words by remember { mutableIntStateOf(phrase.size) }
    var showReceipt by remember { mutableStateOf(false) }
    val arc = remember { Animatable(0f) }

    LaunchedEffect(isActive) {
        if (!isActive) {
            words = phrase.size
            showReceipt = false
            return@LaunchedEffect
        }
        while (true) {
            showReceipt = false
            words = 0
            arc.snapTo(0f)
            launch { arc.animateTo(0.62f, tween(3600, easing = LinearEasing)) }
            repeat(phrase.size) {
                delay(250)
                words = it + 1
            }
            delay(800)
            showReceipt = true
            delay(2600)
        }
    }

    // Фон один на оба состояния: чек проявляется поверх той же тёмной записи,
    // а не подменяет экран вспышкой света.
    Box(
        modifier = Modifier.fillMaxSize().background(Color(0xFF2B313A)),
        contentAlignment = Alignment.Center,
    ) {
        if (showReceipt) {
            Column(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(20.dp),
            ) {
                Text(
                    "✓ " + stringResource(R.string.welcome_ready),
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = Color.White.copy(alpha = 0.85f),
                )
                MiniReceipt()
            }
        } else {
            Column(
                modifier = Modifier.fillMaxSize().padding(vertical = 26.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Box(modifier = Modifier.height(104.dp).fillMaxWidth(), contentAlignment = Alignment.BottomCenter) {
                    Text(
                        text = phrase.take(words).joinToString(" "),
                        fontSize = 23.sp,
                        lineHeight = 29.sp,
                        fontWeight = FontWeight.Bold,
                        color = Color.White,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.padding(horizontal = 20.dp),
                    )
                }

                Waveform()

                Row(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.size(7.dp).clip(CircleShape).background(colors.negative))
                    Text(
                        "0:06",
                        fontSize = 15.sp,
                        fontWeight = FontWeight.SemiBold,
                        fontFamily = FontFamily.Monospace,
                        color = Color.White.copy(alpha = 0.9f),
                    )
                }

                Spacer(Modifier.weight(1f))

                MicButton(arcProgress = arc.value)

                Text(
                    stringResource(R.string.welcome_rec_hint),
                    fontSize = 14.sp,
                    color = Color.White.copy(alpha = 0.55f),
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(horizontal = 16.dp),
                )
            }
        }
    }
}

/** Микрофон как в самой записи: пульс-кольцо и дуга лимита в 60 с. */
@Composable
private fun MicButton(arcProgress: Float) {
    val colors = Splitty.colors
    val infinite = rememberInfiniteTransition(label = "mic")
    val pulse by infinite.animateFloat(
        initialValue = 1f,
        targetValue = 1.55f,
        animationSpec = infiniteRepeatable(tween(1900, easing = LinearEasing), RepeatMode.Restart),
        label = "pulse",
    )
    val pulseAlpha by infinite.animateFloat(
        initialValue = 0.8f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(1900, easing = LinearEasing), RepeatMode.Restart),
        label = "pulseAlpha",
    )

    Box(contentAlignment = Alignment.Center) {
        Box(
            Modifier
                .size(104.dp)
                .scale(pulse)
                .graphicsLayer { alpha = pulseAlpha }
                .drawBehind {
                    drawCircle(
                        color = colors.accent,
                        radius = size.minDimension / 2 - 1.5.dp.toPx(),
                        style = Stroke(width = 3.dp.toPx()),
                    )
                }
        )
        Canvas(Modifier.size(126.dp)) {
            val stroke = 5.dp.toPx()
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
            modifier = Modifier.size(104.dp).clip(CircleShape).background(colors.accent),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Filled.Mic,
                contentDescription = null,
                tint = Color.White,
                modifier = Modifier.size(44.dp),
            )
        }
    }
}

@Composable
private fun Waveform() {
    val base = listOf(10, 26, 40, 19, 32, 46, 15, 29, 37, 21, 11)
    var phase by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        // Волна живая, но нарочно неспешная: экран объясняет, а не пляшет.
        while (true) {
            delay(130)
            phase++
        }
    }
    Row(
        modifier = Modifier.height(48.dp),
        horizontalArrangement = Arrangement.spacedBy(5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        base.indices.forEach { index ->
            val height by animateDpAsState(base[(index + phase) % base.size].dp, label = "bar")
            Box(
                Modifier
                    .width(4.5.dp)
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
            .padding(20.dp),
    ) {
        Row(Modifier.fillMaxWidth()) {
            Text(
                stringResource(R.string.welcome_rcpt_items),
                fontSize = 15.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.inkSecondary,
            )
            Spacer(Modifier.weight(1f))
            Text(
                stringResource(R.string.welcome_rcpt_count),
                fontSize = 15.sp,
                fontFamily = FontFamily.Monospace,
                color = colors.inkSecondary,
            )
        }
        ReceiptItem("Пицца", "800 ₽", "по 400 ₽ × 2")
        ReceiptItem("Кола", "200 ₽", "по 100 ₽ × 2")
        Spacer(Modifier.height(14.dp))
        Box(Modifier.fillMaxWidth().height(2.dp).background(colors.ink))
        Spacer(Modifier.height(14.dp))
        Row(Modifier.fillMaxWidth()) {
            Text(
                stringResource(R.string.welcome_rcpt_total),
                fontSize = 20.sp,
                fontWeight = FontWeight.Bold,
                color = colors.ink,
            )
            Spacer(Modifier.weight(1f))
            Text(
                "1000 ₽",
                fontSize = 24.sp,
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
    Spacer(Modifier.height(10.dp))
    Box(Modifier.fillMaxWidth().height(1.dp).background(colors.hairline))
    Spacer(Modifier.height(10.dp))
    Row(Modifier.fillMaxWidth()) {
        Text(name, fontSize = 19.sp, fontWeight = FontWeight.Bold, color = colors.ink)
        Spacer(Modifier.weight(1f))
        Text(
            sum,
            fontSize = 19.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = FontFamily.Monospace,
            color = colors.ink,
        )
    }
    Row(Modifier.fillMaxWidth().padding(top = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Avatar("Я", colors.accent, size = 24)
        Spacer(Modifier.width(2.dp))
        Avatar("С", colors.chartCategorical[1], size = 24)
        Spacer(Modifier.weight(1f))
        Text(each, fontSize = 14.sp, fontFamily = FontFamily.Monospace, color = colors.inkSecondary)
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
                delay(if (it == 2) 520 else 380)
                shown = it + 1
            }
            delay(2400)
            shown = 0
            delay(400)
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Eyebrow(stringResource(R.string.welcome_paid_eyebrow))

        PaidCard(
            initial = "А",
            who = stringResource(R.string.welcome_paid_anya),
            what = stringResource(R.string.welcome_for_dinner),
            sum = "600 ₽",
            share = "по 200 ₽",
            color = colors.accent,
            visible = shown > 0,
        )
        PaidCard(
            initial = "Б",
            who = stringResource(R.string.welcome_paid_borya),
            what = stringResource(R.string.welcome_for_taxi),
            sum = "300 ₽",
            share = "по 100 ₽",
            color = colors.chartCategorical[2],
            visible = shown > 1,
        )

        Spacer(Modifier.weight(1f))
        Box(Modifier.fillMaxWidth().graphicsLayer { alpha = if (shown > 2) 1f else 0f }) { ArrowDown() }
        Spacer(Modifier.weight(1f))

        val summaryAlpha by animateFloatAsState(if (shown > 2) 1f else 0f, label = "summary")
        Card(
            modifier = Modifier.fillMaxWidth().graphicsLayer { alpha = summaryAlpha },
            background = colors.accent.copy(alpha = 0.11f),
            elevation = 0.dp,
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Avatar("Я", colors.inkSecondary, size = 44)
                    Column {
                        Text(
                            stringResource(R.string.welcome_you_paid_nothing),
                            fontSize = 20.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = colors.ink,
                        )
                        Text(
                            stringResource(R.string.welcome_your_share),
                            fontSize = 15.sp,
                            color = colors.inkSecondary,
                        )
                    }
                    Spacer(Modifier.weight(1f))
                    Text(
                        "300 ₽",
                        fontSize = 26.sp,
                        fontWeight = FontWeight.Bold,
                        fontFamily = FontFamily.Monospace,
                        color = colors.accentText,
                    )
                }
                Box(Modifier.fillMaxWidth().height(1.dp).background(colors.accent.copy(alpha = 0.25f)))
                // Сумма долей выписана, чтобы 300 ₽ можно было проверить в уме.
                Text(
                    stringResource(R.string.welcome_share_math),
                    fontSize = 15.sp,
                    fontFamily = FontFamily.Monospace,
                    color = colors.accentText.copy(alpha = 0.75f),
                )
            }
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
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Avatar(initial, color, size = 44)
                Column {
                    Text(who, fontSize = 19.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
                    Text(what, fontSize = 14.sp, color = colors.inkSecondary)
                }
                Spacer(Modifier.weight(1f))
                Text(
                    sum,
                    fontSize = 26.sp,
                    fontWeight = FontWeight.Bold,
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
                    fontSize = 16.sp,
                    color = colors.inkSecondary,
                )
                Box(
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(colors.accent.copy(alpha = 0.14f))
                        .padding(horizontal = 12.dp, vertical = 5.dp),
                ) {
                    Text(
                        share,
                        fontSize = 16.sp,
                        fontWeight = FontWeight.Bold,
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
        delay(1700)
        if (!touched) withSplitty = true
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Card(modifier = Modifier.fillMaxWidth().height(68.dp)) {
            Row(
                Modifier.fillMaxSize().padding(horizontal = 18.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    stringResource(R.string.welcome_evening_share),
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                )
                Spacer(Modifier.weight(1f))
                Text(
                    "300 ₽",
                    fontSize = 24.sp,
                    fontWeight = FontWeight.Bold,
                    fontFamily = FontFamily.Monospace,
                    color = colors.ink,
                )
            }
        }

        Eyebrow(stringResource(R.string.welcome_you_transfer))

        PayRow(
            initial = "А",
            name = stringResource(R.string.welcome_to_anya),
            note = stringResource(
                if (withSplitty) R.string.welcome_anya_note_with else R.string.welcome_for_dinner
            ),
            sum = if (withSplitty) "300 ₽" else "200 ₽",
            avatarColor = colors.accent,
            sumColor = if (withSplitty) colors.accentText else colors.negative,
        )

        // Место под вторую строку занято в обоих состояниях: слева перевод,
        // справа объяснение, почему его больше нет. Иначе при переключении низ
        // экрана прыгает, а половина карточки пустует.
        Box(Modifier.fillMaxWidth().height(140.dp), contentAlignment = Alignment.TopStart) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                if (!withSplitty) {
                    PayRow(
                        initial = "Б",
                        name = stringResource(R.string.welcome_to_borya),
                        note = stringResource(R.string.welcome_for_taxi),
                        sum = "100 ₽",
                        avatarColor = colors.chartCategorical[2],
                        sumColor = colors.negative,
                    )
                    SideNote(stringResource(R.string.welcome_note_without), colors.negative)
                } else {
                    SettledRow()
                    SideNote(stringResource(R.string.welcome_note_with), colors.accentText)
                }
            }
        }

        Spacer(Modifier.weight(1f))

        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(18.dp))
                .background((if (withSplitty) colors.accent else colors.negative).copy(alpha = 0.1f))
                .padding(vertical = 13.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterHorizontally),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            repeat(2) { index ->
                val filled = if (withSplitty) index == 0 else true
                Box(
                    Modifier
                        .size(width = 15.dp, height = 22.dp)
                        .clip(RoundedCornerShape(3.dp))
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
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
                color = if (withSplitty) colors.accentText else colors.negative,
            )
        }

        CompareSegment(withSplitty = withSplitty) {
            touched = true
            withSplitty = it
        }
    }
}

@Composable
private fun SideNote(text: String, color: Color) {
    Text(text, fontSize = 15.sp, color = color, modifier = Modifier.fillMaxWidth())
}

/**
 * Боря заплатил ровно свою долю: его баланс ноль, поэтому строка «вам
 * переводить» для него исчезает — это и есть сведение долгов.
 */
@Composable
private fun SettledRow() {
    val colors = Splitty.colors
    Card(
        modifier = Modifier.fillMaxWidth().height(96.dp),
        radius = 22.dp,
        background = colors.accent.copy(alpha = 0.12f),
        elevation = 0.dp,
    ) {
        Row(
            Modifier.fillMaxSize().padding(horizontal = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Avatar("Б", colors.chartCategorical[2].copy(alpha = 0.35f), size = 48)
            Column {
                Text(
                    stringResource(R.string.welcome_to_borya),
                    fontSize = 20.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.inkSecondary,
                )
                Text(stringResource(R.string.welcome_settled), fontSize = 15.sp, color = colors.inkSecondary)
            }
            Spacer(Modifier.weight(1f))
            Text(
                "0 ₽",
                fontSize = 26.sp,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                color = colors.accentText,
            )
        }
    }
}

@Composable
private fun PayRow(
    initial: String,
    name: String,
    note: String,
    sum: String,
    avatarColor: Color,
    sumColor: Color,
) {
    val colors = Splitty.colors
    Card(modifier = Modifier.fillMaxWidth().height(96.dp), radius = 22.dp) {
        Row(
            modifier = Modifier.fillMaxSize().padding(horizontal = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Avatar(initial, avatarColor, size = 48)
            Column {
                Text(name, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
                Text(note, fontSize = 15.sp, color = colors.inkSecondary)
            }
            Spacer(Modifier.weight(1f))
            Text(
                sum,
                fontSize = 26.sp,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                color = sumColor,
            )
        }
    }
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
            .padding(5.dp)
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
            .padding(vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (selected) Color.White else colors.inkSecondary,
        )
    }
}
