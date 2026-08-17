package com.zagir.splitty.ui.onboarding

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SurfaceCard
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
                Text(stringResource(R.string.welcome_skip), fontSize = 15.sp, color = colors.inkSecondary)
            }
        }

        HorizontalPager(state = pagerState, modifier = Modifier.weight(1f)) { page ->
            when (page) {
                0 -> WelcomePage(R.string.welcome_1_title, R.string.welcome_1_body) { SharedBillArt() }
                1 -> WelcomePage(R.string.welcome_2_title, R.string.welcome_2_body) { DictationArt() }
                2 -> WelcomePage(R.string.welcome_3_title, R.string.welcome_3_body) { WhoPaidArt() }
                else -> WelcomePage(R.string.welcome_4_title, R.string.welcome_4_body) { TransfersArt() }
            }
        }

        PageDots(count = pages, current = pagerState.currentPage)
        Spacer(Modifier.height(18.dp))

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
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(horizontal = 16.dp)
                .clip(RoundedCornerShape(18.dp))
                .background(colors.accent.copy(alpha = 0.07f)),
        ) { art() }

        Column(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 18.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = stringResource(titleRes),
                fontSize = 21.sp,
                fontWeight = FontWeight.Bold,
                color = colors.ink,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(bodyRes),
                fontSize = 14.sp,
                lineHeight = 19.sp,
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
        horizontalArrangement = Arrangement.spacedBy(6.dp, Alignment.CenterHorizontally),
    ) {
        repeat(count) { index ->
            val width by animateDpAsState(if (index == current) 18.dp else 6.dp, label = "dot")
            Box(
                Modifier
                    .width(width)
                    .height(6.dp)
                    .clip(CircleShape)
                    .background(if (index == current) colors.accent else colors.hairline)
            )
        }
    }
}

/** Расходы складываются в общий счёт группы. */
@Composable
private fun SharedBillArt() {
    val colors = Splitty.colors
    var visible by remember { mutableIntStateOf(0) }
    val slips = listOf("Ужин" to "600 ₽", "Такси" to "300 ₽", "Продукты" to "450 ₽")

    LaunchedEffect(Unit) {
        repeat(slips.size) {
            delay(220)
            visible = it + 1
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        slips.forEachIndexed { index, slip ->
            AnimatedVisibility(visible = index < visible, enter = fadeIn()) {
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 9.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(slip.first, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.weight(1f))
                        Text(slip.second, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
        }
        Spacer(Modifier.weight(1f))
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(13.dp))
                .border(1.5.dp, colors.accent.copy(alpha = 0.55f), RoundedCornerShape(13.dp))
                .padding(vertical = 13.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                stringResource(R.string.welcome_bill_tray),
                fontSize = 12.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.accentText,
            )
        }
    }
}

/** Запись голоса и мини-чек, который из неё получается. */
@Composable
private fun DictationArt() {
    val colors = Splitty.colors
    val phrase = listOf("пицца", "за", "восемьсот", "и", "кола", "за", "двести", "пополам", "с", "Саней")
    var words by remember { mutableIntStateOf(0) }
    var showReceipt by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        while (true) {
            showReceipt = false
            words = 0
            repeat(phrase.size) {
                delay(220)
                words = it + 1
            }
            delay(700)
            showReceipt = true
            delay(2400)
        }
    }

    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        if (showReceipt) {
            MiniReceipt(Modifier.padding(horizontal = 26.dp))
        } else {
            Column(
                modifier = Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color(0xFF2B313A)),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                Text(
                    text = phrase.take(words).joinToString(" "),
                    fontSize = 13.sp,
                    fontWeight = FontWeight.Bold,
                    color = androidx.compose.ui.graphics.Color.White,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.height(54.dp).padding(horizontal = 14.dp),
                )
                Spacer(Modifier.height(10.dp))
                Box(
                    modifier = Modifier.size(52.dp).clip(CircleShape).background(colors.accent),
                    contentAlignment = Alignment.Center,
                ) {
                    Text("🎙", fontSize = 20.sp)
                }
                Spacer(Modifier.height(10.dp))
                Text(
                    stringResource(R.string.welcome_rec_hint),
                    fontSize = 9.sp,
                    color = androidx.compose.ui.graphics.Color.White.copy(alpha = 0.5f),
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}

/** Мини-чек — уменьшенный ReceiptCard с экрана расхода. */
@Composable
private fun MiniReceipt(modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(3.dp))
            .background(colors.receiptPaper)
            .padding(12.dp),
    ) {
        Row(Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.welcome_rcpt_items), fontSize = 9.sp, color = colors.inkSecondary)
            Spacer(Modifier.weight(1f))
            Text(stringResource(R.string.welcome_rcpt_count), fontSize = 9.sp, color = colors.inkSecondary)
        }
        ReceiptItem("Пицца", "800 ₽", "по 400 ₽ × 2")
        ReceiptItem("Кола", "200 ₽", "по 100 ₽ × 2")
        Spacer(Modifier.height(7.dp))
        Box(Modifier.fillMaxWidth().height(1.5.dp).background(colors.ink))
        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.welcome_rcpt_total), fontSize = 12.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.weight(1f))
            Text("1000 ₽", fontSize = 13.sp, fontWeight = FontWeight.Bold)
        }
    }
}

