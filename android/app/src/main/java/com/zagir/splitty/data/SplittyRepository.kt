package com.zagir.splitty.data

import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.model.AddMemberBody
import com.zagir.splitty.core.model.AddMemberResponse
import com.zagir.splitty.core.model.InviteCard
import com.zagir.splitty.core.model.InviteStatus
import com.zagir.splitty.core.model.MarkSeenBody
import com.zagir.splitty.core.model.NotificationsFeed
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.core.model.AliasBody
import com.zagir.splitty.core.model.DeviceBody
import com.zagir.splitty.core.model.AuthResponse
import com.zagir.splitty.core.model.CreateRoomBody
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.GoogleLoginBody
import com.zagir.splitty.core.model.TelegramLoginBody
import com.zagir.splitty.core.model.LinkedProvidersResponse
import com.zagir.splitty.core.model.LoginProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationBody
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.PasswordLoginBody
import com.zagir.splitty.core.model.RegisterBody
import com.zagir.splitty.core.model.RepaymentBody
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SetCurrencyBody
import com.zagir.splitty.core.model.SetPasswordBody
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.model.UpdateMeBody
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.InvalidBaseUrlException
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.network.parseApiError
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.RequestBody.Companion.toRequestBody
import retrofit2.HttpException

/**
 * Результат кешируемого GET: [value] + признак [fromCache] — «сеть недоступна,
 * отдан последний успешный ответ из офлайн-кеша».
 */
data class Fetched<T>(
    val value: T,
    val fromCache: Boolean,
)

/**
 * Репозиторий поверх [SplittyApi]: единственная точка обращения ViewModel к
 * сети. Любая ошибка наружу — [ApiException] с русским сообщением
 * (для алертов — message как есть). Отмена корутины пробрасывается как
 * [CancellationException] — это НЕ ошибка, VM её молча игнорируют.
 *
 * Ключевые GET (rooms, room, friends, activity стр.1, statistics, currencies,
 * me) возвращают [Fetched] и работают через офлайн-кеш [ApiCache]: успех сети
 * обновляет кеш; транспортная ошибка (нет сети) — отдаётся кеш, если он есть,
 * иначе ошибка пробрасывается. Экраны с кешем офлайн НЕ показывают ошибку.
 *
 * После успешной мутации VM обязан звать SessionStore.noteDataChanged() —
 * репозиторий версию данных не трогает.
 */
