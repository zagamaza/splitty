package com.zagir.splitty.ui.expense

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.provider.Settings
import android.util.Log
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.app.ActivityCompat
import com.zagir.splitty.ui.expense.transcribe.NoopTranscriber
import com.zagir.splitty.ui.expense.transcribe.Transcriber
import com.zagir.splitty.ui.expense.transcribe.rememberTranscriber
import java.io.ByteArrayOutputStream
import java.io.File
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** Целевая частота дискретизации записи — как в iOS (audio/wav 16 кГц PCM16 mono). */
const val AUDIO_TARGET_SAMPLE_RATE = 16_000

/**
 * Кап сырого PCM (~2.8 МБ ≈ 87 с при 16 кГц/16 бит/mono) держит WAV под серверным
 * лимитом 3 МБ — как в iOS. Более длинная надиктовка вырождена; лишнее отбрасываем.
 */
const val AUDIO_CAP_BYTES = 2_800_000

/**
 * Кандидаты источника записи. VOICE_RECOGNITION первым: канал без агрессивной
 * пост-обработки (AGC/шумодав), которая портит распознавание; MIC — fallback,
 * если устройство не отдаёт VOICE_RECOGNITION. Финальный выбор — на устройстве
 * (первый источник, с которым AudioRecord инициализируется).
 */
internal val AUDIO_SOURCES = intArrayOf(
    MediaRecorder.AudioSource.VOICE_RECOGNITION,
    MediaRecorder.AudioSource.MIC,
)

/**
 * Кандидаты частоты. 16 кГц напрямую (без ресемпла) там, где железо его даёт;
 * иначе пишем на «родной» частоте и ресемплим к 16 кГц. 44.1/48 кГц гарантированы
 * спецификацией Android на любом устройстве.
 */
internal val AUDIO_SAMPLE_RATES = intArrayOf(AUDIO_TARGET_SAMPLE_RATE, 44_100, 48_000)

/** Ошибки записи голоса — текст показывается пользователю (как AudioRecorderError в iOS). */
class AudioRecorderException(message: String) : Exception(message)

/**
 * Запись голосового ввода расхода (hold-to-talk) через [AudioRecord] — зеркало
 * iOS `AudioRecorder`. Захват PCM16 mono на первой поднявшейся паре источник×частота,
 * ресемпл к 16 кГц при необходимости, кап [AUDIO_CAP_BYTES], уровень громкости 0…1
 * по RMS (формула iOS). На стопе PCM оборачивается WAV-заголовком и пишется в
 * cacheDir (переживает process death; путь — в [lastAudioPath]).
 *
 * Живая логика записи не тестируется на JVM (нужно железо); чистая математика —
 * [wrapWav]/[rmsLevel]/[resampleTo16k]/[CappedPcmBuffer] — покрыта unit-тестами.
 */