@Composable
private fun ReceiptItem(name: String, sum: String, each: String) {
    val colors = Splitty.colors
    Spacer(Modifier.height(7.dp))
    Box(Modifier.fillMaxWidth().height(1.dp).background(colors.hairline))
    Spacer(Modifier.height(7.dp))
    Row(Modifier.fillMaxWidth()) {
        Text(name, fontSize = 11.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.weight(1f))
        Text(sum, fontSize = 11.sp, fontWeight = FontWeight.Bold)
    }
    Row(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        Spacer(Modifier.weight(1f))
        Text(each, fontSize = 9.sp, color = colors.inkSecondary)
    }
}

/** Кто сколько заплатил — предыстория для последнего экрана. */
@Composable
private fun WhoPaidArt() {
    val colors = Splitty.colors
    Column(
        modifier = Modifier.fillMaxSize().padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        PaidCard("А", stringResource(R.string.welcome_paid_anya), "ужин", "600 ₽", "по 200 ₽", colors.accent)
        PaidCard("Б", stringResource(R.string.welcome_paid_borya), "такси", "300 ₽", "по 100 ₽", colors.chartCategorical[2])
        Spacer(Modifier.weight(1f))
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(13.dp))
                .background(colors.accent.copy(alpha = 0.1f))
                .padding(11.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Avatar("Я", colors.inkSecondary)
            Column {
                Text(stringResource(R.string.welcome_you_paid_nothing), fontSize = 11.5.sp, fontWeight = FontWeight.SemiBold)
                Text(stringResource(R.string.welcome_your_share), fontSize = 10.sp, color = colors.inkSecondary)
            }
            Spacer(Modifier.weight(1f))
            Text("300 ₽", fontSize = 14.sp, fontWeight = FontWeight.Bold, color = colors.accentText)
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
    color: androidx.compose.ui.graphics.Color,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 11.dp) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Avatar(initial, color)
            Column {
                Text(who, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                Text(what, fontSize = 9.5.sp, color = colors.inkSecondary)
            }
            Spacer(Modifier.weight(1f))
            Text(sum, fontSize = 14.sp, fontWeight = FontWeight.Bold)
        }
        Spacer(Modifier.height(9.dp))
        Box(Modifier.fillMaxWidth().height(1.dp).background(colors.hairline))
        Spacer(Modifier.height(9.dp))
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.welcome_split_three), fontSize = 10.sp, color = colors.inkSecondary)
            Box(
                modifier = Modifier
                    .clip(CircleShape)
                    .background(colors.accent.copy(alpha = 0.13f))
                    .padding(horizontal = 8.dp, vertical = 3.dp),
            ) {
                Text(share, fontSize = 10.sp, fontWeight = FontWeight.Bold, color = colors.accentText)
            }
        }
    }
}

