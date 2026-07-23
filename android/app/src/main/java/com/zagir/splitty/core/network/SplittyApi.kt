package com.zagir.splitty.core.network

import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.model.AliasBody
import com.zagir.splitty.core.model.AuthResponse
import com.zagir.splitty.core.model.CodeLoginBody
import com.zagir.splitty.core.model.DeviceBody
import com.zagir.splitty.core.model.CreateRoomBody
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.DevLoginBody
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationBody
import com.zagir.splitty.core.model.RepaymentBody
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SetCurrencyBody
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.model.UpdateMeBody
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

    @POST("api/v1/auth/code")
    suspend fun loginWithCode(@Body body: CodeLoginBody): AuthResponse

    @POST("api/v1/auth/dev")
    suspend fun loginDev(@Body body: DevLoginBody): AuthResponse

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

    @GET("api/v1/activity")
    suspend fun activity(
        @Query("limit") limit: Int,
        @Query("offset") offset: Int,
    ): List<ActivityItem>

    @GET("api/v1/rooms/{roomId}/statistics")
    suspend fun statistics(@Path("roomId") roomId: String): Statistics

    // --- Файлы (чеки/фото из Telegram) ---

    @Streaming
    @GET("api/v1/files/{fileId}")
    suspend fun file(@Path("fileId") fileId: String): ResponseBody

    /** Фото профиля Telegram; 404 — фото нет или скрыто приватностью. */
    @GET("api/v1/users/{userId}/avatar")
    suspend fun userAvatar(@Path("userId") userId: Long): ResponseBody
}
