package com.zagir.splitty.ui.expense

import android.os.SystemClock
import androidx.compose.animation.Crossfade
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.outlined.LockOpen
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameMillis
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.scale
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.ui.components.rememberReduceMotion
import com.zagir.splitty.ui.theme.Splitty
import kotlinx.coroutines.delay
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.roundToInt
import kotlin.math.sin

@Composable
fun RecordingOverlay(
    isActive: Boolean,
    isCancelling: Boolean,
    isLocked: Boolean,
    isPreparing: Boolean,
    /** Смещение пальца от точки нажатия, в **dp** (см. [micTouchGesture]). */
    dragX: Float,
    dragY: Float,
    level: Float,
    startedAtElapsedMs: Long?,
    micFrame: androidx.compose.ui.geometry.Rect?,
    hints: List<String>,
    transcript: String? = null,
    onStop: () -> Unit,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
    /**
     * Замороженное «сейчас» для снапшот-тестов. Волна и кольцо считают фазу от
     * живых часов, поэтому без пина картинка зависит от того, сколько миллисекунд
     * прошло до захвата — снапшоты расходились между прогонами. В проде — null.
     */
    frozenNowMs: Long? = null,
) {
    val reduceMotion = rememberReduceMotion()
    // Один тик кормит таймер, кольцо и волну: скрытый оверлей не держит
    // отдельные вечные анимации, а reduce motion оставляет только редкий таймер.
    val tickedNowMs by produceState(
        initialValue = SystemClock.elapsedRealtime(),
        isActive,
        reduceMotion,
    ) {
        while (isActive) {
            value = SystemClock.elapsedRealtime()
            if (reduceMotion) delay(400) else withFrameMillis { }
        }
        value = SystemClock.elapsedRealtime()
    }
    val nowMs = frozenNowMs ?: tickedNowMs
    val overlayAlpha by animateFloatAsState(
        targetValue = if (isActive) 1f else 0f,
        animationSpec = if (reduceMotion) snap() else tween(durationMillis = 120),
        label = "recordingOverlayAlpha",
    )
    val cancelAlpha by animateFloatAsState(
        targetValue = if (isCancelling) 1f else 0f,
        animationSpec = if (reduceMotion) snap() else tween(durationMillis = 150),
        label = "recordingCancelAlpha",
    )
    val activeScale by animateFloatAsState(
        targetValue = if (isActive || reduceMotion) 1f else 0.86f,
        animationSpec = if (reduceMotion) snap() else tween(durationMillis = 180),
        label = "recordingMicPop",
    )
    // Через общие хелперы, а не свои /70f: пороги жеста и их визуализация обязаны
    // расходиться только вместе.
    val lockProgress = micLockProgress(dragY)
    val cancelProgress = micCancelProgress(dragX)
    val elapsedMs = startedAtElapsedMs
        ?.let { (nowMs - it).coerceAtLeast(0L).coerceAtMost(RECORD_LIMIT_SECONDS * 1_000L) }
    val progress = elapsedMs?.toFloat()?.div(RECORD_LIMIT_SECONDS * 1_000f) ?: 0f
    val spinDegrees = rememberRecordingSpinDegrees(
        enabled = isPreparing && isActive,
        reduceMotion = reduceMotion,
    )
    val pulsePhase = rememberRecordingPulsePhase(
        enabled = isActive && !isCancelling && !isPreparing,
        reduceMotion = reduceMotion,
    )

    // Смещение корня оверлея в координатах композиции: оверлей лежит внутри контента
    // MainScaffold (ниже статус-бара и баннера сети), а micFrame приходит в boundsInRoot
    // (от верха окна). Без коррекции мик съезжал ниже реальной кнопки, а scrim не доставал
    // системные бары (белые полосы сверху/снизу).
    var rootOffset by remember { mutableStateOf(Offset.Zero) }
    val windowSizePx = LocalWindowInfo.current.containerSize
    BoxWithConstraints(
        modifier = modifier
            .fillMaxSize()
            .onGloballyPositioned { rootOffset = it.positionInRoot() },
    ) {
        val density = androidx.compose.ui.platform.LocalDensity.current
        val rootWidthPx = constraints.maxWidth.toFloat()
        val rootHeightPx = constraints.maxHeight.toFloat()
        val defaultMicSizePx = with(density) { 82.dp.toPx() }
        val fallbackCenterY = rootHeightPx - with(density) { 86.dp.toPx() }
        // micFrame (boundsInRoot) → в локальные координаты оверлея, вычитая его смещение.
        val micCenter = micFrame?.let {
            Offset(it.center.x - rootOffset.x, it.center.y - rootOffset.y)
        } ?: Offset(rootWidthPx / 2f, fallbackCenterY)
        val micSizePx = micFrame?.width ?: defaultMicSizePx
        val micSize = with(density) { micSizePx.toDp() }
        // После «замка» палец уже отпущен: кнопка стопа остаётся на месте,
        // а не наследует последний drag обычного hold-жеста.
        // dragX/dragY приходят в dp, а micCenter — в пикселях: переводим, иначе
        // кнопка следовала бы за пальцем с плотность-зависимым коэффициентом.
        val micOffset = if (isLocked) {
            Offset.Zero
        } else {
            with(density) {
                Offset(
                    max(dragX, -130f).dp.toPx() * 0.4f,
                    max(dragY, -130f).dp.toPx() * 0.4f,
                )
            }
        }
        val lockedExtraPx = with(density) { (if (isLocked) 52.dp else 100.dp).toPx() }
        val contentBottom = (
            rootHeightPx - micCenter.y + micSizePx / 2f + lockedExtraPx
        ).coerceAtLeast(0f)
        val contentBottomDp = with(density) { contentBottom.toDp() }

        // Фон на ВСЁ окно (перекрывая статус-бар и навбар): оверлей лежит внутри
        // контента MainScaffold, поэтому matchParentSize() кроет только его
        // область — по краям оставались светлые полосы, и слой читался чёрной
        // панелью, а не полноэкранным экраном записи (на iOS это .ignoresSafeArea).
        // Сдвигаем слой обратно на rootOffset и растягиваем на размер окна.
        Box(
            modifier = Modifier
                .offset { IntOffset(-rootOffset.x.roundToInt(), -rootOffset.y.roundToInt()) }
                .size(
                    width = with(density) { windowSizePx.width.toDp() },
                    height = with(density) { windowSizePx.height.toDp() },
                )
                .alpha(overlayAlpha)
                .background(RecordingScrim),
        )

        RecordingContent(
            hints = hints,
            showHints = hints.isNotEmpty() && !isCancelling,
            transcript = transcript,
            isActive = isActive,
            isCancelling = isCancelling,
            isLocked = isLocked,
            isPreparing = isPreparing,
            reduceMotion = reduceMotion,
            level = level,
            elapsedMs = elapsedMs,
            nowMs = nowMs,
            alpha = overlayAlpha,
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 34.dp)
                .padding(bottom = contentBottomDp),
        )

        if (!isLocked) {
            LockZone(
                progress = lockProgress,
                modifier = Modifier
                    .alpha(overlayAlpha)
                    .size(72.dp)
                    .offset {
                        val box = with(density) { 72.dp.toPx() }
                        IntOffset(
                            x = (micCenter.x - box / 2f).roundToInt(),
                            y = (
                                micCenter.y - micSizePx / 2f -
                                    with(density) { 58.dp.toPx() } - box / 2f
                                ).roundToInt(),
                        )
                    },
            )
        } else {
            Text(
                text = stringResource(R.string.rec_locked_hint),
                modifier = Modifier
                    .alpha(overlayAlpha)
                    .width(240.dp)
                    .offset {
                        val w = with(density) { 240.dp.toPx() }
                        val h = with(density) { 24.dp.toPx() }
                        IntOffset(
                            x = (micCenter.x - w / 2f).roundToInt(),
                            y = (
                                micCenter.y - micSizePx / 2f -
                                    with(density) { 24.dp.toPx() } - h / 2f
                                ).roundToInt(),
                        )
                    },
                color = Color.White.copy(alpha = 0.7f),
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                textAlign = TextAlign.Center,
            )
        }

        CancelControl(
            isLocked = isLocked,
            isCancelling = isCancelling,
            progress = cancelProgress,
            onCancel = onCancel,
            modifier = Modifier
                .alpha(overlayAlpha * if (isCancelling) 0.25f else 1f)
                .size(width = 78.dp, height = 56.dp)
                .offset {
                    val boxWidth = with(density) { 78.dp.toPx() }
                    val boxHeight = with(density) { 56.dp.toPx() }
                    val leftLimit = with(density) { 58.dp.toPx() }
                    val controlX = max(
                        leftLimit,
                        micCenter.x - micSizePx / 2f - with(density) { 66.dp.toPx() },
                    )
                    IntOffset(
                        x = (controlX - boxWidth / 2f).roundToInt(),
                        y = (micCenter.y - boxHeight / 2f).roundToInt(),
                    )
                },
        )

        MicOrStop(
            isLocked = isLocked,
            isCancelling = isCancelling,
            isPreparing = isPreparing,
            reduceMotion = reduceMotion,
            level = level,
            progress = progress,
            hasStarted = startedAtElapsedMs != null,
            spinDegrees = spinDegrees,
            pulsePhase = pulsePhase,
            cancelAlpha = cancelAlpha,
            size = micSize,
            onStop = onStop,
            modifier = Modifier
                .alpha(overlayAlpha)
                .size(micSize + 96.dp)
                .offset {
                    val box = with(density) { (micSize + 96.dp).toPx() }
                    IntOffset(
                        x = (micCenter.x + micOffset.x - box / 2f).roundToInt(),
                        y = (micCenter.y + micOffset.y - box / 2f).roundToInt(),
                    )
                }
                .scale(activeScale),
        )
    }
}