class AudioRecorderController(
    private val context: Context,
    /**
     * Караоке-транскрипт: кормится ТЕМ ЖЕ PCM, что уходит в WAV (второй захват
     * микрофона запрещён). По умолчанию выключен — [NoopTranscriber].
     */
    private val transcriber: Transcriber = NoopTranscriber,
) : VoiceRecorder {

    /** Текст караоке или null, если ступень транскрипции выключена (окна нет). */
    val transcript: String?
        get() = if (transcriber.isEnabled) transcriber.transcript else null
    /** true — идёт запись (подсветка микрофона и оверлея). */
    override var isRecording by mutableStateOf(false)
        private set

    /** Момент старта (elapsedRealtime, мс) — для кольца-прогресса лимита и автостопа; null вне записи. */
    var startedAtElapsedMs by mutableStateOf<Long?>(null)
        private set

    /** Текущая громкость 0…1 (RMS буфера) — волна и «дыхание» микрофона реагируют на реальный звук. */
    var level by mutableStateOf(0f)
        private set

    /** WAV последней записи (его шлёт parse); null — записи ещё не было. */
    var audioData by mutableStateOf<ByteArray?>(null)
        private set

    /** Путь к WAV последней записи в cacheDir (переживает process death). */
    override var lastAudioPath by mutableStateOf<String?>(null)
        private set

    /** MIME записанного аудио (серверный allowlist). */
    val mimeType = "audio/wav"

    /**
     * Сессия записи оборвалась на стороне системы (входящий звонок, отъём
     * микрофона): цикл чтения получил терминальный код. Экран обязан
     * остановить запись — иначе UI «пишет» до автостопа впустую.
     */
    var deviceLost by mutableStateOf(false)
        private set

    private val mainHandler = Handler(Looper.getMainLooper())
    @Volatile private var record: AudioRecord? = null
    private var readThread: Thread? = null
    @Volatile private var running = false

    /**
     * Поток чтения не завершился за [JOIN_TIMEOUT_MS] — освобождает [AudioRecord]
     * он сам, на своём выходе. Звать release() «поверх» живого read() нельзя:
     * нативный слой уходит в use-after-free.
     *
     * Флаг принадлежит КОНКРЕТНОЙ сессии записи, а не контроллеру: общий флаг
     * вместе с общим полем [record] давал отпускание чужого, уже нового
     * микрофона, если отставший поток доходил до finally после нового start().
     */
    private class ReaderHandoff {
        @Volatile var releaseByReader = false
    }

    @Volatile private var handoff: ReaderHandoff? = null

    /** Запись WAV на диск — вне главного потока (до 2.8 МБ, это ANR на стопе). */
    private val ioScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /**
     * Активная запись WAV. [lastAudioPath] известен сразу (путь детерминированный),
     * но байтов по нему ещё нет — читатель, стартовавший сразу после [stop],
     * видел пустоту и показывал «Запись не сохранилась» на исправной диктовке
     * (старый файл к этому моменту уже удалён). Ждём через [awaitAudioPersisted].
     */
    @Volatile private var writeJob: Job? = null

    override suspend fun awaitAudioPersisted() {
        writeJob?.join()
    }

    /**
     * Начинает запись. Подъём [AudioRecord] и цикл чтения — в фоновом потоке:
     * на главном они замораживали бы отрисовку оверлея (в iOS та же причина).
     * Бросает [AudioRecorderException], если ни одна пара источник×частота не поднялась.
     * Разрешение RECORD_AUDIO должно быть выдано ДО вызова (см. [rememberRecordAudioPermission]).
     */
    override fun start() {
        if (isRecording) return
        // Предыдущая сессия не отпустила поток чтения: её микрофон ещё открыт, и
        // её finally вот-вот дёрнет release(). Открывать вторую сессию поверх —
        // это чужой release() по нашему AudioRecord и дозапись старого PCM
        // в новый буфер. Ждём столько же, сколько ждал стоп.
        readThread?.takeIf { it.isAlive }?.let { stale ->
            stale.join(JOIN_TIMEOUT_MS)
            if (stale.isAlive) {
                throw AudioRecorderException("Микрофон ещё занят прошлой записью. Попробуйте ещё раз")
            }
        }
        readThread = null
        audioData = null
        deviceLost = false
        val (rec, sampleRate) = openRecord()
            ?: throw AudioRecorderException("Не удалось начать запись. Проверьте доступ к микрофону")
        record = rec
        // Свежий буфер на сессию: общий копил хвост прошлой записи в новый WAV.
        val sessionPcm = CappedPcmBuffer(AUDIO_CAP_BYTES)
        pcm = sessionPcm
        val sessionHandoff = ReaderHandoff()
        handoff = sessionHandoff
        running = true
        isRecording = true
        startedAtElapsedMs = SystemClock.elapsedRealtime()
        level = 0f
        transcriber.start()

        val thread = Thread(
            { readLoop(rec, sampleRate, sessionPcm, sessionHandoff) },
            "splitty-audio",
        )
        thread.priority = Thread.MAX_PRIORITY
        readThread = thread
        thread.start()
    }

    /** Цикл чтения PCM: ресемпл к 16 кГц → кап → уровень. Копит [pcm] до стопа. */
    @Volatile private var pcm = CappedPcmBuffer(AUDIO_CAP_BYTES)

    private fun readLoop(
        rec: AudioRecord,
        sampleRate: Int,
        pcm: CappedPcmBuffer,
        handoff: ReaderHandoff,
    ) {
        // Всё тело цикла под Throwable: это отдельный поток, и любое необработанное
        // исключение здесь убивало процесс целиком, не отпустив микрофон. Самый
        // реальный случай — SecurityException, когда RECORD_AUDIO отозвали после
        // разрешения «только в этот раз», а экран об этом ещё не знает.
        try {
            readLoopInner(rec, sampleRate, pcm)
        } catch (t: Throwable) {
            Log.w("AudioRecorder", "read loop failed: ${t.message}")
            running = false
            mainHandler.post { if (isRecording) deviceLost = true }
        } finally {
            // stop()/cancel() не дождались нас в join — освободить микрофон должны
            // мы: там release() пришёлся бы на живой read() внутри драйвера.
            // Освобождаем СВОЙ rec, а не поле record: оно могло уже указывать
            // на микрофон следующей сессии.
            if (handoff.releaseByReader) {
                handoff.releaseByReader = false
                releaseRecord(rec)
            }
        }
    }

    private fun readLoopInner(rec: AudioRecord, sampleRate: Int, pcm: CappedPcmBuffer) {
        // Кадр ~100 мс: достаточно частый уровень для «живой» волны, без перегрузки.
        val frame = ShortArray(max(sampleRate / 10, 1024))
        try {
            rec.startRecording()
        } catch (_: IllegalStateException) {
            // Тот же терминальный случай, что и read < 0 ниже: сессия не стартовала
            // (микрофон занят звонком/другим приложением). Молчаливый выход держал
            // микрофон и оставлял «идущую» запись на экране до лимита в 60 с.
            running = false
            mainHandler.post { if (isRecording) deviceLost = true }
            return
        }
        while (running) {
            val read = rec.read(frame, 0, frame.size)
            // Отрицательные коды (ERROR_DEAD_OBJECT при входящем звонке, ERROR,
            // ERROR_INVALID_OPERATION) — терминальные для сессии. `continue`
            // превращал это в busy-spin на MAX_PRIORITY-потоке до отпускания
            // микрофона, а под замком — навсегда.
            if (read < 0) {
                running = false
                // Молча выйти нельзя: isRecording остаётся true, уровень замирает,
                // и пользователь ещё до минуты смотрит на «идущую» запись, после
                // чего в parse уходит обрезанный на середине WAV. Сообщаем экрану —
                // он останавливает запись как по лимиту.
                mainHandler.post { if (isRecording) deviceLost = true }
                return
            }
            if (read == 0) continue
            val samples16k =
                if (sampleRate == AUDIO_TARGET_SAMPLE_RATE) frame.copyOf(read)
                else resampleTo16k(frame, read, sampleRate)
            val bytes = shortsToLittleEndian(samples16k)
            pcm.append(bytes)
            transcriber.feed(bytes)
            val lvl = rmsLevel(samples16k)
            mainHandler.post { if (isRecording) level = lvl }
        }
    }

    /**
     * Останавливает запись (отпустили микрофон), собирает WAV в [audioData] и пишет
     * файл. Возвращает данные или null, если запись не велась/пуста.
     */
    override fun stop(): ByteArray? {
        if (!isRecording) return null
        running = false
        joinReaderAndRelease()
        transcriber.stop()
        isRecording = false
        startedAtElapsedMs = null
        level = 0f

        val raw = pcm.toByteArray()
        pcm.reset()
        if (raw.isEmpty()) return null
        val wav = wrapWav(raw, AUDIO_TARGET_SAMPLE_RATE)
        audioData = wav
        // Путь детерминированный, поэтому известен сразу — а сама запись (до 2.8 МБ)
        // уходит на IO: на главном потоке она давала ANR ровно в момент отпускания
        // микрофона. Старый файл удаляем синхронно (это unlink, не запись байтов),
        // чтобы читатель, успевший раньше, не подхватил ПРЕДЫДУЩУЮ диктовку;
        // сама запись атомарна (tmp + rename) — рваного WAV не бывает.
        val target = audioCacheFile(context)
        runCatching { target.delete() }
        lastAudioPath = target.absolutePath
        writeJob = ioScope.launch {
            runCatching { writeAudioAtomically(target, wav) }
                .onFailure { Log.w("AudioRecorder", "не удалось сохранить WAV: ${it.message}") }
        }
        return wav
    }

    /**
     * Дожидается выхода потока чтения и освобождает [AudioRecord]. Не уложился в
     * таймаут — освобождение делегируется самому потоку ([releaseOnReaderExit]):
     * release() из-под живого read() роняет нативный аудио-слой.
     */
    private fun joinReaderAndRelease() {
        val thread = readThread
        val rec = record
        thread?.join(JOIN_TIMEOUT_MS)
        if (thread != null && thread.isAlive) {
            // Поток ещё в read(): отпустит микрофон сам. readThread НЕ обнуляем —
            // следующий start() обязан его увидеть и не открыть вторую сессию
            // поверх живой.
            handoff?.releaseByReader = true
            record = null
        } else {
            readThread = null
            if (rec != null) releaseRecord(rec)
        }
        handoff = null
    }

    /**
     * Отменяет запись без сохранения (свайп-отмена/уход экрана/lifecycle). НИКОГДА
     * не оставляет открытый [AudioRecord] — иначе второй захват микрофона повиснет.
     */
    override fun cancel() {
        running = false
        joinReaderAndRelease()
        transcriber.stop()
        transcriber.reset()
        pcm.reset()
        isRecording = false
        startedAtElapsedMs = null
        level = 0f
    }

    /**
     * Уход экрана: останавливает запись и гасит [ioScope]. Без отмены скоупа
     * незавершённая запись WAV переживала контроллер.
     */
    fun dispose() {
        cancel()
        ioScope.cancel()
    }

    /** Сбрасывает записанное (после отправки/отмены формы). */
    override fun reset() {
        audioData = null
        lastAudioPath = null
        transcriber.reset()
    }

    /** Пробует источники×частоты по порядку; возвращает первую инициализированную запись. */
    private fun openRecord(): Pair<AudioRecord, Int>? {
        for (source in AUDIO_SOURCES) {
            for (rate in AUDIO_SAMPLE_RATES) {
                val minBuffer = AudioRecord.getMinBufferSize(
                    rate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
                )
                if (minBuffer <= 0) continue // частота не поддерживается
                val rec = try {
                    buildRecord(source, rate, max(minBuffer * 4, rate))
                } catch (_: IllegalArgumentException) {
                    continue
                } catch (_: UnsupportedOperationException) {
                    continue
                }
                if (rec.state != AudioRecord.STATE_INITIALIZED) {
                    rec.release()
                    continue
                }
                return rec to rate
            }
        }
        return null
    }

    /**
     * Строит [AudioRecord] через Builder: privacySensitive (API 30+) помечает поток
     * приватным — система не отдаёт наш микрофон конкурентам во время записи (у голого
     * конструктора такого сеттера нет). Разрешение RECORD_AUDIO проверено вызывающим.
     */
    @Suppress("MissingPermission")
    private fun buildRecord(source: Int, rate: Int, bufferBytes: Int): AudioRecord {
        val format = AudioFormat.Builder()
            .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
            .setSampleRate(rate)
            .setChannelMask(AudioFormat.CHANNEL_IN_MONO)
            .build()
        return AudioRecord.Builder()
            .setAudioSource(source)
            .setAudioFormat(format)
            .setBufferSizeInBytes(bufferBytes)
            .apply {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) setPrivacySensitive(true)
            }
            .build()
    }

    @Synchronized
    /**
     * Освобождает КОНКРЕТНЫЙ [AudioRecord]. Поле [record] обнуляется, только если
     * оно всё ещё указывает на него: отставший поток чтения не должен занулять
     * ссылку на микрофон уже начавшейся следующей записи.
     */
    private fun releaseRecord(rec: AudioRecord) {
        try {
            if (rec.recordingState == AudioRecord.RECORDSTATE_RECORDING) rec.stop()
        } catch (_: IllegalStateException) {
            // уже остановлен — игнорируем
        }
        rec.release()
        if (record === rec) record = null
    }

    private companion object {
        /** Сколько ждём выход потока чтения на стопе/отмене, мс. */
        const val JOIN_TIMEOUT_MS = 500L
    }
}

