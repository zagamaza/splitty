@file:UseSerializers(InstantSerializer::class)

package com.zagir.splitty.core.model

import com.zagir.splitty.core.money.aggregateByCurrency
import java.time.Instant
import java.time.OffsetDateTime
import java.time.format.DateTimeParseException
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.UseSerializers
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.Json

// DTO REST API — точно по контракту docs/API.md (v2 + валюты комнат).
// Деньги — целые единицы валюты (Int, копеек нет); id комнат/операций —
// hex-строки ObjectID; id пользователей — Telegram user id (Long).

/** Общий Json проекта: незнакомые поля игнорируются, null-поля не сериализуются. */
val SplittyJson: Json = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
}

/** RFC3339-даты бэкенда: "2026-07-05T12:00:00Z", допустимы доли секунды и смещение. */
object InstantSerializer : KSerializer<Instant> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("java.time.Instant", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): Instant {
        val raw = decoder.decodeString()
        return try {
            Instant.parse(raw)
        } catch (_: DateTimeParseException) {
            OffsetDateTime.parse(raw).toInstant()
        }
    }

    override fun serialize(encoder: Encoder, value: Instant) {
        encoder.encodeString(value.toString())
    }
}

/** Пользователь. */
@Serializable
data class User(
    val id: Long,
    val username: String? = null,
    val displayName: String,
)

/** Профиль текущего пользователя. */
@Serializable
data class Me(
    val id: Long,
    val username: String? = null,
    val displayName: String,
    val lang: String = "ru",
    val notificationOn: Boolean = true,
)

/** Долг: [debtor] должен [lender]'у [sum]. */
@Serializable
data class Debt(
    val debtor: User,
    val lender: User,
    val sum: Int,
)

/** Файл, прикреплённый к операции (чек/фото из Telegram). */
@Serializable
data class OperationFile(
    val type: String,
    val fileId: String,
)

/**
 * Способ деления расхода между получателями. Лениво: незнакомое значение
 * читается как [EQUALLY], а не роняет декодирование всего списка.
 */
@Serializable(with = SplitTypeSerializer::class)
enum class SplitType(val value: String) {
    /** Поровну: доли раскладывает сервер по каноническому правилу. */
    EQUALLY("equally"),

    /** Точными суммами: доли введены вручную, Σ долей == сумме операции. */
    BY_EXACT_AMOUNT("by_exact_amount"),
}

object SplitTypeSerializer : KSerializer<SplitType> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("SplitType", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): SplitType {
        val raw = decoder.decodeString()
        return SplitType.entries.firstOrNull { it.value == raw } ?: SplitType.EQUALLY
    }

    override fun serialize(encoder: Encoder, value: SplitType) {
        encoder.encodeString(value.value)
    }
}

/**
 * Получатель операции и его доля — ХРАНИМАЯ сервером: для equally —
 * канонически вычисленная, для by_exact_amount — введённая.
 */
@Serializable
data class OperationRecipient(
    val user: User,
    val sum: Int,
)

/** Операция: расход или погашение долга. */
@Serializable
data class Operation(
    val id: String,
    val description: String,
    val sum: Int,
    val isDebtRepayment: Boolean = false,
    /** Кто заплатил. */
    val donor: User,
    /** Получатели с долями; Σ долей == [sum]. */
    val recipients: List<OperationRecipient> = emptyList(),
    /** Способ деления; отсутствует у погашений. */
    val splitType: SplitType? = null,
    val createdAt: Instant,
    /** Может быть пустым или отсутствовать. */
    val files: List<OperationFile>? = null,
    /**
     * Клиентский идемпотентный ключ (localId записи outbox, см. docs/API.md
     * «Идемпотентность создания»); есть только у операций, созданных клиентами.
     */
    val clientOpId: String? = null,
) {
    val hasFiles: Boolean get() = !files.isNullOrEmpty()

    /**
     * Операция «касается» пользователя: он платил или есть в получателях
     * (фильтры «Со мной» на экране группы и «Только мои» в активности).
     */
    fun involves(userId: Long): Boolean =
        donor.id == userId || recipients.any { it.user.id == userId }

    /**
     * Доля пользователя по ХРАНИМЫМ суммам получателей (не пересчёт!);
     * null — не участвует в делении.
     */
    fun recipientSum(userId: Long): Int? =
        recipients.firstOrNull { it.user.id == userId }?.sum

    /**
     * Нетто-позиция пользователя в расходе по хранимым долям:
     * >0 — одолжил, <0 — должен, 0 — расчёт, null — не участвует.
     * Донор: одолжил = [sum] − своя доля (если сам среди получателей).
     */
    fun netPosition(userId: Long): Int? {
        val myShare = recipientSum(userId)
        return when {
            donor.id == userId -> sum - (myShare ?: 0)
            myShare != null -> -myShare
            else -> null
        }
    }
}

/**
 * Сумма в конкретной валюте. Суммы в разных валютах НЕ складываются между
 * собой — агрегируются только повалютно (см. aggregateByCurrency).
 */
