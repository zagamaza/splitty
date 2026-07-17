package com.zagir.splitty.ui.expense

import kotlin.math.PI
import kotlin.math.sin
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Чистая математика записи голоса (Task 11): WAV-заголовок, кап PCM, нормализация
 * уровня по RMS (формула iOS) и ресемпл к 16 кГц. Всё — JVM без устройства
 * (сам AudioRecord тестируется на девайсе).
 */
class AudioRecorderTest {

    // --- WAV-заголовок ---

    @Test
    fun `wrapWav writes canonical 44-byte header`() {
        val pcm = ByteArray(100) { it.toByte() }

        val wav = wrapWav(pcm, 16_000)

        assertEquals(144, wav.size) // 44 заголовок + 100 PCM
        assertEquals("RIFF", String(wav.copyOfRange(0, 4), Charsets.US_ASCII))
        assertEquals("WAVE", String(wav.copyOfRange(8, 12), Charsets.US_ASCII))
        assertEquals("fmt ", String(wav.copyOfRange(12, 16), Charsets.US_ASCII))
        assertEquals("data", String(wav.copyOfRange(36, 40), Charsets.US_ASCII))
    }

    @Test
    fun `wrapWav encodes sizes rate and format little-endian`() {
        val pcm = ByteArray(200)

        val wav = wrapWav(pcm, 16_000)

        assertEquals(36 + 200, readLe32(wav, 4))   // RIFF chunk size
        assertEquals(16, readLe32(wav, 16))        // fmt chunk size
        assertEquals(1, readLe16(wav, 20))         // PCM
        assertEquals(1, readLe16(wav, 22))         // mono
        assertEquals(16_000, readLe32(wav, 24))    // sample rate
        assertEquals(16_000 * 2, readLe32(wav, 28)) // byte rate = rate * 2 (mono 16 бит)
        assertEquals(2, readLe16(wav, 32))         // block align
        assertEquals(16, readLe16(wav, 34))        // бит на сэмпл
        assertEquals(200, readLe32(wav, 40))       // data size
    }

    @Test
    fun `wrapWav appends pcm payload unchanged`() {
        val pcm = byteArrayOf(1, 2, 3, 4, 5)

        val wav = wrapWav(pcm, 16_000)

        assertTrue(wav.copyOfRange(44, wav.size).contentEquals(pcm))
    }

    // --- Кап PCM ---

    @Test
    fun `CappedPcmBuffer drops bytes beyond cap`() {
        val buffer = CappedPcmBuffer(cap = 10)

        buffer.append(ByteArray(6) { 1 })
        buffer.append(ByteArray(6) { 2 }) // всего 12 > cap 10

        assertEquals(10, buffer.size())
        val out = buffer.toByteArray()
        assertEquals(10, out.size)
        // первые 6 — из первого чанка, следующие 4 — из второго (обрезка по капу)
        assertEquals(1.toByte(), out[5])
        assertEquals(2.toByte(), out[6])
    }

    @Test
    fun `CappedPcmBuffer ignores writes when full`() {
        val buffer = CappedPcmBuffer(cap = 4)
        buffer.append(ByteArray(4))

        buffer.append(ByteArray(100))

        assertEquals(4, buffer.size())
    }

    @Test
    fun `CappedPcmBuffer reset empties it`() {
        val buffer = CappedPcmBuffer(cap = 10)
        buffer.append(ByteArray(5))

        buffer.reset()

        assertEquals(0, buffer.size())
    }

    // --- Нормализация уровня (RMS → 0..1) ---

    @Test
    fun `rmsLevel is zero for silence`() {
        assertEquals(0f, rmsLevel(ShortArray(1000)))
    }

    @Test
    fun `rmsLevel is zero for empty buffer`() {
        assertEquals(0f, rmsLevel(ShortArray(0)))
    }

    @Test
    fun `rmsLevel saturates to one for full-scale tone`() {
        // Полная амплитуда → RMS ≈ 23170 → дБ ≈ -3 → окно [-50,-8] насыщается в 1.
        val loud = ShortArray(1000) { if (it % 2 == 0) Short.MAX_VALUE else Short.MIN_VALUE }

        assertEquals(1f, rmsLevel(loud))
    }