@Composable
private fun RecordingContent(
    hints: List<String>,
    showHints: Boolean,
    transcript: String?,
    isActive: Boolean,
    isCancelling: Boolean,
    isLocked: Boolean,
    isPreparing: Boolean,
    reduceMotion: Boolean,
    level: Float,
    elapsedMs: Long?,
    nowMs: Long,
    alpha: Float,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.alpha(alpha),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Bottom,
    ) {
        if (transcript != null && !isCancelling) {
            KaraokeWindow(
                transcript = transcript,
                reduceMotion = reduceMotion,
                modifier = Modifier.padding(bottom = 20.dp),
            )
        }
        if (showHints) {
            HintsCard(hints = hints)
            Spacer(modifier = Modifier.height(24.dp))
        }
        Waveform(
            active = isActive && !isCancelling && !reduceMotion,
            level = level,
            timeSeconds = nowMs / 1_000.0,
            modifier = Modifier
                .size(width = 240.dp, height = 44.dp)
                .alpha(if (isCancelling) 0.25f else 1f),
        )
        if (elapsedMs != null) {
            TimerLabel(
                elapsedMs = elapsedMs,
                reduceMotion = reduceMotion,
                modifier = Modifier.padding(top = 12.dp),
            )
        }
        StatusTexts(
            status = when {
                isCancelling -> RecordingStatus.Cancelling
                isLocked -> RecordingStatus.Locked
                isPreparing -> RecordingStatus.Preparing
                else -> RecordingStatus.Recording
            },
            reduceMotion = reduceMotion,
            modifier = Modifier.padding(top = 12.dp),
        )
    }
}

