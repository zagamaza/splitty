package com.zagir.splitty.ui.expense

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import com.github.takahirom.roborazzi.RobolectricDeviceQualifiers
import com.github.takahirom.roborazzi.captureRoboImage
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Roborazzi-снапшоты оверлея записи (Task 12): обычная запись, «Осталось
 * уточнить», замок, свайп влево (отмена), подготовка. Уровень и момент старта
 * фиксированы (живой рекордер не участвует) — иначе снимок дрожал бы от кадра
 * к кадру. Эталон — docs/prototype/splitty-ai-proto.html и iOS RecordingOverlay.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class RecordingOverlaySnapshotTest {

    @get:Rule
    val composeRule = createComposeRule()

    // Фрейм кнопки-микрофона в нижней панели: оверлей рисует свой микрофон
    // ровно тут (у Pixel 5 — 1080×2340 px, микрофон 82 dp ≈ 225 px).
    private val micFrame = Rect(left = 427f, top = 1930f, right = 652f, bottom = 2155f)

    // startedAt = 0 при фиктивном «сейчас» Robolectric даёт стабильный таймер.
    private fun snapshot(
        name: String,
        isCancelling: Boolean = false,
        isLocked: Boolean = false,
        isPreparing: Boolean = false,
        dragX: Float = 0f,
        dragY: Float = 0f,
        level: Float = 0.55f,
        startedAtElapsedMs: Long? = 0L,
        hints: List<String> = emptyList(),
        transcript: String? = null,
    ) {
        // Оверлей держит собственный цикл кадров (таймер, волна, пульс). С
        // автопрокруткой часов композиция никогда не «успокоится» — фиксируем время.
        composeRule.mainClock.autoAdvance = false
        composeRule.setContent {
            SplittyTheme(darkTheme = false) {
                Box(modifier = Modifier.fillMaxSize().background(Splitty.colors.bg)) {
                    RecordingOverlay(
                        isActive = true,
                        isCancelling = isCancelling,
                        isLocked = isLocked,
                        isPreparing = isPreparing,
                        dragX = dragX,
                        dragY = dragY,
                        level = level,
                        startedAtElapsedMs = startedAtElapsedMs,
                        micFrame = micFrame,
                        hints = hints,
                        transcript = transcript,
                        // Часы Compose (mainClock) и SystemClock Robolectric — разные:
                        // без пина фаза волны зависела от реального времени до захвата,
                        // и снапшоты расходились от прогона к прогону.
                        frozenNowMs = FROZEN_NOW_MS,
                        onStop = {},
                        onCancel = {},
                    )
                }
            }
        }
        // Доводим появление до конца (fade/pop ~200 мс), но не отпускаем часы:
        // иначе снимок ловил бы середину анимации, а бесконечный цикл кадров
        // оверлея не дал бы композиции успокоиться.
        composeRule.mainClock.advanceTimeBy(600)
        composeRule.onRoot().captureRoboImage("src/test/snapshots/$name.png")
    }

    // Обычная запись: волна, таймер, зона замка сверху и ✕ слева.
    @Test fun recordingPlain() = snapshot("recording_overlay_plain_light")

    // «Осталось уточнить»: подсказки поверх волны — что доспросить голосом.
    @Test fun recordingWithHints() = snapshot(
        name = "recording_overlay_hints_light",
        hints = listOf("Кто платил?", "Сколько стоила пицца?"),
    )

    // Палец ушёл влево за порог: красный микрофон, ✕-зона гасится.
    @Test fun recordingCancelling() = snapshot(
        name = "recording_overlay_cancel_light",
        isCancelling = true,
        dragX = -90f,
    )

    // Закреплено: вместо микрофона — «Стоп», сверху подпись про завершение.
    @Test fun recordingLocked() = snapshot(
        name = "recording_overlay_locked_light",
        isLocked = true,
    )

    // Караоке-транскрипт (Task 13, за флагом): окно-«телесуфлёр» над волной.
    @Test fun recordingKaraoke() = snapshot(
        name = "recording_overlay_karaoke_light",
        transcript = "пицца за 800 и кола за 200 пополам с Саней",
    )

    // Караоке включено, но человек ещё молчит: плейсхолдер «Слушаю…».
    @Test fun recordingKaraokeListening() = snapshot(
        name = "recording_overlay_karaoke_empty_light",
        transcript = "",
    )

    // Палец нажат, движок ещё не поднялся: спиннер-дуга, таймера нет.
    @Test fun recordingPreparing() = snapshot(
        name = "recording_overlay_preparing_light",
        isPreparing = true,
        startedAtElapsedMs = null,
        level = 0f,
    )

    private companion object {
        /** Пин «сейчас»: вместе с startedAt = 0 даёт таймер 0:00 и стабильную волну. */
        const val FROZEN_NOW_MS = 0L
    }
}