/**
 * Сколько раз платить: сравнение «Без Splitty / Со Splitty» переключателем.
 * Темп задаёт человек — автоматическая смена состояний читалась как мельтешение.
 */
@Composable
private fun TransfersArt() {
    val colors = Splitty.colors
    var withSplitty by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier.fillMaxSize().padding(13.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Text(
            stringResource(R.string.welcome_you_transfer),
            fontSize = 9.5.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.inkSecondary,
        )

        PayRow("А", stringResource(R.string.welcome_to_anya), if (withSplitty) "300 ₽" else "200 ₽",
            colors.accent, if (withSplitty) colors.accentText else colors.negative)

        AnimatedVisibility(visible = !withSplitty, exit = shrinkVertically() + fadeOut(), enter = fadeIn()) {
            PayRow("Б", stringResource(R.string.welcome_to_borya), "100 ₽", colors.chartCategorical[2], colors.negative)
        }

        AnimatedVisibility(visible = withSplitty, enter = fadeIn(tween(300)), exit = fadeOut()) {
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(colors.accent.copy(alpha = 0.11f))
                    .padding(10.dp),
                horizontalArrangement = Arrangement.spacedBy(7.dp),
            ) {
                Text("✓", fontSize = 10.sp, fontWeight = FontWeight.Bold, color = colors.accentText)
                Text(
                    stringResource(R.string.welcome_why),
                    fontSize = 10.5.sp,
                    lineHeight = 14.sp,
                    color = colors.accentText,
                )
            }
        }

        Spacer(Modifier.weight(1f))

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(7.dp, Alignment.CenterHorizontally),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            repeat(2) { index ->
                val filled = if (withSplitty) index == 0 else true
                Box(
                    Modifier
                        .size(width = 9.dp, height = 12.dp)
                        .clip(RoundedCornerShape(2.dp))
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
                fontSize = 11.sp,
                fontWeight = FontWeight.Bold,
                color = if (withSplitty) colors.accentText else colors.negative,
            )
        }

        CompareSegment(withSplitty = withSplitty, onChange = { withSplitty = it })
    }
}

@Composable
private fun CompareSegment(withSplitty: Boolean, onChange: (Boolean) -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(CircleShape)
            .background(colors.ink.copy(alpha = 0.06f))
            .padding(3.dp)
            .testTag("welcome_compare"),
        horizontalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        SegmentHalf(stringResource(R.string.welcome_without), !withSplitty) { onChange(false) }
        SegmentHalf(stringResource(R.string.welcome_with), withSplitty) { onChange(true) }
    }
}

@Composable
private fun androidx.compose.foundation.layout.RowScope.SegmentHalf(
    text: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .weight(1f)
            .clip(CircleShape)
            .background(if (selected) colors.accent else androidx.compose.ui.graphics.Color.Transparent)
            .clickable(onClick = onClick)
            .padding(vertical = 7.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            fontSize = 10.5.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (selected) androidx.compose.ui.graphics.Color.White else colors.inkSecondary,
        )
    }
}



@Composable
private fun PayRow(
    initial: String,
    name: String,
    sum: String,
    avatarColor: androidx.compose.ui.graphics.Color,
    sumColor: androidx.compose.ui.graphics.Color,
) {
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        Row(
            modifier = Modifier.fillMaxWidth().height(46.dp).padding(horizontal = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Avatar(initial, avatarColor, size = 28)
            Text(name, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            Text(sum, fontSize = 14.sp, fontWeight = FontWeight.Bold, color = sumColor)
        }
    }
}

@Composable
private fun Avatar(letter: String, color: androidx.compose.ui.graphics.Color, size: Int = 26) {
    Box(
        modifier = Modifier.size(size.dp).clip(CircleShape).background(color),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            letter,
            fontSize = (size * 0.42f).sp,
            fontWeight = FontWeight.Bold,
            color = androidx.compose.ui.graphics.Color.White,
        )
    }
}