@Serializable
data class CurrencySum(
    val currency: String,
    val sum: Int,
)

/** Валюта из справочника GET /currencies: код, символ и флаг для пикера. */
@Serializable
data class CurrencyInfo(
    val code: String,
    val symbol: String,
    val flag: String,
)

/** Строка списка групп. */
@Serializable
data class RoomSummary(
    val id: String,
    val name: String,
    val createdAt: Instant,
    val isArchived: Boolean = false,
    val members: List<User> = emptyList(),
    val memberCount: Int,
    /** Валюта комнаты («RUB»/«USD»/«EUR»/«IDR») — в ней все суммы комнаты. */
    val currency: String,
    /** Сумма всех расходов комнаты (без погашений). */
    val totalSpent: Int,
    /** >0 — мне должны, <0 — я должен, 0 — расчёт. */
    val myBalance: Int,
)

/** Экран группы одним запросом. */
@Serializable
data class RoomDetail(
    val id: String,
    val name: String,
    val createdAt: Instant,
    val isArchived: Boolean = false,
    val members: List<User> = emptyList(),
    /** Валюта комнаты — в ней показываются ВСЕ суммы экрана группы. */
    val currency: String,
    val totalSpent: Int,
    /** Моя доля расходов. */
    val mySpent: Int,
    val myBalance: Int,
    /** Все долги комнаты. */
    val debts: List<Debt> = emptyList(),
    /** Все операции, новые первыми. */
    val operations: List<Operation> = emptyList(),
)

/** Баланс с другом по одной группе (только ненулевые) — в валюте этой группы. */
@Serializable
data class FriendRoomBalance(
    val roomId: String,
    val roomName: String,
    /** Валюта комнаты. */
    val currency: String,
    val balance: Int,
)

/**
 * Друг и нетто-балансы с ним ПО ВАЛЮТАМ: >0 — друг должен мне, <0 — я должен.
 * Единого поля total нет: суммы разных валют не складываются.
 */
@Serializable
data class FriendBalance(
    val user: User,
    /** Нетто по каждой валюте (сервер отдаёт как есть, без сортировки). */
    val totalsByCurrency: List<CurrencySum> = emptyList(),
    val rooms: List<FriendRoomBalance> = emptyList(),
) {
    /**
     * Ненулевые итоги по убыванию |суммы| (первая валюта — «основная»);
     * пусто — полный расчёт по всем валютам.
     */
    val totals: List<CurrencySum> get() = aggregateByCurrency(totalsByCurrency)
}

/** Элемент ленты активности. */
@Serializable
data class ActivityItem(
    val roomId: String,
    val roomName: String,
    /** Валюта комнаты операции — в ней показываются суммы строки. */
    val roomCurrency: String,
    val operation: Operation,
)

// --- Статистика группы (дашборд «Итоги») ---

/** Траты одного дня; [date] — «2026-07-05» (локальная дата, не RFC3339). */
@Serializable
data class DailySum(
    val date: String,
    val sum: Int,
)

/**
 * Траты одного календарного месяца; [month] — «2026-02» (yyyy-mm).
 * Контракт: сервер шлёт ровно 6 месяцев включая текущий, по возрастанию,
 * месяцы без трат — с sum = 0 (клиент всё равно нормализует ряд).
 */
@Serializable
data class MonthlySum(
    val month: String,
    val sum: Int,
)

/** Сумма участника («Кто платил» / «Чья доля»). */
@Serializable
data class MemberSum(
    val user: User,
    val sum: Int,
)

/** Строка «Топ расходов». */
@Serializable
data class TopOperation(
    val id: String,
    val description: String,
    val sum: Int,
    val donor: User,
    val createdAt: Instant,
)

/**
 * Статистика группы GET /rooms/{id}/statistics — данные дашборда «Итоги».
 * Все суммы — в валюте комнаты [currency].
 */
@Serializable
data class Statistics(
    val currency: String,
    val totalSpent: Int,
    /** Число всех расходов комнаты за всё время (без погашений); 0 у старых серверов. */
    val operationCount: Int = 0,
    /** Потрачено за текущий календарный месяц. */
    val monthSpent: Int,
    /** Траты по дням (дни без трат сервер может опускать — клиент дополняет нулями). */
    val byDay: List<DailySum> = emptyList(),
    /** Траты по месяцам: 6 календарных месяцев включая текущий, по возрастанию. */
    val byMonth: List<MonthlySum> = emptyList(),
    /** Кто сколько заплатил (донор операций). */
    val paidByMember: List<MemberSum> = emptyList(),
    /** Чья какая доля (по хранимым долям получателей). */
    val shareByMember: List<MemberSum> = emptyList(),
    /** Топ расходов по сумме, убывание. */
    val topOperations: List<TopOperation> = emptyList(),
)

/** Ответ авторизации (/auth/code, /auth/dev). */
@Serializable
data class AuthResponse(
    val token: String,
    val user: Me,
)
