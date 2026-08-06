package com.zagir.splitty.core.auth

import android.net.Uri
import android.util.Base64
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

    /** Хост callback-а: `splitty://tg-callback`. */
    const val CALLBACK_HOST = "tg-callback"

    private const val RESULT_PARAM = "tgAuthResult"

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

    /** true — этот Uri пришёл из Telegram-входа, а не из приглашения в группу. */
    fun isCallback(uri: Uri): Boolean =
        uri.scheme == CALLBACK_SCHEME && uri.host == CALLBACK_HOST

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
        if (encoded.isNullOrEmpty()) return null

        val payload = try {
            // base64url: «-»/«_» вместо «+»/«/», хвостовые «=» опущены.
            val bytes = Base64.decode(encoded, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)
            json.decodeFromString<WidgetPayload>(bytes.decodeToString())
        } catch (_: Exception) {
            // Битый base64, не тот JSON, обрезанная строка — для вызывающего
            // это один и тот же случай «Telegram не подтвердил вход».
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
}