@Singleton
class SplittyRepository @Inject constructor(
    private val api: SplittyApi,
    private val parseApi: ParseApi,
    private val json: Json,
    private val cache: ApiCache,
) {

    // --- Auth ---

    /** Вход через Telegram: payload виджета обменивается на сессию. */
    suspend fun loginWithTelegram(payload: TelegramLoginBody): AuthResponse =
        call { api.loginWithTelegram(payload) }

    /** Вход через Google: id-токен из Credential Manager обменивается на сессию. */
    suspend fun loginWithGoogle(idToken: String): AuthResponse =
        call { api.loginWithGoogle(GoogleLoginBody(idToken)) }

    /** Регистрация по email и паролю; 409 `email_taken` — адрес занят. */
    suspend fun register(email: String, password: String, displayName: String): AuthResponse =
        call { api.register(RegisterBody(email = email, password = password, displayName = displayName)) }

    /** Вход по email и паролю; 401 `invalid_credentials` на любую неудачу. */
    suspend fun loginWithPassword(email: String, password: String): AuthResponse =
        call { api.loginWithPassword(PasswordLoginBody(email = email, password = password)) }

    // --- Профиль ---

    suspend fun me(): Fetched<Me> =
        cached(ApiCache.Keys.ME, Me.serializer()) { api.me() }

    suspend fun updateMe(
        displayName: String? = null,
        lang: String? = null,
        notificationOn: Boolean? = null,
    ): Me = call {
        api.updateMe(UpdateMeBody(displayName = displayName, lang = lang, notificationOn = notificationOn))
    }

    // --- Способы входа и удаление аккаунта ---

    /** Привязать Google к текущему аккаунту; ответ — профиль с новым списком. */
    suspend fun linkGoogle(idToken: String): LinkedProvidersResponse =
        call { api.linkGoogle(GoogleLoginBody(idToken)) }

    /** Задать или сменить пароль; [current] нужен, только если пароль уже был. */
    suspend fun setPassword(current: String?, new: String): LinkedProvidersResponse =
        call { api.setPassword(SetPasswordBody(currentPassword = current, newPassword = new)) }

    /** Отвязать способ входа; ответ несёт профиль и (для telegram) warning. */
    suspend fun unlinkProvider(provider: LoginProvider): LinkedProvidersResponse =
        call { api.unlinkProvider(provider.id) }

    /**
     * Удалить аккаунт. Кеш чистит не репозиторий, а [OfflineDataCleaner] по
     * пропаже токена — здесь только запрос, чтобы «удалили на сервере» и
     * «стёрли с устройства» не расходились при сетевой ошибке.
     */
    suspend fun deleteAccount() = call { api.deleteAccount() }

    // --- Комнаты (группы) ---

    suspend fun rooms(archived: Boolean = false): Fetched<List<RoomSummary>> =
        cached(ApiCache.Keys.rooms(archived), ListSerializer(RoomSummary.serializer())) {
            api.rooms(archived)
        }

    suspend fun createRoom(name: String): RoomDetail = call { api.createRoom(CreateRoomBody(name)) }

    suspend fun room(roomId: String): Fetched<RoomDetail> =
        cached(ApiCache.Keys.room(roomId), RoomDetail.serializer()) { api.room(roomId) }

    suspend fun joinRoom(roomId: String): RoomDetail = call { api.joinRoom(roomId) }

    suspend fun archiveRoom(roomId: String) = call { api.archiveRoom(roomId) }

    suspend fun unarchiveRoom(roomId: String) = call { api.unarchiveRoom(roomId) }

    suspend fun setRoomCurrency(roomId: String, currency: String) =
        call { api.setRoomCurrency(roomId, SetCurrencyBody(currency)) }

    // --- Валюты ---

    suspend fun currencies(): Fetched<List<CurrencyInfo>> =
        cached(ApiCache.Keys.CURRENCIES, ListSerializer(CurrencyInfo.serializer())) {
            api.currencies()
        }

    // --- Операции ---

    suspend fun operations(roomId: String, type: String = "all"): List<Operation> =
        call { api.operations(roomId, type) }

    /**
     * POST операции. [clientOpId] — идемпотентный ключ (localId записи outbox):
     * повтор с тем же ключом вернёт существующую операцию вместо дубля.
     */
    suspend fun addOperation(
        roomId: String,
        description: String,
        sum: Long,
        donorId: Long,
        split: ExpenseSplit,
        items: List<OperationItem>? = null,
        clientOpId: String? = null,
    ): Operation = call {
        api.addOperation(roomId, OperationBody.of(description, sum, donorId, split, items, clientOpId))
    }

    /**
     * PUT операции. [items] — позиции чека itemized-операции: проносятся в тело
     * НЕТРОНУТЫМИ (passthrough), иначе плоский PUT затрёт чек на сервере.
     * У обычных расходов null.
     *
     * [version] — версия расхода, с которой человек открывал правку: сервер
     * отклонит запись, если расход успели изменить (409 stale_operation).
     * null — правка идёт безусловно (офлайн-очередь).
     */
    suspend fun updateOperation(
        roomId: String,
        operationId: String,
        description: String,
        sum: Long,
        donorId: Long,
        split: ExpenseSplit,
        items: List<OperationItem>? = null,
        version: Int? = null,
    ): Operation = call {
        api.updateOperation(
            roomId,
            operationId,
            OperationBody.of(description, sum, donorId, split, items, version = version),
        )
    }

    suspend fun deleteOperation(roomId: String, operationId: String) =
        call { api.deleteOperation(roomId, operationId) }

    // --- AI-распознавание расхода ---

    /**
     * POST /rooms/{id}/operations/parse (multipart) — распознаёт расход из
     * фото/аудио/текста в черновик. Ничего не создаёт: чистая функция «ввод →
     * черновик». Части (draft/text/audio/image), имена и MIME собираются вручную
     * как в iOS APIClient. Идёт через [parseApi] с таймаутом 90с. Коды сервера
     * (413/415/429/503) прилетают как [ApiException] с русским message —
     * humanErrorText показывает его как есть.
     *
     * [draft] — текущий черновик формы (для голосовой правки Task 12); при первом
     * распознавании фото обычно null. Хотя бы одно из audio/image/text обязано
     * быть непустым (иначе сервер вернёт 400 «нужно передать audio, image или text»).
     */
    suspend fun parseOperation(
        roomId: String,
        audio: ByteArray? = null,
        image: ByteArray? = null,
        text: String? = null,
        draft: ParseDraft? = null,
    ): ParseResponse = call {
        val builder = MultipartBody.Builder().setType(MultipartBody.FORM)
        draft?.let {
            // Поле формы (не файл): сервер читает его через r.FormValue("draft").
            builder.addFormDataPart("draft", json.encodeToString(ParseDraft.serializer(), it))
        }
        audio?.let {
            builder.addFormDataPart("audio", "audio.wav", it.toRequestBody("audio/wav".toMediaType()))
        }
        image?.let {
            builder.addFormDataPart("image", "image.jpg", it.toRequestBody("image/jpeg".toMediaType()))
        }
        text?.takeIf { it.isNotBlank() }?.let {
            builder.addFormDataPart("text", it)
        }
        parseApi.parse(roomId, builder.build())
    }

    /**
     * POST /users/{id}/aliases — дозапись прозвища участнику после сопоставления
     * нераспознанного имени. Best-effort: провал (403 «нет общей комнаты», сеть)
     * не рушит поток сохранения расхода — возвращаем false, наверх не бросаем.
     * Отмена корутины ([CancellationException]) — не ошибка, пробрасывается.
     */
    suspend fun addAlias(userId: Long, alias: String): Boolean = try {
        call { api.addAlias(userId, AliasBody(alias)) }
        true
    } catch (e: ApiException) {
        false
    }

    // --- Долги и погашение ---

    suspend fun debts(roomId: String, involving: String = "all"): List<Debt> =
        call { api.debts(roomId, involving) }

    suspend fun repay(
        roomId: String,
        debtorId: Long,
        lenderId: Long,
        sum: Long,
        clientOpId: String,
    ): Operation =
        call { api.repay(roomId, RepaymentBody(debtorId = debtorId, lenderId = lenderId, sum = sum, clientOpId = clientOpId)) }

    // --- Друзья, активность, статистика ---

    suspend fun friends(): Fetched<List<FriendBalance>> =
        cached(ApiCache.Keys.FRIENDS, ListSerializer(FriendBalance.serializer())) {
            api.friends()
        }

    /** Лента активности; офлайн-кеш — только у первой страницы (offset == 0). */
    suspend fun activity(limit: Int = 30, offset: Int = 0): Fetched<List<ActivityItem>> =
        if (offset == 0) {
            cached(ApiCache.Keys.ACTIVITY_FIRST_PAGE, ListSerializer(ActivityItem.serializer())) {
                api.activity(limit, 0)
            }
        } else {
            Fetched(call { api.activity(limit, offset) }, fromCache = false)
        }

    /**
     * Раздел «Уведомления»: приглашения + лента + счётчик.
     * Кешируется как и лента активности — офлайн раздел открывается мгновенно.
     */
    suspend fun notificationFeed(limit: Int = 30, offset: Int = 0): Fetched<NotificationsFeed> =
        if (offset == 0) {
            cached(ApiCache.Keys.NOTIFICATIONS_FIRST_PAGE, NotificationsFeed.serializer()) {
                api.notificationFeed(limit, 0)
            }
        } else {
            Fetched(call { api.notificationFeed(limit, offset) }, fromCache = false)
        }

    /**
     * Только счётчик непрочитанного — для бейджа на табе.
     *
     * Мимо кеша намеренно: путь `notificationFeed(offset = 0)` записывает ответ
     * в кеш первой страницы, и запрос на одну строку подменил бы собой всю
     * закешированную ленту раздела — офлайн там осталась бы ровно одна запись.
     */
    suspend fun unreadNotificationCount(): Int = call { api.notificationFeed(1, 0) }.unreadCount

    suspend fun markNotificationsSeen(through: java.time.Instant) =
        call { api.markNotificationsSeen(MarkSeenBody(through)) }

    /** Отметить прочитанной ОДНУ группу — гасит счётчик на её карточке. */
    suspend fun markRoomSeen(roomId: String, through: java.time.Instant) =
        call { api.markRoomSeen(roomId, MarkSeenBody(through)) }

    suspend fun addMember(roomId: String, userId: Long): InviteStatus =
        call { api.addMember(roomId, AddMemberBody(userId)) }.status

    suspend fun leaveRoom(roomId: String) = call { api.leaveRoom(roomId) }

    suspend fun removeMember(roomId: String, userId: Long) = call { api.removeMember(roomId, userId) }

    suspend fun acceptInvite(roomId: String) = call { api.acceptInvite(roomId) }

    suspend fun declineInvite(roomId: String) = call { api.declineInvite(roomId) }

    suspend fun statistics(roomId: String): Fetched<Statistics> =
        cached(ApiCache.Keys.statistics(roomId), Statistics.serializer()) {
            api.statistics(roomId)
        }

    // --- Файлы ---

    /** Привязать FCM-токен устройства (native-пуши). */
    suspend fun registerDevice(token: String) = call { api.registerDevice(DeviceBody(token)) }

    /** Отвязать FCM-токен (logout). */
    suspend fun unregisterDevice(token: String) = call { api.unregisterDevice(DeviceBody(token)) }

    /** Эффективные настройки уведомлений. */
    suspend fun notifications(): NotifySettings = call { api.notifications() }

    /** Обновляет настройки уведомлений; ответ — новые значения. */
    suspend fun updateNotifications(settings: NotifySettings): NotifySettings =
        call { api.updateNotifications(settings) }

    /** Скачивает вложение операции (чек/фото/видео) целиком в память. */
    suspend fun fileData(fileId: String): ByteArray =
        call { api.file(fileId).use { it.bytes() } }

    /** Фото профиля Telegram; ApiException(404) — фото нет. */
    suspend fun userAvatar(userId: Long): ByteArray =
        call { api.userAvatar(userId).use { it.bytes() } }

    // --- Кеш и маппинг ошибок ---

    /**
     * Сеть → успех: обновить кеш и вернуть свежее; транспортная ошибка
     * (IOException — нет сети/таймаут) → последний успешный ответ из кеша,
     * если он есть. Остальные ошибки (HTTP 4xx/5xx) кешем не гасятся.
     */
    private suspend fun <T> cached(
        key: String,
        serializer: KSerializer<T>,
        fetch: suspend () -> T,
    ): Fetched<T> = try {
        val fresh = call(fetch)
        cache.write(key, serializer, fresh)
        Fetched(fresh, fromCache = false)
    } catch (e: ApiException) {
        if (e.code == ApiException.CODE_TRANSPORT) {
            cache.read(key, serializer)?.let { return Fetched(it, fromCache = true) }
        }
        throw e
    }

    private suspend fun <T> call(block: suspend () -> T): T = try {
        block()
    } catch (e: CancellationException) {
        throw e // отмена — не ошибка
    } catch (e: ApiException) {
        throw e
    } catch (e: HttpException) {
        throw parseApiError(e.code(), e.response()?.errorBody()?.string(), json)
    } catch (e: InvalidBaseUrlException) {
        throw ApiException(null, ApiException.CODE_INVALID_URL, "invalid base url", cause = e)
    } catch (e: SerializationException) {
        throw ApiException(null, ApiException.CODE_DECODING, "decoding failed", cause = e)
    } catch (e: IOException) {
        throw ApiException(null, ApiException.CODE_TRANSPORT, "transport failure", cause = e)
    }
}
