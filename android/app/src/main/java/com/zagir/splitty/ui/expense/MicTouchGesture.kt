package com.zagir.splitty.ui.expense

import android.content.Context
import android.os.SystemClock
import android.util.Log
import android.view.accessibility.AccessibilityManager
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import com.zagir.splitty.ui.components.Haptics
import kotlin.math.abs
import kotlin.math.max

// --- Пороги и лимиты жеста hold-to-talk (порт iOS AddExpenseView) ---

/**
 * Порог свайпа вверх для «замка» записи (палец можно отпустить). Единицы — **dp**:
 * iOS-овские -70 это points, а не пиксели, поэтому смещение пальца приводится к dp
 * в [micTouchGesture]. Если сравнивать сырые пиксели, на 3x-экране замок защёлкнется
 * уже через ~23 dp хода — то есть почти от любого движения.
 */
const val MIC_LOCK_THRESHOLD_DP = -70f

/** Порог свайпа влево для отмены записи. **dp**, как points в iOS (-70). */
const val MIC_CANCEL_THRESHOLD_DP = -70f

/** Лимит записи и автостоп — минута (как iOS). */
const val RECORD_LIMIT_SECONDS = 60

/**
 * Минимальный размер WAV, ниже которого считаем это случайным коротким тапом
 * (~0.7 с при 16 кГц/16 бит/mono = 32 КБ/с). Порт iOS `data.count < 24_000` —
 * вместо ошибки показываем обучающий тост «удерживайте».
 */
const val SHORT_TAP_MIN_BYTES = 24_000

/**
 * Срабатывание «замка»: свайп вверх за порог, причём вертикаль доминирует над
 * горизонталью (иначе диагональ влево-вверх ошибочно замкнула бы вместо отмены).
 * Порт iOS `translation.height < -70 && abs(height) >= abs(width)`.
 * [dx]/[dy] — смещение в **dp**.
 */
fun micLockTriggered(dx: Float, dy: Float): Boolean =
    dy < MIC_LOCK_THRESHOLD_DP && abs(dy) >= abs(dx)

/**
 * Срабатывание отмены: свайп влево за порог, горизонталь доминирует над
 * вертикалью. Порт iOS `translation.width < -70 && abs(width) > abs(height)`.
 * [dx]/[dy] — смещение в **dp**.
 */
fun micCancelTriggered(dx: Float, dy: Float): Boolean =
    dx < MIC_CANCEL_THRESHOLD_DP && abs(dx) > abs(dy)

/** Прогресс «замка» 0…1 (для анимации иконки-замка), [dy] в dp. Порт iOS `lockProgress`. */
fun micLockProgress(dy: Float): Float = (-dy / -MIC_LOCK_THRESHOLD_DP).coerceIn(0f, 1f)

/** Прогресс отмены 0…1 (для анимации ✕-зоны), [dx] в dp. Порт iOS `cancelProgress`. */
fun micCancelProgress(dx: Float): Float = (-dx / -MIC_CANCEL_THRESHOLD_DP).coerceIn(0f, 1f)

/**
 * Перевод смещения пальца из пикселей в dp. Вынесено из жеста отдельной функцией
 * ради теста: сравнение сырых пикселей с dp-порогами — тихая регрессия, которая
 * никак не проявляется на 1x-эмуляторе и ломает жест на реальном 3x-телефоне.
 */
fun micDragPxToDp(px: Float, density: Float): Float = px / density

/** true — запись слишком короткая (случайный тап): обучающий тост вместо отправки. */
fun isShortTap(wavBytes: Int): Boolean = wavBytes < SHORT_TAP_MIN_BYTES

/** true — достигнут лимит записи (автостоп). */
fun recordTimedOut(elapsedSeconds: Int): Boolean = elapsedSeconds >= RECORD_LIMIT_SECONDS

/**
 * Длительность записи в секундах по размеру WAV: 16 кГц/16 бит/mono ≈ 32 КБ/с.
 * Минимум 1 — «0 сек» на экране «Записано» читалось бы как сбой (порт iOS
 * `recordedSeconds`).
 */
fun recordedSeconds(wavBytes: Long): Int = max(1L, wavBytes / 32_000L).toInt()

/**
 * Состояние-машина hold-to-talk поверх [AudioRecorderController] (порт iOS
 * обработчиков onBegan/onMoved/onEnded). Держит транзиентные флаги жеста
 * (нажат/замкнут/отменяется + смещение пальца) и рулит записью. Чистые пороги
 * вынесены в top-level функции (см. выше) и покрыты юнит-тестами; сама машина —
 * UI-склейка, тестируется на устройстве.
 *
 * Колбэки задаёт экран: [onFinish] — запись завершена и годна (WAV), [onShortTap] —
 * слишком короткий тап, [onCancelled] — отменена/пуста, [onError] — не поднялся
 * микрофон.
 */