    @Test
    fun `rmsLevel is monotonic in amplitude`() {
        val quiet = ShortArray(1000) { if (it % 2 == 0) 200 else -200 }
        val medium = ShortArray(1000) { if (it % 2 == 0) 3000 else -3000 }
        val loud = ShortArray(1000) { if (it % 2 == 0) 20000 else -20000 }

        val lq = rmsLevel(quiet)
        val lm = rmsLevel(medium)
        val ll = rmsLevel(loud)

        assertTrue(lq < lm, "quiet < medium ($lq !< $lm)")
        assertTrue(lm < ll, "medium < loud ($lm !< $ll)")
        assertTrue(lq in 0f..1f && ll in 0f..1f)
    }

    @Test
    fun `rmsLevel matches iOS formula for known rms`() {
        // rms = 3276.8 (амплитуда квадрата даёт ровно это) → db = 20*log10(0.1) = -20
        // → (−20 + 50)/42 ≈ 0.714
        val samples = ShortArray(1000) { if (it % 2 == 0) 3277 else -3277 }

        val level = rmsLevel(samples)

        assertTrue(level in 0.70f..0.72f, "got $level")
    }

    // --- Ресемпл к 16 кГц ---

    @Test
    fun `resampleTo16k returns copy at target rate`() {
        val input = shortArrayOf(1, 2, 3, 4)

        val out = resampleTo16k(input, inputRate = 16_000)

        assertTrue(out.contentEquals(input))
    }

    @Test
    fun `resampleTo16k downsamples 48k by factor three`() {
        val input = ShortArray(48_000) { (it % 100).toShort() }

        val out = resampleTo16k(input, inputRate = 48_000)

        assertEquals(16_000, out.size)
    }

    @Test
    fun `resampleTo16k upsamples 8k by factor two`() {
        val input = ShortArray(8_000) { (it % 50).toShort() }

        val out = resampleTo16k(input, inputRate = 8_000)

        assertEquals(16_000, out.size)
    }

    @Test
    fun `resampleTo16k preserves ramp shape`() {
        // Линейная пила ресемплится в такую же линейную пилу (без ступеней/выбросов).
        val input = ShortArray(4_800) { (it % 480).toShort() }

        val out = resampleTo16k(input, inputRate = 48_000)

        assertEquals(1_600, out.size)
        assertEquals(0.toShort(), out[0])
        // монотонный рост внутри зуба пилы
        assertTrue(out[10] < out[50], "${out[10]} !< ${out[50]}")
    }

    @Test
    fun `resampleTo16k keeps sine within valid pcm range`() {
        val input = ShortArray(44_100) { (sin(2 * PI * 440 * it / 44_100) * 20_000).toInt().toShort() }

        val out = resampleTo16k(input, inputRate = 44_100)

        assertEquals(16_000, out.size)
        assertTrue(out.all { it >= -32_768 && it <= 32_767 })
        assertTrue(out.any { it > 5_000 } && out.any { it < -5_000 }, "синус сохранил амплитуду")
    }

    @Test
    fun `resampleTo16k handles empty input`() {
        assertEquals(0, resampleTo16k(ShortArray(0), inputRate = 48_000).size)
    }

    // --- shortsToLittleEndian ---

    @Test
    fun `shortsToLittleEndian writes lsb then msb`() {
        val samples = shortArrayOf(0x0102, 0x0304)

        val bytes = shortsToLittleEndian(samples)

        assertEquals(4, bytes.size)
        assertEquals(0x02.toByte(), bytes[0])
        assertEquals(0x01.toByte(), bytes[1])
        assertEquals(0x04.toByte(), bytes[2])
        assertEquals(0x03.toByte(), bytes[3])
    }

    private fun readLe16(data: ByteArray, offset: Int): Int =
        (data[offset].toInt() and 0xFF) or ((data[offset + 1].toInt() and 0xFF) shl 8)

    private fun readLe32(data: ByteArray, offset: Int): Int =
        (data[offset].toInt() and 0xFF) or
            ((data[offset + 1].toInt() and 0xFF) shl 8) or
            ((data[offset + 2].toInt() and 0xFF) shl 16) or
            ((data[offset + 3].toInt() and 0xFF) shl 24)
}