@Composable
private fun LockZone(
    progress: Float,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier = Modifier
                .size(44.dp)
                .scale(1f + progress * 0.3f)
                .clip(CircleShape)
                .background(
                    if (progress > 0.99f) colors.accent else Color.White.copy(alpha = 0.12f),
                )
                .border(1.5.dp, Color.White.copy(alpha = 0.3f), CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = if (progress > 0.5f) Icons.Filled.Lock else Icons.Outlined.LockOpen,
                contentDescription = null,
                tint = Color.White.copy(alpha = 0.9f),
                modifier = Modifier.size(18.dp),
            )
        }
        Icon(
            imageVector = Icons.Filled.KeyboardArrowUp,
            contentDescription = null,
            tint = Color.White.copy(alpha = 0.55f),
            modifier = Modifier.size(18.dp),
        )
    }
}

@Composable
private fun CancelControl(
    isLocked: Boolean,
    isCancelling: Boolean,
    progress: Float,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    val cancelA11y = stringResource(R.string.rec_cancel_a11y)
    val interactionSource = remember { MutableInteractionSource() }
    val clickModifier = if (isLocked) {
        Modifier
            .semantics { contentDescription = cancelA11y }
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                role = Role.Button,
                onClick = onCancel,
            )
    } else {
        Modifier
    }
    Row(
        modifier = modifier.then(clickModifier),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        Icon(
            imageVector = Icons.Filled.KeyboardArrowLeft,
            contentDescription = null,
            tint = Color.White.copy(alpha = 0.55f),
            modifier = Modifier.size(18.dp),
        )
        Box(
            modifier = Modifier
                .size(44.dp)
                .scale(1f + progress * 0.3f)
                .clip(CircleShape)
                .background(
                    if (isCancelling) colors.negative else Color.White.copy(alpha = 0.12f),
                )
                .border(
                    width = 1.5.dp,
                    color = if (isCancelling) colors.negative else Color.White.copy(alpha = 0.3f),
                    shape = CircleShape,
                ),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Filled.Close,
                contentDescription = null,
                tint = Color.White.copy(alpha = 0.9f),
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

@Composable
private fun MicOrStop(
    isLocked: Boolean,
    isCancelling: Boolean,
    isPreparing: Boolean,
    reduceMotion: Boolean,
    level: Float,
    progress: Float,
    hasStarted: Boolean,
    spinDegrees: Float,
    pulsePhase: Float,
    cancelAlpha: Float,
    size: androidx.compose.ui.unit.Dp,
    onStop: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(modifier = modifier, contentAlignment = Alignment.Center) {
        if (!isLocked && !reduceMotion && !isCancelling && !isPreparing) {
            PulseRing(size = size, phase = pulsePhase)
        }
        LimitAndPreparingRing(
            size = size,
            progress = progress,
            hasStarted = hasStarted,
            isPreparing = isPreparing,
            spinDegrees = spinDegrees,
        )
        if (isLocked) {
            StopButton(size = size, onStop = onStop)
        } else {
            MicCircle(
                size = size,
                isCancelling = isCancelling,
                isPreparing = isPreparing,
                reduceMotion = reduceMotion,
                level = level,
                cancelAlpha = cancelAlpha,
            )
        }
    }
}

@Composable
private fun PulseRing(size: androidx.compose.ui.unit.Dp, phase: Float) {
    Box(
        modifier = Modifier
            .size(size)
            .scale(1f + phase * 0.7f)
            .alpha((0.7f * (1f - phase)).coerceIn(0f, 0.7f))
            .border(2.dp, Splitty.colors.accent.copy(alpha = 0.5f), CircleShape),
    )
}

@Composable
private fun LimitAndPreparingRing(
    size: androidx.compose.ui.unit.Dp,
    progress: Float,
    hasStarted: Boolean,
    isPreparing: Boolean,
    spinDegrees: Float,
) {
    val colors = Splitty.colors
    val ringSize = size + 16.dp
    Canvas(modifier = Modifier.size(ringSize)) {
        if (hasStarted) {
            drawArc(
                color = if (progress > 0.85f) colors.negative else Color.White.copy(alpha = 0.9f),
                startAngle = -90f,
                sweepAngle = 360f * progress.coerceIn(0f, 1f),
                useCenter = false,
                style = Stroke(width = 3.5.dp.toPx(), cap = StrokeCap.Round),
            )
        }
        if (isPreparing) {
            drawArc(
                color = Color.White.copy(alpha = 0.85f),
                startAngle = spinDegrees - 90f,
                sweepAngle = 360f * 0.28f,
                useCenter = false,
                style = Stroke(width = 3.dp.toPx(), cap = StrokeCap.Round),
            )
        }
    }
}

@Composable
private fun StopButton(
    size: androidx.compose.ui.unit.Dp,
    onStop: () -> Unit,
) {
    val colors = Splitty.colors
    val stopA11y = stringResource(R.string.rec_stop_a11y)
    val interactionSource = remember { MutableInteractionSource() }
    Box(
        modifier = Modifier
            .size(size)
            .shadow(
                elevation = 20.dp,
                shape = CircleShape,
                ambientColor = colors.accent.copy(alpha = 0.5f),
                spotColor = colors.accent.copy(alpha = 0.5f),
            )
            .clip(CircleShape)
            .background(
                Brush.linearGradient(listOf(colors.accent, colors.accentPressed)),
            )
            .semantics { contentDescription = stopA11y }
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                role = Role.Button,
                onClick = onStop,
            ),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = Icons.Filled.Stop,
            contentDescription = null,
            tint = Color.White,
            modifier = Modifier.size(size * 0.38f),
        )
    }
}

