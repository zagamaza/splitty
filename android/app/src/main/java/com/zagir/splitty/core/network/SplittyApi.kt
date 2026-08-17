package com.zagir.splitty.core.network

import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.model.AddMemberBody
import com.zagir.splitty.core.model.AddMemberResponse
import com.zagir.splitty.core.model.InviteCard
import com.zagir.splitty.core.model.InviteStatus
import com.zagir.splitty.core.model.MarkSeenBody
import com.zagir.splitty.core.model.NotificationsFeed
import com.zagir.splitty.core.model.AliasBody
import com.zagir.splitty.core.model.AuthResponse
import com.zagir.splitty.core.model.DeviceBody
import com.zagir.splitty.core.model.CreateRoomBody
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.GoogleLoginBody
import com.zagir.splitty.core.model.TelegramLoginBody
import com.zagir.splitty.core.model.LinkedProvidersResponse
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationBody
import com.zagir.splitty.core.model.PasswordLoginBody
import com.zagir.splitty.core.model.RegisterBody
import com.zagir.splitty.core.model.RepaymentBody
import com.zagir.splitty.core.model.RoomAvatarResponse
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SetCurrencyBody
import com.zagir.splitty.core.model.SetPasswordBody
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.model.UpdateMeBody
import okhttp3.MultipartBody
import okhttp3.ResponseBody
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.HTTP
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.PUT
import retrofit2.http.Path
import retrofit2.http.Query
import retrofit2.http.Streaming

/**
 * Retrofit-интерфейс всех эндпоинтов REST API (контракт docs/API.md, /api/v1).
 * Пути ОТНОСИТЕЛЬНЫЕ: реальный хост подставляет [BaseUrlInterceptor]
 * (адрес сервера пользователь меняет на экране входа), Bearer-токен —
 * [AuthInterceptor]. Ошибки не-2xx мапятся в [ApiException] в репозитории.
 */
interface SplittyApi {

    // --- Auth (без Bearer-заголовка) ---

    /** Вход через Telegram Login Widget (веб-поток, без ухода в Telegram). */
    @POST("api/v1/auth/telegram")
    suspend fun loginWithTelegram(@Body body: TelegramLoginBody): AuthResponse

    /** Вход через Google: id-токен от Credential Manager (Task 18). */
    @POST("api/v1/auth/google")
    suspend fun loginWithGoogle(@Body body: GoogleLoginBody): AuthResponse

    /** Регистрация по email и паролю; 409 `email_taken` — адрес занят. */
    @POST("api/v1/auth/register")
    suspend fun register(@Body body: RegisterBody): AuthResponse

    /**
     * Вход по email и паролю. 401 `invalid_credentials` одинаков для неверного
     * пароля и незнакомого адреса — сервер намеренно не даёт проверить, есть ли
     * такая регистрация.
     */
    @POST("api/v1/auth/login")
    suspend fun loginWithPassword(@Body body: PasswordLoginBody): AuthResponse

    // --- Профиль ---

    @GET("api/v1/me")
    suspend fun me(): Me

    /** Эффективные настройки уведомлений (категория × канал). */
    @GET("api/v1/me/notifications")
    suspend fun notifications(): NotifySettings

    /** Частичное обновление настроек уведомлений; ответ — новые значения. */
    @PATCH("api/v1/me/notifications")
    suspend fun updateNotifications(@Body settings: NotifySettings): NotifySettings

    /** Привязать FCM-токен устройства (native-пуши). Идемпотентно; 204. */
    @POST("api/v1/me/devices")
    suspend fun registerDevice(@Body body: DeviceBody)

    /** Отвязать FCM-токен (logout). DELETE с телом — через @HTTP. 204. */
    @HTTP(method = "DELETE", path = "api/v1/me/devices", hasBody = true)
    suspend fun unregisterDevice(@Body body: DeviceBody)

    @PATCH("api/v1/me")
    suspend fun updateMe(@Body body: UpdateMeBody): Me

    // --- Способы входа (привязка/отвязка) ---