/**
 * Готовит рекордер, привязанный к жизни экрана: на выходе из композиции [cancel]
 * гарантирует, что микрофон освобождён (lifecycle-cancel — как в iOS stop по сцене).
 */
@Composable
fun rememberAudioRecorder(): AudioRecorderController {
    val context = LocalContext.current
    val transcriber = rememberTranscriber()
    val controller = remember(transcriber) {
        AudioRecorderController(context.applicationContext, transcriber)
    }
    DisposableEffect(Unit) {
        onDispose { controller.dispose() }
    }
    return controller
}

/**
 * Накопитель PCM с жёстким капом [cap] байт: пишется из аудио-потока чтения,
 * читается на стопе с главного. Свыше капа — молча отбрасываем (как PCMSink в iOS).
 */
class CappedPcmBuffer(private val cap: Int) {
    private val buffer = ByteArrayOutputStream()

    @Synchronized
    fun append(chunk: ByteArray) {
        val remaining = cap - buffer.size()
        if (remaining <= 0) return
        buffer.write(chunk, 0, min(remaining, chunk.size))
    }

    @Synchronized
    fun toByteArray(): ByteArray = buffer.toByteArray()

    @Synchronized
    fun reset() = buffer.reset()

    @Synchronized
    fun size(): Int = buffer.size()
}

