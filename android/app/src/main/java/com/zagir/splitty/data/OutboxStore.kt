@file:UseSerializers(InstantSerializer::class)

package com.zagir.splitty.data

import android.util.Log
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.InstantSerializer
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.RecipientSum
import com.zagir.splitty.core.model.SplitType
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.time.Instant
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.UseSerializers
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

private const val TAG = "OutboxStore"

// Outbox офлайн-операций (фиксированный дизайн v1, паритет с iOS):
// созданные без сети расходы копятся в файле outbox.json и досылаются
// OutboxSyncer'ом по FIFO, когда сеть появляется.

/** Вид офлайн-действия. В v1 UI ставит в очередь только [CREATE]. */
@Serializable
enum class OutboxKind {
    @SerialName("create")
    CREATE,

    @SerialName("update")
    UPDATE,

    @SerialName("delete")
    DELETE,
}

/** Статус записи outbox. */
@Serializable
enum class OutboxStatus {
    /** Ждёт отправки (сеть/5xx при досылке оставляют pending). */
    @SerialName("pending")
    PENDING,

    /** Сервер отверг (HTTP 4xx): текст — в [OutboxEntry.errorMessage]. */
    @SerialName("failed")
    FAILED,
}

/**
 * Параметры расхода в записи outbox — то же правило, что у OperationBody:
 * ровно одно из полей [recipientIds]/[recipientSums] задаёт способ деления.
 */
@Serializable
data class OutboxPayload(
    val description: String,
    val sum: Long,
    val donorId: Long,
    val recipientIds: List<Long>? = null,
    val recipientSums: List<RecipientSum>? = null,
    /**
     * Позиции чека itemized-операции. Nullable c default — СТАРЫЕ outbox.json
     * на устройствах тестеров (без этого поля) обязаны читаться без потери
     * очереди. Скоуп passthrough — только offline create/local edit.
     */
    val items: List<OperationItem>? = null,
) {
    /** Способ деления по наличию полей (recipientSums → «По суммам»). */
    val splitType: SplitType
        get() = if (recipientSums != null) SplitType.BY_EXACT_AMOUNT else SplitType.EQUALLY

    /** Получатели в исходном порядке (важен для equally-деления сервера). */
    val recipientOrder: List<Long>
        get() = recipientSums?.map { it.userId } ?: recipientIds.orEmpty()

    fun toSplit(): ExpenseSplit = when {
        recipientSums != null -> ExpenseSplit.ByExactAmount(recipientSums)
        else -> ExpenseSplit.Equally(recipientIds.orEmpty())
    }

    companion object {
        fun of(
            description: String,
            sum: Long,
            donorId: Long,
            split: ExpenseSplit,
            items: List<OperationItem>? = null,
        ): OutboxPayload =
            when (split) {
                is ExpenseSplit.Equally -> OutboxPayload(
                    description = description,
                    sum = sum,
                    donorId = donorId,
                    recipientIds = split.recipientIds,
                    items = items,
                )

                is ExpenseSplit.ByExactAmount -> OutboxPayload(
                    description = description,
                    sum = sum,
                    donorId = donorId,
                    recipientSums = split.recipientSums,
                    items = items,
                )
            }
    }
}

/** Запись outbox: локальная (ещё не отправленная) операция комнаты. */
@Serializable
data class OutboxEntry(
    /**
     * UUID записи; он же — идемпотентный `clientOpId` при досылке POST
     * (повтор после потерянного ответа не создаёт дубль, см. docs/API.md).
     */
    val localId: String,
    val roomId: String,
    val kind: OutboxKind = OutboxKind.CREATE,
    val payload: OutboxPayload,
    val createdAt: Instant,
    val status: OutboxStatus = OutboxStatus.PENDING,
    /**
     * Текст ошибки сервера при [status] == FAILED. Остаётся String, а не
     * UiText: запись сериализуется на диск, и это ровно тот message, который
     * прислал бэкенд — он уже на языке пользователя.
     */
    val errorMessage: String? = null,
) {
    val isFailed: Boolean get() = status == OutboxStatus.FAILED
}

/**
 * Хранилище outbox: список записей в JSON-файле (запись атомарная — tmp +
 * ATOMIC_MOVE), наружу — [entries]-StateFlow (экран группы показывает
 * локальные операции живьём). Все операции последовательны через Mutex.
 * Порядок списка — порядок постановки (FIFO для синка).
 *
 * Не Android-зависим — покрыт юнит-тестами (файл создаётся во временной папке).
 */