    /**
     * POST /me/link/google — привязать Google к ТЕКУЩЕМУ аккаунту (кто
     * «текущий», решает Bearer-токен, а не тело запроса). Повтор с той же
     * личностью — 200 (идемпотентно); 409 `identity_taken` — личность уже
     * принадлежит другому профилю Splitty (слияние профилей вне объёма).
     *
     * Тело то же, что у входа, — [GoogleLoginBody]: и `/auth/google`, и
     * `/me/link/google` читают на сервере один и тот же `{"idToken": …}`.
     */
    @POST("api/v1/me/link/google")
    suspend fun linkGoogle(@Body body: GoogleLoginBody): LinkedProvidersResponse

    /**
     * POST /me/password — задать или сменить пароль. Текущий обязателен, только
     * если он уже есть; 403 `invalid_password` — не сошёлся (не 401: сессия
     * жива, и разлогинивать по нему нельзя).
     */
    @POST("api/v1/me/password")
    suspend fun setPassword(@Body body: SetPasswordBody): LinkedProvidersResponse

    /**
     * DELETE /me/link/{provider} — отвязать способ входа.
     * 409 `last_identity` — это последний способ войти, отвязывать нельзя.
     * Ответ по telegram несёт `warning`, который клиент обязан показать.
     */
    @DELETE("api/v1/me/link/{provider}")
    suspend fun unlinkProvider(@Path("provider") provider: String): LinkedProvidersResponse

    /**
     * DELETE /me — удаление аккаунта (требование и Apple Guideline 5.1.1(v),
     * и Google Play). 204 без тела; 403 — демо-аккаунт ревьюеров.
     */
    @DELETE("api/v1/me")
    suspend fun deleteAccount()

    /**
     * «Выйти на всех устройствах»: сервер перестаёт принимать все ранее выданные
     * токены, включая текущий, и снимает push-токены устройств.
     */
    @POST("api/v1/me/revoke-tokens")
    suspend fun revokeTokens()

    // --- Комнаты (группы) ---

    @GET("api/v1/rooms")
    suspend fun rooms(@Query("archived") archived: Boolean): List<RoomSummary>

    @POST("api/v1/rooms")
    suspend fun createRoom(@Body body: CreateRoomBody): RoomDetail

    @GET("api/v1/rooms/{roomId}")
    suspend fun room(@Path("roomId") roomId: String): RoomDetail

    @POST("api/v1/rooms/{roomId}/join")
    suspend fun joinRoom(@Path("roomId") roomId: String): RoomDetail

    @POST("api/v1/rooms/{roomId}/archive")
    suspend fun archiveRoom(@Path("roomId") roomId: String)

    @POST("api/v1/rooms/{roomId}/unarchive")
    suspend fun unarchiveRoom(@Path("roomId") roomId: String)

    @PUT("api/v1/rooms/{roomId}/currency")
    suspend fun setRoomCurrency(
        @Path("roomId") roomId: String,
        @Body body: SetCurrencyBody,
    )

    // --- Валюты ---

    @GET("api/v1/currencies")
    suspend fun currencies(): List<CurrencyInfo>

    // --- Операции (расходы) ---

    @GET("api/v1/rooms/{roomId}/operations")
    suspend fun operations(
        @Path("roomId") roomId: String,
        @Query("type") type: String,
    ): List<Operation>

    @POST("api/v1/rooms/{roomId}/operations")
    suspend fun addOperation(
        @Path("roomId") roomId: String,
        @Body body: OperationBody,
    ): Operation

    @PUT("api/v1/rooms/{roomId}/operations/{operationId}")
    suspend fun updateOperation(
        @Path("roomId") roomId: String,
        @Path("operationId") operationId: String,
        @Body body: OperationBody,
    ): Operation

    @DELETE("api/v1/rooms/{roomId}/operations/{operationId}")
    suspend fun deleteOperation(
        @Path("roomId") roomId: String,
        @Path("operationId") operationId: String,
    )

