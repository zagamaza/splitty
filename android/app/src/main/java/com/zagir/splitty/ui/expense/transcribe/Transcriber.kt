package com.zagir.splitty.ui.expense.transcribe

import android.content.Context
import android.os.Build
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import com.zagir.splitty.BuildConfig

/**
 * Живой транскрипт надиктовки («караоке» в оверлее записи) — порт iOS
 * `AudioRecorder.transcript`. Best-effort: parse на сервере работает от самого
 * аудио, транскрипт нужен только чтобы пользователь ВИДЕЛ, что его слышат.
 *
 * Кормится тем же PCM16 16 кГц mono, что уходит в WAV, — второй захват микрофона
 * запрещён (см. AudioRecorderController). Реализации выбираются «лестницей»
 * в [rememberTranscriber]; при [isEnabled] == false оверлей рисуется без караоке.
 */
interface Transcriber {
    /** Текущий текст (финализированные сегменты + живой хвост). Compose-состояние. */
    val transcript: String

    /** false — караоке недоступно (флаг выключен/нет движка): окно не показываем. */
    val isEnabled: Boolean

    fun start()

    /** Кадр PCM16 LE mono 16 кГц из аудио-потока записи. Не блокирует надолго. */
    fun feed(pcm: ByteArray, length: Int = pcm.size)

    fun stop()

    fun reset()
}

/** Заглушка: флаг выключен или движка нет — оверлей без караоке-окна. */
object NoopTranscriber : Transcriber {
    override val transcript: String = ""
    override val isEnabled: Boolean = false
    override fun start() = Unit
    override fun feed(pcm: ByteArray, length: Int) = Unit
    override fun stop() = Unit
    override fun reset() = Unit
}

/**
 * Склейка сегментов распознавания: движок финализирует кусок после паузы в речи
 * и начинает следующий с нуля. Держим «хвост» отдельно от финализированного —
 * иначе живой партиал затирал бы уже сказанное (порт iOS `finalizedTranscript`
 * + `joinSegments`). Чистая логика: покрыта unit-тестами.
 */
class TranscriptAccumulator {
    private var finalized: String = ""

    /** Живой (ещё не финальный) кусок поверх накопленного — то, что видно на экране. */
    fun partial(segment: String): String = joinSegments(finalized, segment)

    /** Сегмент закрыт: дописываем его в накопленное и возвращаем полный текст. */
    fun finalize(segment: String): String {
        finalized = joinSegments(finalized, segment)
        return finalized
    }

    fun reset() {
        finalized = ""
    }

    companion object {
        /** Пробел только между непустыми — иначе текст начинался бы с пробела. */
        fun joinSegments(head: String, tail: String): String = when {
            head.isEmpty() -> tail
            tail.isEmpty() -> head
            else -> "$head $tail"
        }
    }
}

/**
 * Выбор реализации по «лестнице» (решение из плана): платформенный
 * SpeechRecognizer с нашим PCM (API 33+, on-device) → Vosk с моделью,
 * докачанной на устройство → без караоке. Всё под флагом
 * `BuildConfig.KARAOKE_TRANSCRIPT`: выключенный флаг = [NoopTranscriber],
 * лишние движки даже не поднимаются.
 */
fun createTranscriber(
    context: Context,
    enabled: Boolean = BuildConfig.KARAOKE_TRANSCRIPT,
    sdkInt: Int = Build.VERSION.SDK_INT,
): Transcriber {
    if (!enabled) return NoopTranscriber
    @Suppress("NewApi") // ветка отгорожена sdkInt; параметр — ради тестов на JVM
    if (sdkInt >= Build.VERSION_CODES.TIRAMISU && PlatformTranscriber.isSupported(context)) {
        return PlatformTranscriber(context)
    }
    val vosk = VoskTranscriber(context)
    return if (vosk.isEnabled) vosk else NoopTranscriber
}

/**
 * Транскрайбер, привязанный к жизни экрана: на выходе из композиции [Transcriber.stop]
 * гарантирует, что движок распознавания и пайп PCM закрыты (как lifecycle-cancel
 * у рекордера — иначе сессия распознавания пережила бы экран).
 */
@Composable
fun rememberTranscriber(): Transcriber {
    val context = LocalContext.current
    val transcriber = remember(context) { createTranscriber(context.applicationContext) }
    DisposableEffect(transcriber) {
        onDispose {
            transcriber.stop()
            transcriber.reset()
        }
    }
    return transcriber
}