/** PCM16 mono little-endian: каждый сэмпл — 2 байта (LSB, MSB). */
internal fun shortsToLittleEndian(samples: ShortArray, count: Int = samples.size): ByteArray {
    val out = ByteArray(count * 2)
    for (i in 0 until count) {
        val v = samples[i].toInt()
        out[i * 2] = (v and 0xFF).toByte()
        out[i * 2 + 1] = ((v shr 8) and 0xFF).toByte()
    }
    return out
}

/**
 * Уровень громкости 0…1 по RMS — ТОЧНАЯ формула iOS (`AudioRecorder.makeEngine`):
 * RMS каждого 4-го сэмпла (дёшево и достаточно), перевод в дБ и линейное окно
 * [-50 дБ … -8 дБ] → 0…1. Тихо → 0, громкая речь → ближе к 1.
 */
fun rmsLevel(samples: ShortArray, count: Int = samples.size, stride: Int = 4): Float {
    if (count <= 0) return 0f
    var acc = 0f
    var n = 0
    var i = 0
    while (i < count) {
        val v = samples[i].toFloat()
        acc += v * v
        n++
        i += stride
    }
    val rms = sqrt(acc / max(1, n))
    val db = 20f * log10(max(rms, 1f) / 32_768f)
    return max(0f, min(1f, (db + 50f) / 42f))
}