class OutboxStore(
    private val file: File,
    private val json: Json,
) {
    private val mutex = Mutex()
    private var isLoaded = false

    /**
     * Файл удалось прочитать (или его ещё не было) — перезаписывать безопасно.
     *
     * Пока false, на диске лежат неотправленные расходы, которых нет в памяти:
     * первая же запись стёрла бы очередь начисто. Не читается только при ошибке
     * ввода-вывода — она может быть транзиентной, поэтому чтение повторяется
     * перед каждой записью. Битый JSON — наоборот, восстановлению не подлежит и
     * блокировать запись навсегда не должен.
     */
    private var didRead = false

    /**
     * Очередь уже стёрта логаутом в этом процессе — сливать с диском НЕЛЬЗЯ никогда.
     *
     * Одного `didRead = true` в [clear] не хватало: если сама запись файла падала,
     * persistLocked сбрасывал didRead обратно в false, и первый же add() следующего
     * аккаунта возвращал с диска очередь ПРЕДЫДУЩЕГО — OutboxSyncer отправлял чужие
     * расходы в чужие комнаты. Флаг липкий на всё время жизни процесса: после
     * логаута на диске нет ничего, что имело бы смысл восстанавливать.
     */
    private var cleared = false

    private val _entries = MutableStateFlow<List<OutboxEntry>>(emptyList())

    /** Все записи outbox в порядке постановки (FIFO). */
    val entries: StateFlow<List<OutboxEntry>> = _entries.asStateFlow()

    /** Гарантирует, что файл прочитан (первый вызов любого метода делает это сам). */
    suspend fun awaitLoaded(): Unit = mutex.withLock { ensureLoadedLocked() }

    /** Запись по localId; null — уже отправлена/удалена. */
    suspend fun entry(localId: String): OutboxEntry? = mutex.withLock {
        ensureLoadedLocked()
        _entries.value.firstOrNull { it.localId == localId }
    }

    /** Ставит запись в конец очереди. */
    suspend fun add(entry: OutboxEntry): Unit = mutex.withLock {
        ensureLoadedLocked()
        persistLocked(_entries.value + entry)
    }

    /**
     * Правка неотправленной записи: новый payload, статус сбрасывается в
     * pending (после failed пользовательская правка — повод повторить отправку).
     */
    suspend fun update(localId: String, payload: OutboxPayload): Unit = mutex.withLock {
        ensureLoadedLocked()
        persistLocked(
            _entries.value.map { entry ->
                if (entry.localId == localId) {
                    entry.copy(payload = payload, status = OutboxStatus.PENDING, errorMessage = null)
                } else {
                    entry
                }
            }
        )
    }

    /** Удаляет запись (отправлена успешно либо удалена пользователем). */
    suspend fun remove(localId: String): Unit = mutex.withLock {
        ensureLoadedLocked()
        // removed передаётся явно: удаление выражено ОТСУТСТВИЕМ записи, а ветка
        // слияния persistLocked возвращает с диска всё, чего нет в памяти. Путь
        // узкий (чтение обязано провалиться в ensureLoadedLocked и починиться в
        // persistLocked под тем же локом), но исход тяжёлый: удалённый расход
        // всё равно уходит в комнату, а отправленный — повторно.
        persistLocked(_entries.value.filterNot { it.localId == localId }, removed = setOf(localId))
    }

    /** HTTP 4xx при досылке: запись остаётся, но помечается failed с текстом. */
    suspend fun markFailed(localId: String, message: String): Unit = mutex.withLock {
        ensureLoadedLocked()
        persistLocked(
            _entries.value.map { entry ->
                if (entry.localId == localId) {
                    entry.copy(status = OutboxStatus.FAILED, errorMessage = message)
                } else {
                    entry
                }
            }
        )
    }

    /** Полная очистка (logout). */
    suspend fun clear(): Unit = mutex.withLock {
        // Без didRead=true логаут ушёл бы в ветку слияния persistLocked и вернул
        // на диск очередь ПРЕДЫДУЩЕГО аккаунта — следующий вошедший отправил бы
        // чужие расходы в свои комнаты.
        didRead = true
        cleared = true
        isLoaded = true
        // Память чистим ДО записи: если запись файла упадёт, в _entries не должна
        // остаться очередь предыдущего аккаунта — иначе следующая же успешная
        // запись вернула бы её на диск и отправила чужие расходы в свои комнаты.
        _entries.value = emptyList()
        persistLocked(emptyList())
        // Запись не удалась (persistLocked снял didRead) — на диске остался JSON
        // предыдущего аккаунта. Файл целиком удаляем: пустая очередь и отсутствие
        // файла для нас равнозначны, а чужие расходы пережить логаут не должны.
        if (!didRead) {
            withContext(Dispatchers.IO) {
                runCatching { file.delete() }.onFailure { Log.e(TAG, "outbox clear: delete failed", it) }
            }
        }
    }

    // --- Файл ---

    private suspend fun ensureLoadedLocked() {
        if (isLoaded) return
        // Только УСПЕШНОЕ чтение считается загрузкой. Раньше isLoaded взводился
        // и после null (транзиентная ошибка ввода-вывода): очередь навсегда
        // оставалась пустой в памяти — неотправленные расходы пропадали из UI, а
        // OutboxSyncer, дождавшись awaitLoaded, видел пустой список и ничего не
        // досылал при каждом восстановлении сети.
        val loaded = readFileLocked() ?: return
        // Слияние, а не замена: при неудачном чтении add() кладёт запись только в
        // память (isLoaded остаётся false). Простое присваивание списка с диска
        // выбрасывало такой офлайн-расход навсегда — он пропадал из очереди и из UI.
        val known = _entries.value.mapTo(HashSet()) { it.localId }
        _entries.value = loaded.filterNot { it.localId in known } + _entries.value
        isLoaded = true
    }

    /**
     * Читает очередь с диска. null — файл есть, но прочитать не удалось (ошибка
     * ввода-вывода): вызывающий обязан НЕ перезаписывать файл. Битый JSON даёт
     * пустой список и снимает [didRead] — такие данные не вернуть.
     */
    private suspend fun readFileLocked(): List<OutboxEntry>? = withContext(Dispatchers.IO) {
        if (!file.isFile) {
            didRead = true // очереди ещё не было — писать безопасно
            return@withContext emptyList()
        }
        val text = runCatching { file.readText() }.getOrNull() ?: return@withContext null
        didRead = true
        runCatching {
            json.decodeFromString(ListSerializer(OutboxEntry.serializer()), text)
        }.getOrDefault(emptyList())
    }

    private suspend fun persistLocked(entries: List<OutboxEntry>, removed: Set<String> = emptySet()) {
        var toWrite = entries
        if (!didRead && !cleared) {
            // Повторная попытка: ошибка чтения могла быть транзиентной. Записи с
            // диска, которых нет в памяти, возвращаются в очередь — иначе они
            // пропали бы при этой же перезаписи.
            val disk = readFileLocked()
            if (disk == null) {
                _entries.value = entries // память обновляем, файл не трогаем
                return
            }
            val known = entries.map { it.localId }.toSet() + removed
            toWrite = disk.filterNot { it.localId in known } + entries
        }
        val outEntries = toWrite
        // Диск может отказать (нет места, права, съёмное хранилище). Раньше
        // IOException летел из viewModelScope сохранения расхода — а обработчик
        // корутинных ошибок стоит только на @ApplicationScope, так что процесс
        // падал ровно в офлайн-сценарии, где сохранить иначе нельзя. Память
        // обновляем всегда: запись останется в очереди и уйдёт при синке.
        withContext(Dispatchers.IO) {
            runCatching {
                file.parentFile?.mkdirs()
                val tmp = File(file.parentFile, "${file.name}.tmp")
                tmp.writeText(json.encodeToString(ListSerializer(OutboxEntry.serializer()), outEntries))
                try {
                    Files.move(
                        tmp.toPath(),
                        file.toPath(),
                        StandardCopyOption.ATOMIC_MOVE,
                        StandardCopyOption.REPLACE_EXISTING,
                    )
                } catch (_: Exception) {
                    Files.move(tmp.toPath(), file.toPath(), StandardCopyOption.REPLACE_EXISTING)
                }
            }.onFailure {
                // Файл не перезаписан — didRead сбрасываем, чтобы следующая
                // попытка снова слила очередь с диском, а не затёрла её.
                didRead = false
                Log.e(TAG, "outbox persist failed", it)
            }
        }
        _entries.value = outEntries
    }
}