@Composable
private fun MicCircle(
    size: androidx.compose.ui.unit.Dp,
    isCancelling: Boolean,
    isPreparing: Boolean,
    reduceMotion: Boolean,
    level: Float,
    cancelAlpha: Float,
) {
    val colors = Splitty.colors
    val breathScale = if (reduceMotion) 1f else 1f + level.coerceIn(0f, 1f) * 0.1f
    Box(
        modifier = Modifier
            .size(size)
            .scale(breathScale)
            .shadow(
                elevation = 20.dp,
                shape = CircleShape,
                ambientColor = (if (isCancelling) colors.negative else colors.accent)
                    .copy(alpha = 0.5f),
                spotColor = (if (isCancelling) colors.negative else colors.accent)
                    .copy(alpha = 0.5f),
            )
            .clip(CircleShape)
            .background(Brush.linearGradient(listOf(colors.accent, colors.accentPressed)))
            .alpha(if (isPreparing) 0.75f else 1f),
        contentAlignment = Alignment.Center,
    ) {
        // Красный слой отмены рисуем ТОЛЬКО когда действительно отменяем: полагаться
        // на .alpha(0f) поверх зелёного мика нельзя — фон-слой с нулевой альфой всё
        // равно просвечивал (оранжевый мик в покое). cancelAlpha даёт кроссфейд.
        if (cancelAlpha > 0f) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .alpha(cancelAlpha)
                    .background(
                        Brush.linearGradient(
                            listOf(colors.negative, colors.negative.copy(alpha = 0.8f)),
                        ),
                    ),
            )
        }
        Icon(
            imageVector = Icons.Filled.Mic,
            contentDescription = null,
            tint = Color.White,
            modifier = Modifier
                .size(size * 0.37f)
                .alpha(1f - cancelAlpha)
                .scale(if (isCancelling) 0.6f else 1f),
        )
        Icon(
            imageVector = Icons.Filled.Close,
            contentDescription = null,
            tint = Color.White,
            modifier = Modifier
                .size(size * 0.37f)
                .alpha(cancelAlpha)
                .scale(if (isCancelling) 1f else 0.6f),
        )
    }
}

