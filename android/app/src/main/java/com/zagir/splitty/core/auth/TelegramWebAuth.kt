package com.zagir.splitty.core.auth

import android.net.Uri
import android.util.Base64
import android.util.Log
import com.zagir.splitty.core.model.TelegramLoginBody
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * Вход через Telegram Login Widget без ухода в приложение Telegram —
 * порт iOS `TelegramWebAuth`. Серверная половина: `internal/rest/tg_callback.go`.
 *
 * Поток: `<baseUrl>/tg-auth` в Custom Tabs → oauth.telegram.org →
 * `/tg-callback` → `splitty://tg-callback?tgAuthResult=…` обратно в приложение.
 *
 * Подпись `hash` проверяет сервер (`checkTelegramHash`): ключа бота у клиента
 * нет и быть не должно. Здесь только разбор — поэтому он и вынесен из UI,
 * чтобы проверяться юнит-тестами без живого браузера.
 */
object TelegramWebAuth {

    /** Совпадает с `appScheme` на сервере и схемой в AndroidManifest. */
    const val CALLBACK_SCHEME = "splitty"

    /** Хост callback-а кастомной схемы: `splitty://tg-callback`. */
    const val CALLBACK_HOST = "tg-callback"

    /** Путь callback-а на домене: `https://<домен>/tg-callback` (app link). */
    const val CALLBACK_PATH = "/tg-callback"

    private const val RESULT_PARAM = "tgAuthResult"

    private const val TAG = "TelegramWebAuth"

    private val json = Json { ignoreUnknownKeys = true }

    /** Поля виджета: ключи — как в JSON Telegram, на сервер уходят в camelCase. */
    @Serializable
    private data class WidgetPayload(
        val id: Long,
        @SerialName("first_name") val firstName: String? = null,
        @SerialName("last_name") val lastName: String? = null,
        val username: String? = null,
        @SerialName("photo_url") val photoUrl: String? = null,
        @SerialName("auth_date") val authDate: Long,
        val hash: String,
    )

    /**
     * true — этот Uri пришёл из Telegram-входа, а не из приглашения в группу.
     *
     * Форм две, и обе рабочие. `https://<домен>/tg-callback` — app link:
     * Android перехватывает ссылку сам, страница-перекладчик даже не грузится.
     * `splitty://tg-callback` — запасной путь для случаев, когда верификация
     * домена не прошла (debug-подпись) и ссылку открыл браузер: тогда до
     * приложения доводит JS на странице.
     */
    fun isCallback(uri: Uri): Boolean = when (uri.scheme) {
        CALLBACK_SCHEME -> uri.host == CALLBACK_HOST
        "https" -> uri.path == CALLBACK_PATH
        else -> false
    }

    /** Адрес, который открывается в Custom Tabs. */
    fun startUrl(baseUrl: String): String = baseUrl.trimEnd('/') + "/tg-auth"

    /**
     * Достаёт payload из callback-Uri. null — Telegram результат не передал
     * или он битый; звать имеет смысл только на [isCallback].
     *
     * Результат лежит и в query, и во fragment: сервер кладёт его в query,
     * но Telegram отдаёт во fragment — разбираем оба.
     */
    fun decode(uri: Uri): TelegramLoginBody? {
        val encoded = uri.getQueryParameter(RESULT_PARAM)
            ?: uri.fragment?.let { fragment ->
                // Fragment — те же key=value через «&», Uri его не разбирает.
                Uri.parse("?$fragment").getQueryParameter(RESULT_PARAM)
            }
        if (encoded.isNullOrEmpty()) {
            Log.w(TAG, "callback без tgAuthResult: $uri")
            return null
        }

        val payload = try {
            json.decodeFromString<WidgetPayload>(decodeBase64(encoded).decodeToString())
        } catch (e: Exception) {
            // Битый base64, не тот JSON, обрезанная строка — для вызывающего
            // это один и тот же случай «Telegram не подтвердил вход».
            // В лог пишем ДЛИНУ и начало, но не весь payload: в нём подпись входа.
            Log.w(TAG, "не разобрали tgAuthResult (len=${encoded.length}, начало=${encoded.take(12)}…)", e)
            return null
        }

        return TelegramLoginBody(
            id = payload.id,
            firstName = payload.firstName,
            lastName = payload.lastName,
            username = payload.username,
            photoUrl = payload.photoUrl,
            authDate = payload.authDate,
            hash = payload.hash,
        )
    }

    /**
     * Декодирует и base64url, и обычный base64.
     *
     * Какой именно алфавит пришлёт Telegram — не наше дело: в URL результат
     * попадает через `encodeURIComponent`, и «+» доезжает целым. Раньше здесь
     * стоял голый `Base64.URL_SAFE`, и payload со стандартным алфавитом ронял
     * декодер — вход умирал молча, оставляя человека на экране входа.
     * iOS терпим к обоим (`Data(base64URLEncoded:)`), теперь и Android.
     */
    private fun decodeBase64(raw: String): ByteArray {
        val normalized = raw.replace('-', '+').replace('_', '/')
        // Хвостовые «=» Telegram может и опустить — дополняем сами.
        val padded = normalized.padEnd(normalized.length + (4 - normalized.length % 4) % 4, '=')
        return Base64.decode(padded, Base64.DEFAULT)
    }
}