    /**
     * Дозапись прозвища участнику после сопоставления нераспознанного имени
     * AI-распознавания. Best-effort: ответ 204 без тела, ошибки не критичны.
     */
    @POST("api/v1/users/{userId}/aliases")
    suspend fun addAlias(@Path("userId") userId: Long, @Body body: AliasBody)

    // --- Долги и погашение (settle up) ---

    @GET("api/v1/rooms/{roomId}/debts")
    suspend fun debts(
        @Path("roomId") roomId: String,
        @Query("involving") involving: String,
    ): List<Debt>

    @POST("api/v1/rooms/{roomId}/repayments")
    suspend fun repay(
        @Path("roomId") roomId: String,
        @Body body: RepaymentBody,
    ): Operation

    // --- Друзья, активность, статистика ---

    @GET("api/v1/friends")
    suspend fun friends(): List<FriendBalance>

    /**
     * Раздел «Уведомления»: приглашения + лента + счётчик.
     *
     * Имя намеренно НЕ `notifications()` — так уже называются настройки
     * уведомлений (см. выше), и совпадение имён было бы ловушкой.
     */
    @GET("api/v1/notifications")
    suspend fun notificationFeed(
        @Query("limit") limit: Int,
        @Query("offset") offset: Int,
    ): NotificationsFeed

    @POST("api/v1/me/notifications-seen")
    suspend fun markNotificationsSeen(@Body body: MarkSeenBody)

    /**
     * Отметить прочитанной ОДНУ группу — гасит счётчик на её карточке.
     * Отдельно от глобальной отметки: раздел «Уведомления» счётчики групп не
     * гасит, иначе их почти никто не успевал бы увидеть.
     */
    @POST("api/v1/rooms/{roomId}/notifications-seen")
    suspend fun markRoomSeen(
        @Path("roomId") roomId: String,
        @Body body: MarkSeenBody,
    )

    @POST("api/v1/rooms/{roomId}/members")
    suspend fun addMember(
        @Path("roomId") roomId: String,
        @Body body: AddMemberBody,
    ): AddMemberResponse

    @DELETE("api/v1/rooms/{roomId}/members/me")
    suspend fun leaveRoom(@Path("roomId") roomId: String)

    @DELETE("api/v1/rooms/{roomId}/members/{userId}")
    suspend fun removeMember(
        @Path("roomId") roomId: String,
        @Path("userId") userId: Long,
    )

    @POST("api/v1/invites/{roomId}/accept")
    suspend fun acceptInvite(@Path("roomId") roomId: String)

    @POST("api/v1/invites/{roomId}/decline")
    suspend fun declineInvite(@Path("roomId") roomId: String)

    @GET("api/v1/activity")
    suspend fun activity(
        @Query("limit") limit: Int,
        @Query("offset") offset: Int,
    ): List<ActivityItem>

    @GET("api/v1/rooms/{roomId}/statistics")
    suspend fun statistics(@Path("roomId") roomId: String): Statistics

    // --- Фото группы ---

    /**
     * Загрузка фото группы. Тело собирается в репозитории: сервер ищет часть с
     * именем `image` и проверяет тип ПО СИГНАТУРЕ файла, а не по заголовку.
     */
    @PUT("api/v1/rooms/{roomId}/avatar")
    suspend fun setRoomAvatar(
        @Path("roomId") roomId: String,
        @Body body: MultipartBody,
    ): RoomAvatarResponse

    @DELETE("api/v1/rooms/{roomId}/avatar")
    suspend fun deleteRoomAvatar(@Path("roomId") roomId: String)

    // --- Файлы (своё хранилище и вложения из Telegram) ---

    @Streaming
    @GET("api/v1/files/{fileId}")
    suspend fun file(@Path("fileId") fileId: String): ResponseBody

    /** Фото профиля Telegram; 404 — фото нет или скрыто приватностью. */
    @GET("api/v1/users/{userId}/avatar")
    suspend fun userAvatar(@Path("userId") userId: Long): ResponseBody
}
