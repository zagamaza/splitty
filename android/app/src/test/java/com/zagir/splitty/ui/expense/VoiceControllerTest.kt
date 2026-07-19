package com.zagir.splitty.ui.expense

import com.zagir.splitty.ui.components.Haptics
import kotlin.test.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Машина состояний hold-to-talk (Task 12): что происходит с записью при
 * отпускании, свайпах, замке, автостопе и отобранном системой касании. Живой
 * микрофон подменён [FakeRecorder] — проверяем именно ветвления, а не железо.
 * Robolectric нужен только ради android.util.Log/SystemClock в замере латентности.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class VoiceControllerTest {

    /** Запись-заглушка: считает вызовы и отдаёт заданный размер WAV. */
    private class FakeRecorder(private var wavSize: Int = 96_000) : VoiceRecorder {
        override var isRecording = false
        override var lastAudioPath: String? = null
        var startCount = 0
        var cancelCount = 0
        var resetCount = 0
        var failOnStart = false

        override fun start() {
            if (failOnStart) throw AudioRecorderException("нет микрофона")
            startCount++
            isRecording = true
            lastAudioPath = "/cache/rec.wav"
        }

        override fun stop(): ByteArray? {
            if (!isRecording) return null
            isRecording = false
            return ByteArray(wavSize)
        }

        override fun cancel() {
            cancelCount++
            isRecording = false
        }

        override fun reset() {
            resetCount++
            lastAudioPath = null
        }

        fun withWavSize(size: Int) = apply { wavSize = size }
    }

    /** Хептики без Android: считаем вызовы, чтобы проверить контракт откликов. */
    private class FakeHaptics : Haptics {
        var taps = 0
        var successes = 0
        var warnings = 0
        override fun tap() { taps++ }
        override fun success() { successes++ }
        override fun warning() { warnings++ }
    }

    private class Probe {
        var finishedPath: String? = null
        var shortTaps = 0
        var cancels = 0
        var error: String? = null
    }

    private fun controller(
        recorder: FakeRecorder = FakeRecorder(),
        haptics: FakeHaptics = FakeHaptics(),
        talkBack: Boolean = false,
        probe: Probe = Probe(),
    ): Triple<VoiceController, Probe, FakeHaptics> {
        val vc = VoiceController(recorder = recorder, haptics = haptics, talkBack = talkBack)
        vc.onFinish = { probe.finishedPath = it }
        vc.onShortTap = { probe.shortTaps++ }
        vc.onCancelled = { probe.cancels++ }
        vc.onError = { probe.error = it }
        return Triple(vc, probe, haptics)
    }

    // --- Обычное удержание ---

    @Test
    fun `hold and release finishes with the recorded wav path`() {
        val recorder = FakeRecorder()
        val (vc, probe, haptics) = controller(recorder)

        vc.began(downUptimeMs = 0L)
        assertTrue(vc.isMicPressed)
        assertEquals(1, recorder.startCount)
        assertEquals(1, haptics.taps) // хептик касания — до подъёма движка

        vc.ended(dx = 0f, dy = 0f)
        assertFalse(vc.isMicPressed)
        assertEquals("/cache/rec.wav", probe.finishedPath)
        assertEquals(0, probe.cancels)
    }

    // Слишком короткое удержание — обучающий тост вместо отправки в модель.
    @Test
    fun `short tap does not send anything and hints the gesture`() {
        val recorder = FakeRecorder().withWavSize(SHORT_TAP_MIN_BYTES - 1)
        val (vc, probe, _) = controller(recorder)

        vc.began(0L)
        vc.ended(dx = 0f, dy = 0f)

        assertEquals(1, probe.shortTaps)
        assertNull(probe.finishedPath)
        assertEquals(1, recorder.resetCount)
    }

    // --- Отмена свайпом влево ---

    @Test
    fun `swipe left marks cancelling and release discards the recording`() {
        val recorder = FakeRecorder()
        val (vc, probe, _) = controller(recorder)

        vc.began(0L)
        vc.moved(dx = -90f, dy = 0f)
        assertTrue(vc.isCancelling)

        vc.ended(dx = -90f, dy = 0f)
        assertFalse(vc.isCancelling)
        assertEquals(1, probe.cancels)
        assertEquals(1, recorder.cancelCount)
        assertNull(probe.finishedPath)
    }

    // Вернули палец из зоны отмены — запись продолжается и уходит в распознавание.
    @Test
    fun `returning from the cancel zone keeps the recording`() {
        val (vc, probe, _) = controller()

        vc.began(0L)
        vc.moved(dx = -90f, dy = 0f)
        vc.moved(dx = -10f, dy = 0f)
        assertFalse(vc.isCancelling)

        vc.ended(dx = -10f, dy = 0f)
        assertEquals("/cache/rec.wav", probe.finishedPath)
        assertEquals(0, probe.cancels)
    }

    // --- Замок ---

    @Test
    fun `swipe up locks and release no longer stops the recording`() {
        val recorder = FakeRecorder()
        val (vc, probe, haptics) = controller(recorder)

        vc.began(0L)
        vc.moved(dx = 0f, dy = -90f)
        assertTrue(vc.isLocked)
        assertFalse(vc.isMicPressed)
        assertEquals(1, haptics.successes) // защёлка — сразу на пороге
        assertEquals(0f, vc.dragY) // смещение сброшено: микрофон вернулся в фрейм

        vc.ended(dx = 0f, dy = -90f)
        assertNull(probe.finishedPath) // отпускание после замка игнорируется
        assertTrue(recorder.isRecording)

        vc.stopLocked()
        assertFalse(vc.isLocked)
        assertEquals("/cache/rec.wav", probe.finishedPath)
    }

    @Test
    fun `cancel button in locked mode discards the recording`() {
        val recorder = FakeRecorder()
        val (vc, probe, _) = controller(recorder)

        vc.began(0L)
        vc.moved(dx = 0f, dy = -90f)
        vc.cancelLocked()

        assertFalse(vc.isLocked)
        assertEquals(1, probe.cancels)
        assertEquals(1, recorder.cancelCount)
        assertNull(probe.finishedPath)
    }

    // --- CANCEL от системы (звонок, родительский скролл отобрал касание) ---

    @Test
    fun `system cancel discards the recording silently`() {
        val recorder = FakeRecorder()
        val (vc, probe, _) = controller(recorder)

        vc.began(0L)
        vc.systemCancelled()

        assertFalse(vc.isMicPressed)
        assertEquals(1, recorder.cancelCount)
        assertEquals(1, probe.cancels)
        assertNull(probe.finishedPath)
    }

    // Закреплённую запись системный CANCEL не трогает: палец там и не нужен.
    @Test
    fun `system cancel does not touch a locked recording`() {
        val recorder = FakeRecorder()
        val (vc, _, _) = controller(recorder)

        vc.began(0L)
        vc.moved(dx = 0f, dy = -90f)
        vc.systemCancelled()

        assertTrue(vc.isLocked)
        assertEquals(0, recorder.cancelCount)
        assertTrue(recorder.isRecording)
    }

    // --- Автостоп по лимиту ---

    @Test
    fun `auto stop finishes the recording in both hold and locked modes`() {
        val (held, heldProbe, _) = controller()
        held.began(0L)
        held.autoStop()
        assertEquals("/cache/rec.wav", heldProbe.finishedPath)
        assertFalse(held.isMicPressed)

        val (locked, lockedProbe, _) = controller()
        locked.began(0L)
        locked.moved(dx = 0f, dy = -90f)
        locked.autoStop()
        assertEquals("/cache/rec.wav", lockedProbe.finishedPath)
        assertFalse(locked.isLocked)
    }

    @Test
    fun `auto stop is a no-op when nothing is being recorded`() {
        val recorder = FakeRecorder()
        val (vc, probe, _) = controller(recorder)

        vc.autoStop()

        assertNull(probe.finishedPath)
        assertEquals(0, probe.cancels)
    }

    // --- TalkBack: удержание недоступно, работает тап-toggle ---

    @Test
    fun `talkback tap toggles a locked recording`() {
        val recorder = FakeRecorder()
        val (vc, probe, _) = controller(recorder, talkBack = true)

        vc.toggleTalkBack()
        assertTrue(vc.isLocked)
        assertEquals(1, recorder.startCount)

        vc.toggleTalkBack()
        assertFalse(vc.isLocked)
        assertEquals("/cache/rec.wav", probe.finishedPath)
    }

    @Test
    fun `hold gesture is inert under talkback`() {
        val recorder = FakeRecorder()
        val (vc, _, _) = controller(recorder, talkBack = true)

        vc.began(0L)

        assertFalse(vc.isMicPressed)
        assertEquals(0, recorder.startCount)
    }

    // --- Микрофон не поднялся ---

    @Test
    fun `start failure surfaces the error and leaves no pressed state`() {
        val recorder = FakeRecorder().apply { failOnStart = true }
        val (vc, probe, _) = controller(recorder)

        vc.began(0L)

        assertEquals("нет микрофона", probe.error)
        assertFalse(vc.isMicPressed)
        assertFalse(vc.isLocked)
    }
}
