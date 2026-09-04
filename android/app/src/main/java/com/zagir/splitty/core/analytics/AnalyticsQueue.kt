package com.zagir.splitty.core.analytics

import android.util.Log
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

private const val TAG = "AnalyticsQueue"

/** Одна запись очереди — ровно то, что уедет на сервер. */
@Serializable
data class AnalyticsRecord(
    val id: String,
    val name: String,
    val at: String,
    val session: String,
    val platform: String = "android",
    val appVersion: String,
    val locale: String,
    val params: Map<String, String> = emptyMap(),
    /**
     * Кому принадлежит запись.
     *
     * У очереди расходов такого поля нет, и смену аккаунта там разгребает
     * асинхронный [com.zagir.splitty.data.OfflineDataCleaner]. Событиям этого
     * мало: разбор идёт до первой отправки, а не когда-нибудь потом, иначе
     * гонка отдаст события прошлого человека новому.
     */
    val ownerUserId: Long,
)

@Serializable
private data class QueueFile(val schemaVersion: Int, val records: List<AnalyticsRecord>)

/**
 * Очередь событий на диске.
 *
 * Отдельная сущность, а не тот же OutboxStore: его payload завязан на операции.
 * Приём файла повторяется, модель — нет.
 *
 * Ключевое отличие от очереди расходов: события НЕ наследуются при смене
 * владельца, а выбрасываются. Расход человек ввёл сам и терять его нельзя;
 * событие содержимого не несёт, и приклеить его чужому человеку хуже.
 */
class AnalyticsQueue(
    private val file: File,
    private val json: Json,
) {
    private val mutex = Mutex()
    private var isLoaded = false
    private var didRead = false
    private var records: MutableList<AnalyticsRecord> = mutableListOf()

    suspend fun append(record: AnalyticsRecord) = mutex.withLock {
        ensureLoaded()
        records.add(record)
        if (records.size > CAPACITY) {
            // Потолок: файл не должен расти без предела, а свежие события
            // полезнее давних.
            records = records.takeLast(CAPACITY).toMutableList()
        }
        persist()
    }

    /** Забирает до [limit] записей владельца для отправки. */
    suspend fun take(limit: Int, owner: Long): List<AnalyticsRecord> = mutex.withLock {
        ensureLoaded()
        records.filter { it.ownerUserId == owner }.take(limit)
    }

    suspend fun remove(ids: Set<String>) = mutex.withLock {
        if (ids.isEmpty()) return@withLock
        ensureLoaded()
        records.removeAll { it.id in ids }
        persist()
    }

    /** Оставляет только записи этого владельца; null — чистит всё. */
    suspend fun keepOwned(userId: Long?) = mutex.withLock {
        ensureLoaded()
        val before = records.size
        if (userId == null) {
            records.clear()
        } else {
            records.removeAll { it.ownerUserId != userId }
        }
        if (records.size != before) persist()
    }

    suspend fun snapshot(): List<AnalyticsRecord> = mutex.withLock {
        ensureLoaded()
        records.toList()
    }

    private suspend fun ensureLoaded() {
        if (isLoaded && didRead) return
        withContext(Dispatchers.IO) {
            if (!file.exists()) {
                didRead = true
                isLoaded = true
                return@withContext
            }
            runCatching { file.readText() }
                .onSuccess { text ->
                    didRead = true
                    // Битый JSON восстановлению не подлежит и блокировать
                    // запись навсегда не должен — в отличие от сбоя чтения.
                    records = runCatching { json.decodeFromString(QueueFile.serializer(), text).records }
                        .getOrElse { emptyList() }
                        .toMutableList()
                }
                .onFailure { Log.e(TAG, "не удалось прочитать очередь событий", it) }
            isLoaded = true
        }
    }

    private suspend fun persist() {
        // Пока файл не прочитан, на диске лежат события, которых нет в памяти:
        // запись стёрла бы их начисто.
        if (!didRead) return
        val snapshot = QueueFile(SCHEMA_VERSION, records.toList())
        withContext(Dispatchers.IO) {
            runCatching {
                val tmp = File(file.parentFile, "${file.name}.tmp")
                tmp.writeText(json.encodeToString(QueueFile.serializer(), snapshot))
                Files.move(tmp.toPath(), file.toPath(), StandardCopyOption.REPLACE_EXISTING)
            }.onFailure { Log.e(TAG, "не удалось записать очередь событий", it) }
        }
    }

    companion object {
        const val CAPACITY = 500
        const val SCHEMA_VERSION = 1
    }
}