/**
 * Минимум записи, нужный машине состояний [VoiceController]. Отдельный интерфейс
 * (а не сам [AudioRecorderController]) — чтобы ветвления жеста (отмена, короткий
 * тап, автостоп, CANCEL от системы) проверялись юнит-тестами на JVM: живой
 * AudioRecord требует железа.
 */
interface VoiceRecorder {
    val isRecording: Boolean

    /** Путь к WAV последней записи в cacheDir; null — записи не было. */
    val lastAudioPath: String?

    fun start()

    /** Стоп: WAV записанного или null, если писать было нечего. */
    fun stop(): ByteArray?

    fun cancel()

    fun reset()
}

@Stable
class VoiceController(
    private val recorder: VoiceRecorder,
    private val haptics: Haptics,
    private val talkBack: Boolean,
) {
    /** Палец на микрофоне (обычный hold; в locked — false). */
    var isMicPressed by mutableStateOf(false)
        private set

    /** Запись закреплена свайпом вверх — палец можно отпустить. */
    var isLocked by mutableStateOf(false)
        private set

    /** Палец ушёл в зону отмены (свайп влево) — отпускание отменит запись. */
    var isCancelling by mutableStateOf(false)
        private set

    var dragX by mutableFloatStateOf(0f)
        private set

    var dragY by mutableFloatStateOf(0f)
        private set

    /** Оверлей активен (жест идёт или запись пишется). */
    val isActive: Boolean get() = isMicPressed || recorder.isRecording || isLocked

    /** Идёт «подготовка» — палец нажат, но движок ещё не поднялся. */
    val isPreparing: Boolean get() = !recorder.isRecording

    /** Запись завершена и годна: путь к WAV в cacheDir (переживает process death). */
    var onFinish: ((audioPath: String) -> Unit)? = null
    var onShortTap: (() -> Unit)? = null
    var onCancelled: (() -> Unit)? = null
    var onError: ((String) -> Unit)? = null

    /** Касание микрофона: мгновенный хептик и старт записи (порт iOS `onBegan`). */
    fun began(downUptimeMs: Long) {
        Log.d(TAG, "mic touch latency=${SystemClock.uptimeMillis() - downUptimeMs}ms")
        if (talkBack) return // TalkBack — режим тапа, обрабатывается [toggleTalkBack]
        if (isLocked || isMicPressed) return
        isMicPressed = true
        haptics.tap() // хептик #1 — отклик касания ДО подъёма движка
        startRecording()
    }

    /** Движение пальца: замок вверх / отмена влево (порт iOS `onMoved`). */
    fun moved(dx: Float, dy: Float) {
        if (isLocked || !isMicPressed) return
        dragX = dx
        dragY = dy
        if (micLockTriggered(dx, dy)) {
            isLocked = true
            isMicPressed = false
            isCancelling = false
            dragX = 0f
            dragY = 0f
            haptics.success() // хептик защёлкивания — сразу на пороге, не по отпусканию
            return
        }
        val cancel = micCancelTriggered(dx, dy)
        if (cancel != isCancelling) {
            isCancelling = cancel
            haptics.tap()
        }
    }

    /** Отпускание пальца: отмена (если в зоне) либо стоп+распознавание (порт iOS `onEnded`). */
    fun ended(dx: Float, dy: Float) {
        if (isLocked) return // после замка отпускание игнорируется
        isMicPressed = false
        val cancel = micCancelTriggered(dx, dy)
        isCancelling = false
        dragX = 0f
        dragY = 0f
        if (cancel) cancelRecording() else finishRecording()
    }

    /** Система отобрала касание (звонок/сворачивание) — молча отменяем (порт iOS `onCancelled`). */
    fun systemCancelled() {
        if (isLocked) return
        isMicPressed = false
        isCancelling = false
        dragX = 0f
        dragY = 0f
        cancelRecording()
    }

    /** TalkBack: одиночный тап переключает locked-запись (hold недоступен под screen reader). */
    fun toggleTalkBack() {
        if (recorder.isRecording || isLocked) {
            isLocked = false
            finishRecording()
        } else {
            isLocked = true
            haptics.tap()
            startRecording()
        }
    }

    /** Кнопка «Готово» в locked-оверлее. */
    fun stopLocked() {
        isLocked = false
        finishRecording()
    }

    /** Кнопка «Отмена»/predictive-back в locked-оверлее. */
    fun cancelLocked() {
        isLocked = false
        isCancelling = false
        cancelRecording()
    }

    /** Автостоп по лимиту записи (60 с) — стоп+распознавание независимо от режима. */
    fun autoStop() {
        if (!recorder.isRecording && !isMicPressed) return
        isLocked = false
        isMicPressed = false
        isCancelling = false
        finishRecording()
    }

    private fun startRecording() {
        try {
            recorder.start()
        } catch (e: AudioRecorderException) {
            isMicPressed = false
            isLocked = false
            onError?.invoke(e.message ?: "Не удалось начать запись")
        }
    }

    private fun finishRecording() {
        val data = recorder.stop()
        if (data == null) {
            onCancelled?.invoke()
            return
        }
        // Тап вместо удержания (~0.7 с) — «запись» пуста: не гоним в Gemini, а
        // подсказываем жест обучающим тостом (модальный алерт тут пугал).
        if (isShortTap(data.size)) {
            recorder.reset()
            onShortTap?.invoke()
            return
        }
        val path = recorder.lastAudioPath
        if (path == null) {
            onCancelled?.invoke()
            return
        }
        onFinish?.invoke(path)
    }

    private fun cancelRecording() {
        recorder.cancel()
        recorder.reset()
        onCancelled?.invoke()
    }

    private companion object {
        const val TAG = "MicHold"
    }
}

