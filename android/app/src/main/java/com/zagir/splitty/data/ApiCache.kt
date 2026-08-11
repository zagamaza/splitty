package com.zagir.splitty.data

import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json

/**
 * Офлайн-кеш последних успешных ответов ключевых GET: JSON-файлы
 * `<dir>/<key>.json` (dir = context.filesDir/cache-api). Запись атомарная —
 * во временный файл с последующим ATOMIC_MOVE, чтобы обрыв процесса не
 * оставил битый кеш. Битый/отсутствующий файл при чтении — просто null
 * (кеш не критичен, свежие данные придут из сети).
 *
 * Не Android-зависим (java.io + kotlinx.serialization) — покрыт юнит-тестами.
 */
class ApiCache(
    private val dir: File,
    private val json: Json,
) {

    /** Счётчик уникальных имён временных файлов (см. [write]). */
    private val tmpCounter = AtomicLong(0)

    /** Ключи кешируемых GET-ответов (v1 офлайн-дизайна). */
    object Keys {
        const val FRIENDS = "friends"
        const val ACTIVITY_FIRST_PAGE = "activity-page0"
        const val NOTIFICATIONS_FIRST_PAGE = "notifications-first"
        const val CURRENCIES = "currencies"
        const val ME = "me"

        fun rooms(archived: Boolean): String = if (archived) "rooms-archived" else "rooms"
        fun room(roomId: String): String = "room-$roomId"
        fun statistics(roomId: String): String = "statistics-$roomId"
    }

    /** Сохраняет свежий ответ сети (атомарно; ошибки записи не критичны). */
    suspend fun <T> write(key: String, serializer: KSerializer<T>, value: T) {
        withContext(Dispatchers.IO) {
            runCatching {
                dir.mkdirs()
                val target = fileFor(key)
                // Имя tmp уникально на запись: общий "<key>.tmp" два писателя
                // одного ключа (первая загрузка + pull-to-refresh) затирали друг
                // другу на полуслове — в кеш уезжал обрезанный JSON.
                val tmp = File(dir, "${target.name}.${tmpCounter.incrementAndGet()}.tmp")
                try {
                    tmp.writeText(json.encodeToString(serializer, value))
                    try {
                        Files.move(
                            tmp.toPath(),
                            target.toPath(),
                            StandardCopyOption.ATOMIC_MOVE,
                            StandardCopyOption.REPLACE_EXISTING,
                        )
                    } catch (_: Exception) {
                        // ФС без атомарного move — обычная замена (тоже одним файлом).
                        Files.move(tmp.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING)
                    }
                } finally {
                    // Имена уникальны — недоехавший tmp иначе копился бы навсегда.
                    tmp.delete()
                }
            }
        }
    }

    /** Последний закешированный ответ; null — кеша нет или он не читается. */
    suspend fun <T> read(key: String, serializer: KSerializer<T>): T? =
        withContext(Dispatchers.IO) {
            val file = fileFor(key)
            if (!file.isFile) return@withContext null
            runCatching { json.decodeFromString(serializer, file.readText()) }.getOrNull()
        }

    /** Полная очистка кеша (logout). */
    suspend fun clear() {
        withContext(Dispatchers.IO) {
            // Рекурсивно: непустой подкаталог обычный delete() не берёт, и данные
            // прошлого аккаунта пережили бы логаут. Каталог сразу восстанавливаем —
            // write() рассчитывает, что он существует.
            runCatching {
                dir.deleteRecursively()
                dir.mkdirs()
            }
        }
    }

    /**
     * Имя файла кеша. Ключ склеивается из id, пришедших С СЕРВЕРА, поэтому
     * всё, кроме `[A-Za-z0-9-_]`, схлопывается в `_`: иначе roomId вида
     * `../../outbox` уводил запись за пределы каталога кеша (path traversal) —
     * поверх очереди офлайн-расходов, которую clear() к тому же не стирает.
     */
    private fun fileFor(key: String): File = File(dir, "${key.replace(UNSAFE_KEY_CHARS, "_")}.json")

    private companion object {
        val UNSAFE_KEY_CHARS = Regex("[^A-Za-z0-9\\-_]")
    }
}