@Composable
private fun TimerLabel(
    elapsedMs: Long,
    reduceMotion: Boolean,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    val elapsedSeconds = (elapsedMs / 1_000L).coerceAtMost(RECORD_LIMIT_SECONDS.toLong()).toInt()
    val remaining = (RECORD_LIMIT_SECONDS - elapsedSeconds).coerceAtLeast(0)
    val blinkOn = (elapsedMs / 500L) % 2L == 0L
    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .alpha(if (reduceMotion || blinkOn) 1f else 0.25f)
                .clip(CircleShape)
                .background(colors.negative),
        )
        Spacer(modifier = Modifier.width(8.dp))
        Text(
            text = "${elapsedSeconds / 60}:${(elapsedSeconds % 60).toString().padStart(2, '0')}",
            color = Color.White,
            fontSize = 18.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = FontFamily.Monospace,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
        if (remaining <= 15) {
            Spacer(modifier = Modifier.width(8.dp))
            Text(
                text = stringResource(R.string.rec_remaining, remaining),
                color = colors.negativeText,
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold,
                fontFamily = FontFamily.Monospace,
                style = TextStyle(fontFeatureSettings = "tnum"),
            )
        }
    }
}

@Composable
private fun StatusTexts(
    status: RecordingStatus,
    reduceMotion: Boolean,
    modifier: Modifier = Modifier,
) {
    Crossfade(
        targetState = status,
        modifier = modifier,
        animationSpec = if (reduceMotion) snap() else tween(durationMillis = 180),
        label = "recordingStatus",
    ) { target ->
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = stringResource(target.titleRes),
                color = if (target == RecordingStatus.Cancelling) {
                    Splitty.colors.negative
                } else {
                    Color.White
                },
                fontSize = 20.sp,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(target.subtitleRes),
                color = Color.White.copy(alpha = 0.75f),
                fontSize = 13.sp,
                textAlign = TextAlign.Center,
            )
        }
    }
}

