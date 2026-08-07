package com.zagir.splitty.core.auth

import android.net.Uri
import android.util.Base64
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Разбор возврата из Telegram Login Widget (порт iOS TelegramWebAuthTests).
 * Robolectric нужен из-за android.net.Uri и android.util.Base64 — обе заглушены
 * в JVM-юните и без него молча вернули бы null.
 */
@RunWith(RobolectricTestRunner::class)
// sdk = 34: Robolectric 4.13 не знает targetSdk приложения (36) и падает
// на конфигурации — тот же пин стоит у остальных Robolectric-тестов.
@Config(sdk = [34])
class TelegramWebAuthTest {

    private fun encode(json: String): String =
        Base64.encodeToString(json.toByteArray(), Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)

    private val payloadJson = """
        {"id":147181773,"first_name":"Загир","last_name":null,"username":"zagir",
         "photo_url":"https://t.me/i/userpic/320/zagir.jpg","auth_date":1750000000,
         "hash":"abc123"}
    """.trimIndent()

    @Test
    fun `callback узнаётся в обеих формах — схема и app link`() {
        assertTrue(TelegramWebAuth.isCallback(Uri.parse("splitty://tg-callback?tgAuthResult=x")))
        // App link: Android перехватывает https-ссылку сам, страница не грузится.
        assertTrue(TelegramWebAuth.isCallback(Uri.parse("https://splitor.zagirnur.dev/tg-callback?native=1")))
        // Приглашение в группу ходит по тем же схеме и домену — его трогать нельзя.
        assertFalse(TelegramWebAuth.isCallback(Uri.parse("splitty://join/65a0000000000000000000ff")))
        assertFalse(TelegramWebAuth.isCallback(Uri.parse("https://splitor.zagirnur.dev/join/65a0000000000000000000ff")))
    }

    /**
     * Через app link результат приезжает во FRAGMENT: его кладёт туда сам
     * Telegram, а на сервер фрагмент не уходит вовсе — там его подхватывал JS.
     * Раз страницы теперь нет, читать фрагмент обязано приложение.
     */
    @Test
    fun `app link с результатом во фрагменте разбирается`() {
        val encoded = encode(payloadJson)
        val uri = Uri.parse("https://splitor.zagirnur.dev/tg-callback?native=1#tgAuthResult=$encoded")

        assertEquals(147181773L, TelegramWebAuth.decode(uri)?.id)
    }

    @Test
    fun `payload читается из query`() {
        val uri = Uri.parse("splitty://tg-callback?tgAuthResult=${encode(payloadJson)}")

        val body = TelegramWebAuth.decode(uri)

        assertEquals(147181773L, body?.id)
        assertEquals("Загир", body?.firstName)
        assertEquals("zagir", body?.username)
        assertEquals(1750000000L, body?.authDate)
        assertEquals("abc123", body?.hash)
    }

    @Test
    fun `payload читается из fragment`() {
        // Telegram кладёт результат во fragment, сервер — в query: работать
        // обязаны оба, иначе вход ломается на половине устройств.
        val uri = Uri.parse("splitty://tg-callback#tgAuthResult=${encode(payloadJson)}")

        assertEquals(147181773L, TelegramWebAuth.decode(uri)?.id)
    }

    @Test
    fun `битый и пустой результат дают null, а не падение`() {
        assertNull(TelegramWebAuth.decode(Uri.parse("splitty://tg-callback")))
        assertNull(TelegramWebAuth.decode(Uri.parse("splitty://tg-callback?tgAuthResult=")))
        assertNull(TelegramWebAuth.decode(Uri.parse("splitty://tg-callback?tgAuthResult=не-base64")))
        // Валидный base64, но не тот JSON — обязательных полей нет.
        assertNull(TelegramWebAuth.decode(Uri.parse("splitty://tg-callback?tgAuthResult=${encode("{}")}")))
    }

    /**
     * Какой алфавит base64 придёт от Telegram — не наше дело, и опираться на
     * догадку нельзя: раньше здесь стоял голый URL_SAFE, и payload со
     * стандартным алфавитом ронял декодер. Вход умирал МОЛЧА — человек
     * оставался на экране входа без единого намёка. Терпим оба, как iOS.
     */
    @Test
    fun `payload читается и в обычном base64, и в base64url, и без padding`() {
        val standard = Base64.encodeToString(payloadJson.toByteArray(), Base64.NO_WRAP)
        val urlSafe = standard.replace('+', '-').replace('/', '_')
        val noPadding = urlSafe.trimEnd('=')

        for (variant in listOf(standard, urlSafe, noPadding)) {
            val uri = Uri.parse("splitty://tg-callback?tgAuthResult=" + Uri.encode(variant))
            assertEquals("не разобрали вариант «${variant.takeLast(6)}»", 147181773L, TelegramWebAuth.decode(uri)?.id)
        }
    }

    @Test
    fun `адрес входа собирается без двойного слеша`() {
        assertEquals("http://127.0.0.1:7171/tg-auth", TelegramWebAuth.startUrl("http://127.0.0.1:7171"))
        assertEquals("http://127.0.0.1:7171/tg-auth", TelegramWebAuth.startUrl("http://127.0.0.1:7171/"))
    }
}
