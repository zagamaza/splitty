@file:UseSerializers(InstantSerializer::class)

package com.zagir.splitty.core.model

import com.zagir.splitty.core.money.aggregateByCurrency
import java.time.Instant
import java.time.OffsetDateTime
import java.time.format.DateTimeParseException
import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
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
            // фолбэк бросает тот же DateTimeParseException — он не ловится
            // репозиторием (там только SerializationException/IOException),
            // поэтому переупаковываем в ошибку разбора ответа
            try {
                OffsetDateTime.parse(raw).toInstant()
            } catch (e: DateTimeParseException) {
                throw SerializationException("невалидная дата в ответе сервера: \"$raw\"", e)
            }
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

/** Каналы уведомлений одной категории. */
@Serializable
data class ChannelPrefs(
    val telegram: Boolean = true,
    val push: Boolean = false,
)

/**
 * Настройки уведомлений: категория событий × канал доставки
 * (GET/PATCH /me/notifications, сервер отдаёт эффективные значения).
 */
@Serializable
data class NotifySettings(
    val operations: ChannelPrefs = ChannelPrefs(),
    val debts: ChannelPrefs = ChannelPrefs(),
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

/**
 * Доля участника в позиции чека (itemized-операция, AI-распознавание).
 * ЧИСТЫЙ транспорт (декод/энкод) — логика раскладки (веса/фиксы) появится
 * отдельным файлом в Task 6; здесь без вычислений. userId — Telegram id (Long).
 */
@Serializable
data class ItemShare(
    val userId: Long,
    /**
     * Относительный вес доли (1 = поровну); сервер игнорирует при заданном [amount].
     *
     * [EncodeDefault] обязателен: без него kotlinx выбрасывает `weight: 1` из тела,
     * сервер видит нулевые веса и либо возвращает 400 («цена позиции не распределена
     * полностью»), либо при смешанных весах молча считает деление иначе, чем показал
     * предпросмотр.
     */
    @OptIn(ExperimentalSerializationApi::class)
    @EncodeDefault(EncodeDefault.Mode.ALWAYS)
    val weight: Int = 1,
    /** Фиксированная сумма участника (целые единицы валюты); null — доля по весу. */
    val amount: Int? = null,
)

/**
 * Позиция чека itemized-операции: что заказали, почём и как делится. Единый
 * транспортный вид (read-модель операции и write-path [OperationBody.items]) —
 * совпадает с серверным `ai.DraftItem`. Только декод/энкод, без логики долей.
 */
@Serializable
data class OperationItem(
    /** Название позиции («Пицца», «Сервисный сбор»). */
    val name: String,
    /** ВСЕГДА суммарная стоимость строки (целые единицы, уже с учётом [qty]). */
    val price: Int,
    /**
     * Количество — только для показа («×10»); в делении НЕ участвует.
     *
     * [EncodeDefault] обязателен (см. [ItemShare.weight]): без него kotlinx
     * выбрасывает `qty: 1` из тела, а сервер копирует поле без доопределения —
     * и в базу навсегда уходит `qty: 0`.
     */
    @OptIn(ExperimentalSerializationApi::class)
    @EncodeDefault(EncodeDefault.Mode.ALWAYS)
    val qty: Int = 1,
    /** Доли участников; null/пусто у надбавок (делятся по базе). */
    val shares: List<ItemShare>? = null,
    /**
     * «item» — обычная позиция, «surcharge» — надбавка (сбор/чаевые/доставка).
     *
     * [EncodeDefault] обязателен (см. [ItemShare.weight]): обычной позиции kind
     * нигде не присваивается явно, без аннотации поле выпадает из тела, сервер
     * видит "" и с валидацией kind отвечает 400 на КАЖДОЕ сохранение чека.
     */
    @OptIn(ExperimentalSerializationApi::class)
    @EncodeDefault(EncodeDefault.Mode.ALWAYS)
    val kind: String = KIND_ITEM,
    /** Правило деления надбавки «proportional»|«equally»; null у обычных позиций. */
    val split: String? = null,
    /** Процент надбавки — только для показа («Сбор 10%»); в расчёте НЕ участвует. */
    val percent: Int? = null,
    /**
     * Только в черновике parse: нераспознанные имена для сопоставления
     * участнику. В read-модели операции всегда null.
     */
    val unknown: List<String>? = null,
) {
    companion object {
        const val KIND_ITEM = "item"
        const val KIND_SURCHARGE = "surcharge"
        const val SPLIT_PROPORTIONAL = "proportional"
        const val SPLIT_EQUALLY = "equally"
    }
}

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
     * Позиции чека itemized-операции (AI-распознавание); null у обычных операций.
     * Плоские [recipients] — источник для долгов; [items] — «зачем так вышло».
     * Правка такой операции плоским PUT затирает чек — до Task 10 она запрещена.
     */
    val items: List<OperationItem>? = null,
    /**
     * Клиентский идемпотентный ключ (localId записи outbox, см. docs/API.md
     * «Идемпотентность создания»); есть только у операций, созданных клиентами.
     */
    val clientOpId: String? = null,
) {
    val hasFiles: Boolean get() = !files.isNullOrEmpty()

    /** Позиции чека без опциональности (зеркало iOS `operation.itemList`). */
    val itemList: List<OperationItem> get() = items ?: emptyList()

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
 * Черновик расхода из AI-распознавания (`POST /rooms/{id}/operations/parse`).
 * Клиент шлёт текущий черновик на голосовую правку — сервер возвращает новый.
 * Порт iOS [ParseDraft]. Только транспорт: логика долей — в [OperationItems].
 */
@Serializable
data class ParseDraft(
    val description: String,
    val sum: Int,
    /** Кто платил; null — модель не определила донора. */
    val donorId: Long? = null,
    /** Позиции чека; item с непустым `unknown` требует сопоставления перед сохранением. */
    val items: List<OperationItem>? = null,
) {
    /** Позиции без опциональности. */
    val itemList: List<OperationItem> get() = items ?: emptyList()

    /** Есть ли нераспознанные имена хотя бы в одной позиции (блокирует сохранение). */
    val hasUnknown: Boolean get() = itemList.any { !it.unknown.isNullOrEmpty() }
}

/**
 * Ответ распознавания: обновлённый черновик и опциональные уточняющие вопросы
 * модели («кто платил?»). Порт iOS [ParseResponse].
 */
@Serializable
data class ParseResponse(
    val draft: ParseDraft,
    /** Уточняющие вопросы модели; null/пусто — вопросов нет. */
    val questions: List<String>? = null,
) {
    /** Вопросы без опциональности. */
    val questionList: List<String> get() = questions ?: emptyList()
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
    /**
     * Долги группы неисчислимы (старые данные бота: доли не сходятся). Сервер
     * шлёт `debtsUnavailable: true` (omitempty → у здоровых комнат ключа нет,
     * поэтому default false), при этом `debts=[]` и `myBalance=0`. Клиент
     * показывает бейдж «долги не считаются» вместо ложного «все в расчёте».
     */
    val debtsUnavailable: Boolean = false,
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