/**
 * Транскрипт-«телесуфлёр» (караоке): окно ~3 строки, текст прижат к низу —
 * свежие слова всегда видны, старые уезжают вверх и растворяются в градиентной
 * маске. Порт iOS `transcriptWindow`. Показывается только когда транскрайбер
 * включён (флаг KARAOKE_TRANSCRIPT и живой движок) — иначе окна нет вовсе.
 */
@Composable
private fun KaraokeWindow(
    transcript: String,
    reduceMotion: Boolean,
    modifier: Modifier = Modifier,
) {
    // Маска «растворения» сверху рисуется поверх содержимого в DstIn: обычный
    // градиент-фон закрасил бы затемнение оверлея прямоугольником.
    val fade = remember {
        Brush.verticalGradient(
            0f to Color.Transparent,
            0.4f to Color.Black,
            1f to Color.Black,
        )
    }
    Box(
        modifier = modifier
            .widthIn(max = 320.dp)
            .height(128.dp)
            .clip(RoundedCornerShape(4.dp))
            .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
            .drawWithContent {
                drawContent()
                drawRect(brush = fade, blendMode = BlendMode.DstIn)
            },
        contentAlignment = Alignment.BottomCenter,
    ) {
        Crossfade(
            targetState = transcript,
            animationSpec = if (reduceMotion) snap() else tween(durationMillis = 180),
            label = "karaokeTranscript",
        ) { text ->
            if (text.isEmpty()) {
                Text(
                    text = stringResource(R.string.rec_karaoke_listening),
                    color = Color.White.copy(alpha = 0.45f),
                    fontSize = 19.sp,
                    fontWeight = FontWeight.SemiBold,
                    textAlign = TextAlign.Center,
                )
            } else {
                Text(
                    text = text,
                    color = Color.White,
                    fontSize = 21.sp,
                    fontWeight = FontWeight.SemiBold,
                    lineHeight = 27.sp,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}

@Composable
private fun HintsCard(hints: List<String>, modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Column(
        modifier = modifier
            .widthIn(max = 320.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(Color.White.copy(alpha = 0.1f))
            .border(1.dp, Color.White.copy(alpha = 0.14f), RoundedCornerShape(16.dp))
            .padding(horizontal = 16.dp, vertical = 13.dp),
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        Text(
            text = stringResource(R.string.rec_hints_title).uppercase(),
            color = colors.accentText,
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.2.sp,
        )
        hints.forEach { hint ->
            Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(
                    modifier = Modifier
                        .padding(top = 7.dp)
                        .size(5.dp)
                        .clip(CircleShape)
                        .background(colors.accent),
                )
                Text(
                    text = hint,
                    color = Color.White.copy(alpha = 0.92f),
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                )
            }
        }
    }
}

@Composable
private fun Waveform(
    active: Boolean,
    level: Float,
    timeSeconds: Double,
    modifier: Modifier = Modifier,
) {
    Canvas(modifier = modifier) {
        val bars = 26
        val barWidth = 4.dp.toPx()
        val gap = 4.dp.toPx()
        val totalWidth = bars * barWidth + (bars - 1) * gap
        val startX = (size.width - totalWidth) / 2f
        val centerY = size.height / 2f
        repeat(bars) { i ->
            val h = if (active) {
                val wave = abs(sin(timeSeconds * 6.0 + i * 0.55))
                val env = abs(sin(i * 1.9))
                val amp = 0.18 + level.coerceIn(0f, 1f) * 1.1
                (6.dp.toPx() + (wave * (14.dp.toPx() + env * 24.dp.toPx()) * amp)).toFloat()
            } else {
                // Reduce motion и отмена держат «осциллограф» плоским: сигнал
                // состояния остаётся, но экран не шевелится сам по себе.
                8.dp.toPx()
            }
            val x = startX + i * (barWidth + gap)
            drawRoundRect(
                color = Color.White.copy(alpha = 0.9f),
                topLeft = Offset(x, centerY - h / 2f),
                size = Size(barWidth, h),
                cornerRadius = CornerRadius(barWidth / 2f, barWidth / 2f),
            )
        }
    }
}

@Composable
private fun rememberRecordingSpinDegrees(enabled: Boolean, reduceMotion: Boolean): Float {
    if (reduceMotion || !enabled) return 0f
    val transition = rememberInfiniteTransition(label = "recordingPreparingSpin")
    val degrees by transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 900, easing = LinearEasing),
            repeatMode = RepeatMode.Restart,
        ),
        label = "recordingPreparingSpinDegrees",
    )
    return degrees
}

@Composable
private fun rememberRecordingPulsePhase(enabled: Boolean, reduceMotion: Boolean): Float {
    if (reduceMotion || !enabled) return 0f
    val transition = rememberInfiniteTransition(label = "recordingPulse")
    val phase by transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 1_400, easing = LinearEasing),
            repeatMode = RepeatMode.Restart,
        ),
        label = "recordingPulsePhase",
    )
    return phase
}

private enum class RecordingStatus(
    val titleRes: Int,
    val subtitleRes: Int,
) {
    Cancelling(R.string.rec_status_cancel_title, R.string.rec_status_cancel_sub),
    Locked(R.string.rec_status_locked_title, R.string.rec_status_locked_sub),
    Preparing(R.string.rec_status_preparing_title, R.string.rec_status_preparing_sub),
    Recording(R.string.rec_status_recording_title, R.string.rec_status_recording_sub),
}

/**
 * Фон оверлея записи. На iOS это тёмный `ultraThinMaterial` + `Color.black.opacity(0.35)`,
 * что визуально читается СЕРЫМ, а не чёрным. Здесь берём сплошной серый (без
 * прозрачности — блюра под оверлеем всё равно нет): раньше стоял `bg` тёмной
 * темы `#0C0F13`, самый тёмный токен палитры, и экран выглядел чёрным.
 */
private val RecordingScrim = Color(0xFF2B3038)