/**
 * Жест hold-to-talk на прозрачной поверхности микрофона. Использует «сырой» цикл
 * pointerInput (не detectDragGestures) с перехватом в [PointerEventPass.Initial],
 * чтобы касание доходило без арбитража — минимальная латентность (аналог
 * UIKit `beginTracking` в iOS, где SwiftUI-жест опаздывал на 100–300 мс). Палец
 * может уходить за границы кнопки (замок/отмена) — цикл слушает до отпускания.
 *
 * Под TalkBack ([talkBack] = true) hold недоступен — жест вырождается в одиночный
 * тап ([onTap]), который переключает locked-запись.
 */
fun Modifier.micTouchGesture(
    enabled: Boolean,
    talkBack: Boolean,
    onTap: () -> Unit,
    onBegan: (downUptimeMs: Long) -> Unit,
    onMoved: (dx: Float, dy: Float) -> Unit,
    onEnded: (dx: Float, dy: Float) -> Unit,
    onSystemCancelled: () -> Unit,
): Modifier = pointerInput(enabled, talkBack) {
    if (!enabled) return@pointerInput
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        if (talkBack) {
            // Под screen reader — простой тап-toggle (waitForUpOrCancellation).
            val up = waitForUpOrCancellation()
            if (up != null) onTap()
            return@awaitEachGesture
        }
        val startPos = down.position
        onBegan(down.uptimeMillis)
        while (true) {
            val event = awaitPointerEvent(PointerEventPass.Main)
            // Касание отобрали (родительский скролл, системный жест, звонок):
            // это НЕ отпускание — записанное отбрасываем молча, без распознавания.
            if (event.changes.any { it.isConsumed }) {
                onSystemCancelled()
                break
            }
            val change = event.changes.firstOrNull { it.id == down.id } ?: event.changes.first()
            // Смещение приводим к dp: пороги замка/отмены — это iOS-овские points,
            // и на сыром пикселе они бы масштабировались вместе с плотностью экрана.
            val offset = change.position - startPos
            val dx = micDragPxToDp(offset.x, density)
            val dy = micDragPxToDp(offset.y, density)
            if (change.pressed) {
                onMoved(dx, dy)
                change.consume()
            } else {
                onEnded(dx, dy)
                change.consume()
                break
            }
        }
    }
}

/**
 * Включено ли исследование касанием (TalkBack): под screen reader удержание
 * недоступно — жест вырождается в тап-toggle. Слушаем изменения на лету:
 * пользователь может включить TalkBack, не покидая экран.
 */
@Composable
fun rememberTouchExploration(): Boolean {
    val context = LocalContext.current
    val manager = remember(context) {
        context.getSystemService(Context.ACCESSIBILITY_SERVICE) as AccessibilityManager
    }
    var enabled by remember { mutableStateOf(manager.isTouchExplorationEnabled) }
    DisposableEffect(manager) {
        val listener = AccessibilityManager.TouchExplorationStateChangeListener { enabled = it }
        manager.addTouchExplorationStateChangeListener(listener)
        onDispose { manager.removeTouchExplorationStateChangeListener(listener) }
    }
    return enabled
}