/**
 * Линейный ресемпл PCM16 к 16 кГц с частоты [inputRate]. Возвращает [input] как есть,
 * если [inputRate] уже целевой. На синтетике (пила/тон) даёт монотонную интерполяцию.
 */
fun resampleTo16k(input: ShortArray, count: Int = input.size, inputRate: Int): ShortArray {
    if (inputRate == AUDIO_TARGET_SAMPLE_RATE) return input.copyOf(count)
    if (count <= 0) return ShortArray(0)
    val outLen = ((count.toLong() * AUDIO_TARGET_SAMPLE_RATE) / inputRate).toInt()
    if (outLen <= 0) return ShortArray(0)
    val out = ShortArray(outLen)
    val step = inputRate.toDouble() / AUDIO_TARGET_SAMPLE_RATE
    for (j in 0 until outLen) {
        val src = j * step
        val i0 = src.toInt()
        val frac = src - i0
        val a = input[i0].toDouble()
        val b = if (i0 + 1 < count) input[i0 + 1].toDouble() else a
        out[j] = (a + (b - a) * frac).toInt().coerceIn(-32_768, 32_767).toShort()
    }
    return out
}

/** Оборачивает сырой PCM (16 бит LE, mono) в WAV-контейнер — как `AudioRecorder.wav` в iOS. */
fun wrapWav(pcm: ByteArray, sampleRate: Int): ByteArray {
    val byteRate = sampleRate * 2 // mono, 16 бит
    val header = ByteArrayOutputStream(44)
    header.write("RIFF".toByteArray(Charsets.US_ASCII))
    header.write(le32(36 + pcm.size))
    header.write("WAVE".toByteArray(Charsets.US_ASCII))
    header.write("fmt ".toByteArray(Charsets.US_ASCII))
    header.write(le32(16))          // размер fmt-чанка
    header.write(le16(1))           // PCM
    header.write(le16(1))           // mono
    header.write(le32(sampleRate))
    header.write(le32(byteRate))
    header.write(le16(2))           // block align
    header.write(le16(16))          // бит на сэмпл
    header.write("data".toByteArray(Charsets.US_ASCII))
    header.write(le32(pcm.size))
    return header.toByteArray() + pcm
}

