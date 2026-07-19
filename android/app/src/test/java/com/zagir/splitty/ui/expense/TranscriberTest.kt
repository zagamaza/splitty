package com.zagir.splitty.ui.expense

import com.zagir.splitty.ui.expense.transcribe.NoopTranscriber
import com.zagir.splitty.ui.expense.transcribe.createTranscriber
import com.zagir.splitty.ui.expense.transcribe.TranscriptAccumulator
import com.zagir.splitty.ui.expense.transcribe.VoskEngine
import com.zagir.splitty.ui.expense.transcribe.VoskSession
import com.zagir.splitty.ui.expense.transcribe.VoskTranscriber
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File
import java.nio.file.Files

/**
 * Караоке-транскрипт (Task 13): аккумуляция сегментов, готовность модели Vosk,
 * разбор JSON движка и поведение выключенной ступени. Живые движки
 * (SpeechRecognizer/Vosk) требуют устройства — на JVM проверяется чистая логика.
 * Robolectric нужен только ради настоящего org.json (в голом unit-тесте он застаблен).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class TranscriberTest {

    @Test
    fun `flag off gives no karaoke at all`() {
        val transcriber = createTranscriber(FakeContext(modelReady = true), enabled = false)
        assertTrue(transcriber === NoopTranscriber)
        assertFalse(transcriber.isEnabled)
    }

    // --- Аккумуляция сегментов ---

    @Test
    fun `partial does not overwrite finalized segments`() {
        val acc = TranscriptAccumulator()
        assertEquals("пицца", acc.partial("пицца"))
        assertEquals("пицца за 800", acc.finalize("пицца за 800"))
        // Следующий сегмент движок начинает с нуля — он ДОПИСЫВАЕТСЯ, а не заменяет.
        assertEquals("пицца за 800 и кола", acc.partial("и кола"))
        assertEquals("пицца за 800 и кола за 200", acc.finalize("и кола за 200"))
    }

    @Test
    fun `join segments avoids leading and double spaces`() {
        assertEquals("хвост", TranscriptAccumulator.joinSegments("", "хвост"))
        assertEquals("голова", TranscriptAccumulator.joinSegments("голова", ""))
        assertEquals("голова хвост", TranscriptAccumulator.joinSegments("голова", "хвост"))
        assertEquals("", TranscriptAccumulator.joinSegments("", ""))
    }

    @Test
    fun `reset clears finalized tail`() {
        val acc = TranscriptAccumulator()
        acc.finalize("старая надиктовка")
        acc.reset()
        assertEquals("новая", acc.partial("новая"))
    }

    // --- Выключенная ступень ---

    @Test
    fun `noop transcriber stays silent and disabled`() {
        assertFalse(NoopTranscriber.isEnabled)
        NoopTranscriber.start()
        NoopTranscriber.feed(ByteArray(64))
        NoopTranscriber.stop()
        assertEquals("", NoopTranscriber.transcript)
    }

    // --- Vosk: модель on-demand ---

    @Test
    fun `model is ready only when fully unpacked`() {
        val root = Files.createTempDirectory("vosk").toFile()
        val model = File(root, VoskTranscriber.MODEL_DIR_NAME)
        assertFalse("нет каталога — ступень выключена", VoskTranscriber.isModelReady(model))
        model.mkdirs()
        assertFalse("пустой каталог — докачка оборвалась", VoskTranscriber.isModelReady(model))
        File(model, "am").mkdirs()
        assertFalse("половина модели — тоже не готова", VoskTranscriber.isModelReady(model))
        File(model, "conf").mkdirs()
        assertTrue(VoskTranscriber.isModelReady(model))
    }

    @Test
    fun `vosk json is parsed for partial and final results`() {
        assertEquals("пицца", VoskTranscriber.textOf("""{"partial":"пицца"}"""))
        assertEquals("пицца за 800", VoskTranscriber.textOf("""{"text":"пицца за 800"}"""))
        assertEquals("", VoskTranscriber.textOf("""{"partial":""}"""))
        assertEquals("", VoskTranscriber.textOf("не json"))
        assertEquals("", VoskTranscriber.textOf(null))
    }

    @Test
    fun `vosk accumulates segments from engine`() {
        val engine = FakeVoskEngine(
            listOf(
                Chunk(isFinal = false, text = """{"partial":"пицца"}"""),
                Chunk(isFinal = true, text = """{"text":"пицца за 800"}"""),
                Chunk(isFinal = false, text = """{"partial":"и кола"}"""),
            ),
            tail = """{"text":"и кола за 200"}""",
        )
        val transcriber = VoskTranscriber(FakeContext(modelReady = true), engine)
        assertTrue(transcriber.isEnabled)
        transcriber.start()
        repeat(3) { transcriber.feed(ByteArray(320)) }
        assertEquals("пицца за 800 и кола", transcriber.transcript)
        transcriber.stop()
        // На стопе движок отдаёт остаток фразы — он тоже попадает в текст.
        assertEquals("пицца за 800 и кола за 200", transcriber.transcript)
        assertTrue("распознаватель закрыт — RAM модели освобождена", engine.closed)
    }

    @Test
    fun `vosk without model is disabled and ignores audio`() {
        val engine = FakeVoskEngine(emptyList(), tail = "")
        val transcriber = VoskTranscriber(FakeContext(modelReady = false), engine)
        assertFalse(transcriber.isEnabled)
        transcriber.start()
        transcriber.feed(ByteArray(320))
        assertEquals("", transcriber.transcript)
        assertNull("движок даже не поднимался", engine.openedPath)
    }

    // --- Фейки ---

    private data class Chunk(val isFinal: Boolean, val text: String)

    private class FakeVoskEngine(
        private val chunks: List<Chunk>,
        private val tail: String,
    ) : VoskEngine {
        override val isAvailable = true
        var openedPath: String? = null
        var closed = false

        override fun open(modelPath: String, sampleRate: Float): VoskSession {
            openedPath = modelPath
            return object : VoskSession {
                private var index = 0
                private var last: Chunk? = null

                override fun accept(pcm: ByteArray, length: Int): Boolean {
                    last = chunks.getOrNull(index)
                    index++
                    return last?.isFinal == true
                }

                override fun partialResult(): String = last?.text.orEmpty()
                override fun result(): String = last?.text.orEmpty()
                override fun finalResult(): String = tail
                override fun close() { closed = true }
            }
        }
    }

    /** Минимальный контекст: [VoskTranscriber] нужен только filesDir. */
    private class FakeContext(modelReady: Boolean) : android.content.ContextWrapper(null) {
        private val dir: File = Files.createTempDirectory("files").toFile().also { root ->
            if (modelReady) {
                val model = File(root, VoskTranscriber.MODEL_DIR_NAME)
                File(model, "am").mkdirs()
                File(model, "conf").mkdirs()
            }
        }

        override fun getFilesDir(): File = dir
    }
}
