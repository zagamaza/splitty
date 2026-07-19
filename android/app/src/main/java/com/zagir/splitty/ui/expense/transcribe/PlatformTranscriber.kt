package com.zagir.splitty.ui.expense.transcribe

import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.zagir.splitty.ui.expense.AUDIO_TARGET_SAMPLE_RATE
import java.io.OutputStream

/**
 * Караоке через системный [SpeechRecognizer], которому скармливаем СВОЙ PCM
 * (`EXTRA_AUDIO_SOURCE`, API 33+) — второй захват микрофона запрещён, а иначе
 * распознаватель открыл бы собственный поток и подрался с [AudioRecord] за канал.
 * Читающий конец пайпа отдаём системе, в пишущий льём те же кадры, что уходят
 * в WAV.
 *
 * Всё, что трогает [SpeechRecognizer], идёт на главном потоке (требование API);
 * запись PCM — на аудио-потоке, поэтому поток вывода закрывается под замком.
 * Живой движок на JVM недоступен — тестируется склейка сегментов
 * ([TranscriptAccumulator]) и работа заглушки.
 */
@RequiresApi(Build.VERSION_CODES.TIRAMISU)
class PlatformTranscriber(private val context: Context) : Transcriber {

    override var transcript by mutableStateOf("")
        private set

    override val isEnabled: Boolean = true

    private val main = Handler(Looper.getMainLooper())
    private val accumulator = TranscriptAccumulator()

    private var recognizer: SpeechRecognizer? = null
    private var readFd: ParcelFileDescriptor? = null

    private val feedLock = Any()
    private var feed: OutputStream? = null

    override fun start() {
        accumulator.reset()
        transcript = ""
        val pipe = try {
            ParcelFileDescriptor.createPipe()
        } catch (e: Exception) {
            Log.w(TAG, "pipe failed: ${e.message}")
            return
        }
        readFd = pipe[0]
        synchronized(feedLock) {
            feed = ParcelFileDescriptor.AutoCloseOutputStream(pipe[1])
        }
        main.post { startRecognizer() }
    }

    private fun startRecognizer() {
        val rec = try {
            if (SpeechRecognizer.isOnDeviceRecognitionAvailable(context)) {
                SpeechRecognizer.createOnDeviceSpeechRecognizer(context)
            } else {
                SpeechRecognizer.createSpeechRecognizer(context)
            }
        } catch (e: Exception) {
            Log.w(TAG, "recognizer unavailable: ${e.message}")
            return
        }
        recognizer = rec
        rec.setRecognitionListener(listener)
        try {
            rec.startListening(recognitionIntent())
        } catch (e: Exception) {
            Log.w(TAG, "startListening failed: ${e.message}")
            releaseRecognizer()
        }
    }

    /**
     * Интент распознавания: источник — наш пайп с PCM16 mono 16 кГц, частичные
     * результаты включены (караоке без них показывало бы текст только в конце),
     * язык — системный (голосовой ввод расхода у нас русский, но локаль решает
     * пользователь).
     */
    private fun recognitionIntent(): Intent =
        Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
            putExtra(
                RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
            )
            putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE, readFd)
            putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_CHANNEL_COUNT, 1)
            putExtra(
                RecognizerIntent.EXTRA_AUDIO_SOURCE_ENCODING,
                android.media.AudioFormat.ENCODING_PCM_16BIT,
            )
            putExtra(
                RecognizerIntent.EXTRA_AUDIO_SOURCE_SAMPLING_RATE,
                AUDIO_TARGET_SAMPLE_RATE,
            )
        }

    override fun feed(pcm: ByteArray, length: Int) {
        synchronized(feedLock) {
            val stream = feed ?: return
            try {
                stream.write(pcm, 0, length)
            } catch (e: Exception) {
                // Система закрыла свой конец (сессия кончилась) — просто перестаём кормить.
                Log.d(TAG, "feed closed: ${e.message}")
                closeFeedLocked()
            }
        }
    }

    override fun stop() {
        synchronized(feedLock) { closeFeedLocked() }
        main.post { releaseRecognizer() }
    }

    override fun reset() {
        accumulator.reset()
        transcript = ""
    }

    private fun closeFeedLocked() {
        try {
            feed?.close()
        } catch (_: Exception) {
            // уже закрыт
        }
        feed = null
    }

    private fun releaseRecognizer() {
        recognizer?.let {
            try {
                it.stopListening()
            } catch (_: Exception) {
                // сессия могла уже завершиться сама
            }
            it.destroy()
        }
        recognizer = null
        try {
            readFd?.close()
        } catch (_: Exception) {
            // система могла закрыть дескриптор раньше
        }
        readFd = null
    }

    private val listener = object : RecognitionListener {
        override fun onPartialResults(partialResults: Bundle?) {
            firstText(partialResults)?.let { transcript = accumulator.partial(it) }
        }

        override fun onResults(results: Bundle?) {
            // Сегмент закрыт (пауза в речи): дописываем его и ждём следующий —
            // движок сам продолжает сессию, пока в пайп идёт PCM.
            firstText(results)?.let { transcript = accumulator.finalize(it) }
        }

        override fun onError(error: Int) {
            // Караоке best-effort: молчание/таймаут не должны ломать саму запись.
            Log.d(TAG, "recognition error=$error")
        }

        override fun onReadyForSpeech(params: Bundle?) = Unit
        override fun onBeginningOfSpeech() = Unit
        override fun onRmsChanged(rmsdB: Float) = Unit
        override fun onBufferReceived(buffer: ByteArray?) = Unit
        override fun onEndOfSpeech() = Unit
        override fun onEvent(eventType: Int, params: Bundle?) = Unit

        private fun firstText(bundle: Bundle?): String? = bundle
            ?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
            ?.firstOrNull()
            ?.takeIf { it.isNotBlank() }
    }

    companion object {
        private const val TAG = "Karaoke"

        /** Есть ли на устройстве распознавание вообще (иначе спускаемся по лестнице). */
        fun isSupported(context: Context): Boolean = try {
            SpeechRecognizer.isRecognitionAvailable(context) ||
                SpeechRecognizer.isOnDeviceRecognitionAvailable(context)
        } catch (_: Exception) {
            false
        }
    }
}
