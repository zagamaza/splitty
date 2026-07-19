package com.zagir.splitty.ui.expense.transcribe

import android.content.Context
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.zagir.splitty.ui.expense.AUDIO_TARGET_SAMPLE_RATE
import org.json.JSONObject
import java.io.File

/**
 * Fallback-ступень караоке для устройств без платформенного распознавания
 * (API < 33 или пустой SpeechRecognizer): офлайн-модель Vosk ru-small.
 *
 * Модель НЕ лежит в APK (≈45 МБ на 15 МБ приложения — неприемлемо): она
 * докачивается на устройство и распаковывается в [modelDir]. Пока каталога нет,
 * ступень выключена ([isEnabled] == false) и оверлей рисуется без караоке —
 * это штатное состояние, а не ошибка.
 *
 * Сама библиотека тоже не в зависимостях: обращение к движку идёт через
 * [VoskEngine] с реализацией на рефлексии — модуль подключается вместе с
 * докачкой, а сборка и unit-тесты остаются без нативной зависимости
 * (профилирование RAM: ru-small держит ~50 МБ на распознаватель, поэтому
 * распознаватель живёт только на время записи и закрывается на [stop]).
 */
class VoskTranscriber(
    context: Context,
    private val engine: VoskEngine = ReflectiveVoskEngine,
) : Transcriber {

    /** Каталог распакованной модели — заполняется докачкой, не сборкой. */
    private val modelDir: File = File(context.filesDir, MODEL_DIR_NAME)

    override var transcript by mutableStateOf("")
        private set

    override val isEnabled: Boolean = engine.isAvailable && isModelReady(modelDir)

    private val accumulator = TranscriptAccumulator()
    private val lock = Any()
    private var session: VoskSession? = null

    override fun start() {
        if (!isEnabled) return
        accumulator.reset()
        transcript = ""
        synchronized(lock) {
            session = try {
                engine.open(modelDir.absolutePath, AUDIO_TARGET_SAMPLE_RATE.toFloat())
            } catch (e: Throwable) {
                Log.w(TAG, "vosk open failed: ${e.message}")
                null
            }
        }
    }

    override fun feed(pcm: ByteArray, length: Int) {
        val active = synchronized(lock) { session } ?: return
        try {
            // true — фраза закрыта паузой: её текст уходит в накопленное,
            // дальше движок начинает следующую с нуля.
            if (active.accept(pcm, length)) {
                transcript = accumulator.finalize(textOf(active.result()))
            } else {
                transcript = accumulator.partial(textOf(active.partialResult()))
            }
        } catch (e: Throwable) {
            Log.w(TAG, "vosk feed failed: ${e.message}")
            close()
        }
    }

    override fun stop() {
        val tail = synchronized(lock) { session }?.let {
            try {
                textOf(it.finalResult())
            } catch (_: Throwable) {
                ""
            }
        }
        if (!tail.isNullOrBlank()) transcript = accumulator.finalize(tail)
        close()
    }

    override fun reset() {
        accumulator.reset()
        transcript = ""
    }

    private fun close() {
        synchronized(lock) {
            try {
                session?.close()
            } catch (_: Throwable) {
                // движок уже мог освободиться
            }
            session = null
        }
    }

    companion object {
        private const val TAG = "Karaoke"

        /** Имя каталога модели в filesDir (сюда её кладёт докачка). */
        const val MODEL_DIR_NAME = "vosk-model-ru"

        /**
         * Модель считается готовой, только если распакована целиком: у Vosk это
         * подкаталоги `am` и `conf`. Иначе оборванная докачка роняла бы движок
         * при первом же удержании микрофона.
         */
        fun isModelReady(dir: File): Boolean =
            dir.isDirectory && File(dir, "am").isDirectory && File(dir, "conf").isDirectory

        /** Vosk отдаёт JSON вида `{"partial":"..."}` / `{"text":"..."}`. */
        internal fun textOf(json: String?): String {
            if (json.isNullOrBlank()) return ""
            return try {
                val obj = JSONObject(json)
                (obj.optString("text").takeIf { it.isNotBlank() }
                    ?: obj.optString("partial")).trim()
            } catch (_: Throwable) {
                ""
            }
        }
    }
}

/** Одна сессия распознавания (обёртка над `org.vosk.Recognizer`). */
interface VoskSession {
    /** true — фраза закрыта (можно забирать [result]). */
    fun accept(pcm: ByteArray, length: Int): Boolean
    fun partialResult(): String
    fun result(): String
    fun finalResult(): String
    fun close()
}

/** Доступ к движку Vosk; подменяется фейком в тестах. */
interface VoskEngine {
    /** Есть ли библиотека на устройстве (в сборке без модуля — false). */
    val isAvailable: Boolean
    fun open(modelPath: String, sampleRate: Float): VoskSession
}

/**
 * Реализация через рефлексию: классы `org.vosk.*` подключаются вместе с
 * докачиваемым модулем, поэтому прямая ссылка на них ломала бы сборку.
 */
object ReflectiveVoskEngine : VoskEngine {
    override val isAvailable: Boolean by lazy {
        try {
            Class.forName("org.vosk.Recognizer")
            true
        } catch (_: Throwable) {
            false
        }
    }

    override fun open(modelPath: String, sampleRate: Float): VoskSession {
        val modelClass = Class.forName("org.vosk.Model")
        val model = modelClass.getConstructor(String::class.java).newInstance(modelPath)
        val recognizerClass = Class.forName("org.vosk.Recognizer")
        val recognizer = recognizerClass
            .getConstructor(modelClass, Float::class.javaPrimitiveType)
            .newInstance(model, sampleRate)
        return ReflectiveSession(model, recognizer)
    }

    private class ReflectiveSession(
        private val model: Any,
        private val recognizer: Any,
    ) : VoskSession {
        private val cls = recognizer.javaClass

        override fun accept(pcm: ByteArray, length: Int): Boolean =
            cls.getMethod("acceptWaveForm", ByteArray::class.java, Int::class.javaPrimitiveType)
                .invoke(recognizer, pcm, length) as Boolean

        override fun partialResult(): String = call("getPartialResult")
        override fun result(): String = call("getResult")
        override fun finalResult(): String = call("getFinalResult")

        override fun close() {
            runCatching { cls.getMethod("close").invoke(recognizer) }
            runCatching { model.javaClass.getMethod("close").invoke(model) }
        }

        private fun call(name: String): String =
            cls.getMethod(name).invoke(recognizer) as? String ?: ""
    }
}
