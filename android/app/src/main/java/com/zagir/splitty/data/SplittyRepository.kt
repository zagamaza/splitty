package com.zagir.splitty.data

import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.model.AuthResponse
import com.zagir.splitty.core.model.CodeLoginBody
import com.zagir.splitty.core.model.CreateRoomBody
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.DevLoginBody
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationBody
import com.zagir.splitty.core.model.RepaymentBody
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SetCurrencyBody
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.model.UpdateMeBody
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.InvalidBaseUrlException
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
    private val json: Json,
    private val cache: ApiCache,
) {

    // --- Auth ---

    suspend fun loginWithCode(code: String): AuthResponse =
        call { api.loginWithCode(CodeLoginBody(code)) }

    suspend fun loginDev(userId: Long, displayName: String, username: String?): AuthResponse =
        call { api.loginDev(DevLoginBody(userId = userId, displayName = displayName, username = username)) }

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
        sum: Int,
        donorId: Long,
        split: ExpenseSplit,
        clientOpId: String? = null,
    ): Operation = call {
        api.addOperation(roomId, OperationBody.of(description, sum, donorId, split, clientOpId))
    }

    suspend fun updateOperation(
        roomId: String,
        operationId: String,
        description: String,
        sum: Int,
        donorId: Long,
        split: ExpenseSplit,
    ): Operation = call {
        api.updateOperation(roomId, operationId, OperationBody.of(description, sum, donorId, split))
    }

    suspend fun deleteOperation(roomId: String, operationId: String) =
        call { api.deleteOperation(roomId, operationId) }

    // --- Долги и погашение ---

    suspend fun debts(roomId: String, involving: String = "all"): List<Debt> =
        call { api.debts(roomId, involving) }

    suspend fun repay(roomId: String, debtorId: Long, lenderId: Long, sum: Int): Operation =
        call { api.repay(roomId, RepaymentBody(debtorId = debtorId, lenderId = lenderId, sum = sum)) }

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

    suspend fun statistics(roomId: String): Fetched<Statistics> =
        cached(ApiCache.Keys.statistics(roomId), Statistics.serializer()) {
            api.statistics(roomId)
        }

    // --- Файлы ---

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
        throw ApiException(null, ApiException.CODE_INVALID_URL, "Некорректный адрес сервера", e)
    } catch (e: SerializationException) {
        throw ApiException(null, ApiException.CODE_DECODING, "Не удалось обработать ответ сервера", e)
    } catch (e: IOException) {
        throw ApiException(null, ApiException.CODE_TRANSPORT, "Нет соединения с сервером", e)
    }
}
