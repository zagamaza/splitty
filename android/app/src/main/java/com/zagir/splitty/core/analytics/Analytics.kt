package com.zagir.splitty.core.analytics

import android.util.Log
import com.zagir.splitty.BuildConfig
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import java.time.Instant
import java.time.format.DateTimeFormatter
import java.util.Locale
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import retrofit2.HttpException

private const val TAG = "Analytics"

/** Тело пачки. Поля совпадают с контрактом, см. docs/analytics-events.md. */
@kotlinx.serialization.Serializable
data class EventsBody(val events: List<AnalyticsRecord>)

@kotlinx.serialization.Serializable
data class EventsResult(val accepted: Int = 0, val duplicates: Int = 0, val rejected: Int = 0)

/**
 * Сбор продуктовых событий.
 *
 * Нет сессии — не пишем вовсе: приём на сервере закрыт авторизацией, а копить
 * события «до входа» значило бы решать, кому они достанутся, когда человек
 * войдёт. Приветствие пост-логинное, так что теряется практически только
 * app_open холодного старта.
 */
@Singleton
class Analytics @Inject constructor(
    private val queue: AnalyticsQueue,
    private val api: SplittyApi,
    private val session: SessionStore,
    @ApplicationScope private val scope: CoroutineScope,
) {
    private val flushMutex = Mutex()

    @Volatile
    private var sessionId: String = UUID.randomUUID().toString()

    @Volatile
    private var lastActivity: Long = System.currentTimeMillis()

    @Volatile
    private var ownerUserId: Long? = null

    /**
     * Смена владельца: события прошлого человека выбрасываем, а не
     * переклеиваем на нового.
     */
    fun onOwnerChanged(userId: Long?) {
        if (ownerUserId == userId) return
        ownerUserId = userId
        sessionId = UUID.randomUUID().toString()
        scope.launch { queue.keepOwned(userId) }
    }

    /** Новая сессия на холодном старте. */
    fun startSession() {
        sessionId = UUID.randomUUID().toString()
        lastActivity = System.currentTimeMillis()
    }

    fun track(event: AnalyticsEvent) {
        if (!ENABLED) return
        val owner = ownerUserId ?: session.currentUserId() ?: return
        val record = record(event, owner)
        scope.launch {
            queue.append(record)
            if (queue.take(BATCH_SIZE, owner).size >= BATCH_SIZE) flush()
        }
    }

    /**
     * Событие, после которого очередь перестаёт существовать: выход и удаление
     * аккаунта.
     *
     * Отправляется НАПРЯМУЮ, минуя очередь. Сразу после такого события
     * OfflineDataCleaner вычищает очередь как чужую, и положенная в неё запись
     * не уехала бы никогда — событие выглядело бы проинструментированным и
     * молчало.
     *
     * Не доехало — значит потеряно: у последнего вздоха ретраить негде.
     */
    fun trackTerminal(event: AnalyticsEvent) {
        if (!ENABLED) return
        val owner = ownerUserId ?: session.currentUserId() ?: return
        val record = record(event, owner)
        scope.launch {
            runCatching { api.postEvents(EventsBody(listOf(record))) }
        }
    }

    private fun record(event: AnalyticsEvent, owner: Long): AnalyticsRecord {
        val now = System.currentTimeMillis()
        if (now - lastActivity > SESSION_IDLE_LIMIT_MS) {
            sessionId = UUID.randomUUID().toString()
        }
        lastActivity = now

        return AnalyticsRecord(
            id = UUID.randomUUID().toString(),
            name = event.name,
            at = DateTimeFormatter.ISO_INSTANT.format(Instant.ofEpochMilli(now)),
            session = sessionId,
            appVersion = BuildConfig.VERSION_NAME,
            locale = Locale.getDefault().toLanguageTag(),
            params = event.params,
            ownerUserId = owner,
        )
    }

    /** Отправляет накопленное. Ошибка — не повод чистить очередь. */
    suspend fun flush() {
        if (!ENABLED) return
        val owner = ownerUserId ?: session.currentUserId() ?: return
        flushMutex.withLock {
            val batch = queue.take(BATCH_SIZE, owner)
            if (batch.isEmpty()) return@withLock
            try {
                api.postEvents(EventsBody(batch))
                queue.remove(batch.map { it.id }.toSet())
            } catch (e: CancellationException) {
                throw e
            } catch (e: HttpException) {
                // Постоянный отказ не крутится вечно: сервер сказал, что эта
                // пачка ему не годится, и повтор ничего не изменит.
                if (e.code() in setOf(400, 401, 403, 413)) {
                    queue.remove(batch.map { it.id }.toSet())
                }
            } catch (e: Exception) {
                Log.d(TAG, "пачка событий не ушла, попробуем позже", e)
            }
        }
    }

    companion object {
        /**
         * Выключатель — константа сборки, а не настройка.
         *
         * Настройка потянула бы строки в пять локалей и перезапись эталонов
         * Roborazzi (CI сверяет снимки и падает на расхождении) ради
         * переключателя, который нужен нам, а не человеку.
         */
        const val ENABLED = true
        const val BATCH_SIZE = 20
        const val SESSION_IDLE_LIMIT_MS = 30 * 60 * 1000L
    }
}