private fun le16(v: Int): ByteArray = byteArrayOf((v and 0xFF).toByte(), ((v shr 8) and 0xFF).toByte())

private fun le32(v: Int): ByteArray = byteArrayOf(
    (v and 0xFF).toByte(),
    ((v shr 8) and 0xFF).toByte(),
    ((v shr 16) and 0xFF).toByte(),
    ((v shr 24) and 0xFF).toByte(),
)

/**
 * Файл последней записи в cacheDir. Имя фиксированное (детерминизм, без
 * Random/времени): нужен только один «последний» голос — перезаписываем.
 * Путь переживает process death (восстановление голоса).
 */
fun audioCacheFile(context: Context): File {
    val dir = File(context.cacheDir, "audio").apply { mkdirs() }
    return File(dir, "voice.wav")
}

/**
 * Пишет WAV атомарно: сначала tmp, затем переименование. Читатель (отправка на
 * /parse) видит либо полный файл, либо ничего — но не обрезанный на середине.
 */
fun writeAudioAtomically(target: File, bytes: ByteArray) {
    val tmp = File(target.parentFile, "${target.name}.tmp")
    tmp.writeBytes(bytes)
    if (!tmp.renameTo(target)) {
        // ФС без атомарного rename — обычная перезапись.
        target.writeBytes(bytes)
        tmp.delete()
    }
}

/** Пишет WAV в cacheDir и возвращает путь (синхронный вариант — для тестов/утилит). */
fun writeAudioToCache(context: Context, bytes: ByteArray): String {
    val file = audioCacheFile(context)
    writeAudioAtomically(file, bytes)
    return file.absolutePath
}

// MARK: разрешение RECORD_AUDIO

/**
 * Готовит запрос разрешения на микрофон для hold-to-talk. Возвращает лямбду
 * `request()`:
 * - разрешение уже есть → [onGranted] сразу;
 * - не спрашивали/можно спросить → системный prompt, при выдаче → [onGranted];
 * - «навсегда отказано» (отказ + `shouldShowRequestPermissionRationale == false`)
 *   → [onPermanentlyDenied] (экран показывает алерт с переходом в настройки).
 */
@Composable
fun rememberRecordAudioPermission(
    onGranted: () -> Unit,
    onPermanentlyDenied: () -> Unit,
): () -> Unit {
    val context = LocalContext.current
    val grantedCb by rememberUpdatedState(onGranted)
    val deniedCb by rememberUpdatedState(onPermanentlyDenied)

    val launcher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        if (granted) {
            grantedCb()
        } else {
            val activity = context.findActivity()
            val canAskAgain = activity != null &&
                ActivityCompat.shouldShowRequestPermissionRationale(activity, Manifest.permission.RECORD_AUDIO)
            // Отказ и просить больше нельзя → это «навсегда», ведём в настройки.
            if (!canAskAgain) deniedCb()
        }
    }

    return remember {
        {
            if (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                grantedCb()
            } else {
                launcher.launch(Manifest.permission.RECORD_AUDIO)
            }
        }
    }
}

/** Открывает системные настройки приложения (для «навсегда отказано» → выдать вручную). */
fun openAppSettings(context: Context) {
    val intent = Intent(
        Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
        Uri.fromParts("package", context.packageName, null),
    ).apply { addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) }
    context.startActivity(intent)
}

/** Достаёт Activity из context (для shouldShowRequestPermissionRationale). */
private fun Context.findActivity(): Activity? {
    var ctx = this
    while (ctx is android.content.ContextWrapper) {
        if (ctx is Activity) return ctx
        ctx = ctx.baseContext
    }
    return null
}

/** Выдано ли RECORD_AUDIO: до жеста, чтобы удержание не упиралось в SecurityException. */
fun hasRecordAudioPermission(context: Context): Boolean =
    context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
