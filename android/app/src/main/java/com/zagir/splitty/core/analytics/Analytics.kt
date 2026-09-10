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
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import retrofit2.HttpException

private const val TAG = "Analytics"

/** Тело пачки. Поля совпадают с контрактом, см. docs/analytics-events.md. */
@kotlinx.serialization.Serializable
data class EventsBody(val events: List<EventBody>)

/**
 * Одно событие НА ПРОВОДЕ.
 *
 * Отдельный тип от [AnalyticsRecord] ради одного поля: у записи очереди есть
 * ownerUserId — он нужен на диске, чтобы при смене аккаунта разобрать, чьи
 * события остались. Серверу его слать нечего: человека тот берёт из токена,
 * поле игнорирует, и в контракте его нет. iOS шлёт ровно этот набор.
 */
@kotlinx.serialization.Serializable
data class EventBody(
    val id: String,
    val name: String,
    val at: String,
    val session: String,
    val platform: String,
    val appVersion: String,
    val locale: String,
    val params: Map<String, String>,
) {
    constructor(record: AnalyticsRecord) : this(
        id = record.id,
        name = record.name,
        at = record.at,
        session = record.session,
        platform = record.platform,
        appVersion = record.appVersion,
        locale = record.locale,
        params = record.params,
    )
}

/** Пачка событий ДО входа: вместо человека — установка приложения. */
@kotlinx.serialization.Serializable
data class AnonymousEventsBody(val device: String, val events: List<EventBody>)

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
    private val deviceId: DeviceIdSource,
) {
    private val flushMutex = Mutex()

    @Volatile
    private var sessionId: String = UUID.randomUUID().toString()

    @Volatile
    private var lastActivity: Long = System.currentTimeMillis()

    /**
     * Кто владеет очередью — по версии OfflineDataCleaner.
     *
     * Служит ТОЛЬКО учёту смены аккаунта: завести или погасить таймер и решить,
     * чьи записи оставить. Адресовать им события нельзя: поле двигает отдельная
     * эмиссия чистильщика и оно отстаёт от сессии. Владельца события и токен
     * отправки берут одним снимком `session.state`.
     */
    @Volatile
    private var ownerUserId: Long? = null

    /** Периодический досыл; живёт, пока есть владелец. */
    @Volatile
    private var timer: Job? = null

    /**
     * Смена владельца: события прошлого человека выбрасываем, а не
     * переклеиваем на нового.
     *
     * [keepQueue] — исключение ровно для протухшей сессии. Человек вернётся
     * ТЕМ ЖЕ аккаунтом, записи лежат с его номером и доедут после
     * переавторизации. Без исключения 401 стирал очередь молча — при том что
     * соседняя очередь неотправленных расходов ту же ситуацию переживает
     * намеренно (см. OfflineDataCleaner). Две противоположные политики на один
     * сценарий — это и была дыра.
     */
    fun onOwnerChanged(userId: Long?, keepQueue: Boolean = false) {
        if (ownerUserId == userId) return
        ownerUserId = userId
        sessionId = UUID.randomUUID().toString()
        if (userId == null) stopTimer() else startTimer()
        if (keepQueue) return
        scope.launch { queue.keepOwned(userId) }
    }

    /**
     * Досыл по времени.
     *
     * Без него единственным поводом отправки был порог [BATCH_SIZE]: человек,
     * сделавший за сеанс десяток шагов, не отправлял НИЧЕГО — записи лежали на
     * диске до двадцатого события, то есть у большинства навсегда. iOS так
     * умеет с самого начала (flushInterval), android — нет; это расхождение и
     * съело воронку.
     */
    private fun startTimer() {
        if (timer?.isActive == true) return
        timer = scope.launch {
            while (isActive) {
                delay(FLUSH_INTERVAL_MS)
                flush()
            }
        }
    }

    private fun stopTimer() {
        timer?.cancel()
        timer = null
    }

    /**
     * Приложение ушло в фон — отправляем, не дожидаясь порога и тика таймера.
     *
     * Момент важный: дальше процесс может не проснуться вовсе, а на диске
     * останется хвост, который никто не досылает.
     */
    fun onBackgrounded() {
        if (!ENABLED) return
        scope.launch { flush() }
    }

    /**
     * Приложение вернулось на передний план — досылаем хвост прошлого сеанса.
     *
     * Без этого после перезапуска накопленное ждало бы первого тика таймера, а
     * короткий сеанс столько может и не прожить. Здесь, а не внутри
     * [onOwnerChanged]: тот зовётся из collect'а OfflineDataCleaner, и отправка
     * оттуда лезет в тот же файл очереди одновременно с его чисткой.
     */
    fun onForegrounded() {
        if (!ENABLED) return
        scope.launch { flush() }
    }

    /** Новая сессия на холодном старте. */
    fun startSession() {
        sessionId = UUID.randomUUID().toString()
        lastActivity = System.currentTimeMillis()
    }

    /**
     * Событие, случившееся ДО входа.
     *
     * Уходит сразу и мимо очереди: очередь именная (записи лежат с номером
     * владельца и чистятся при смене аккаунта), и класть туда безымянные
     * значило бы решать, кому они достанутся после входа. Событий здесь
     * единицы за запуск, и потеря одного без сети — честная цена за то, что
     * этот поток ни с кем не связывается.
     */
    fun trackAnonymous(event: AnalyticsEvent) {
        if (!ENABLED) return
        val record = record(event, owner = 0L)
        scope.launch {
            try {
                api.postAnonymousEvents(AnonymousEventsBody(deviceId.get(), listOf(EventBody(record))))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.w(TAG, "anonymous event lost", e)
            }
        }
    }

    /**
     * Событие входа: владелец известен ЯВНО, из ответа сервера.
     *
     * Обычный [track] здесь не годится: он берёт владельца из `session.state`,
     * а тот наполняется отдельным collect'ом DataStore и на момент вызова —
     * сразу после signIn — может ещё молчать; событие молча возвращалось ни с
     * чем. Плюс отмена: в соседнем catch этого же экрана записано наблюдение,
     * что успешный вход сносит экран вместе с его scope (из-за чего штатная
     * отмена когда-то показывалась алертом). Запись в своём scope снимает и
     * это.
     *
     * В базе это выглядело так: login_started — 70 событий, login_completed —
     * 8. Последняя ступень воронки была не «плохой», а несуществующей.
     */
    fun trackSignedIn(event: AnalyticsEvent, userId: Long, token: String) {
        if (!ENABLED) return
        val record = record(event, userId)
        scope.launch {
            queue.append(record)
            // Владелец И токен — оба явные, оба от ЭТОГО входа. Обычный [flush]
            // берёт владельца из ownerUserId (его двигает OfflineDataCleaner) и
            // токен из перехватчика. В окне «A вышел → B вошёл» владелец может
            // ещё быть A, а сессия уже B — и записи A уехали бы под токеном B,
            // то есть события одного человека записались бы на другого.
            flushOwned(userId, "Bearer " + token)
        }
    }

    fun track(event: AnalyticsEvent) {
        if (!ENABLED) return
        // Владелец — из состояния сессии, а не из ownerUserId. То поле двигает
        // OfflineDataCleaner своей эмиссией и в окне «A вышел → B вошёл» оно
        // отстаёт: событие ЭКРАНА, который уже смотрит B, записывалось на A —
        // а потом reconciliation стирал его как чужое. Событие пропадало,
        // выглядя записанным.
        val owner = session.state.value?.me?.id ?: return
        val record = record(event, owner)
        scope.launch {
            queue.append(record)
            if (queue.take(BATCH_SIZE, owner).size >= BATCH_SIZE) flush()
        }
    }

    /**
     * Событие, после которого очереди уже не будет: нажатие «Выйти».
     *
     * Удаление аккаунта сюда не относится и относиться не может — см.
     * docs/analytics-events.md.
     *
     * Отправляется НАПРЯМУЮ, минуя очередь. Сразу после такого события
     * OfflineDataCleaner вычищает очередь как чужую, и положенная в неё запись
     * не уехала бы никогда — событие выглядело бы проинструментированным и
     * молчало.
     *
     * Не доехало — значит потеряно: у последнего вздоха ретраить негде.
     */
    fun trackTerminal(event: AnalyticsEvent): Job? {
        if (!ENABLED) return null
        // Токен и владелец — ОДНИМ снимком состояния. Два независимых чтения
        // могли бы разъехаться (человек вышел между ними), а заголовок берём
        // явный: перехватчик подставил бы токен, актуальный на момент
        // отправки, то есть уже чужой.
        val snapshot = session.state.value ?: return null
        val token = snapshot.token ?: return null
        // Владелец — из ТОГО ЖЕ снимка, что и токен. Поле ownerUserId живёт
        // своей жизнью (его двигает onOwnerChanged) и может отстать, а
        // разъехавшаяся пара «токен одного, номер другого» — ровно то, от чего
        // здесь и защищаемся.
        val owner = snapshot.me?.id ?: return null
        val record = record(event, owner)
        val authorization = "Bearer " + token
        // Запуск в СВОЁМ scope, а не в вызывающем: экран профиля исчезает сразу
        // за выходом, и отправка, привязанная к его жизни, отменялась бы на
        // полпути.
        return scope.launch {
            // Сначала хвост очереди, потом сам выход: очередь исчезнет через
            // мгновение, и это последняя возможность её отправить.
            drainBacklog(owner, authorization)
            try {
                api.postEvents(EventsBody(listOf(EventBody(record))), authorization)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.d(TAG, "терминальное событие не ушло", e)
            }
        }
    }

    /**
     * То же, но с ожиданием: вызывающий обязан дождаться, прежде чем ронять
     * сессию.
     *
     * Без ожидания отправка гонялась с чисткой и обычно ей проигрывала:
     * `logout()` запускал `sessionStore.logout()` независимо, `OfflineDataCleaner`
     * успевал вызвать keepOwned(null) раньше, чем drain доходил до первого
     * take, и хвост очереди исчезал ровно так же, как до всей этой правки.
     *
     * Ожидание ограничено [TERMINAL_WAIT_MS]: выход не имеет права зависнуть
     * из-за медленной сети. Не успели — уходим без хвоста, это честнее, чем
     * держать человека в приложении, из которого он попросился выйти.
     */
    suspend fun trackTerminalAndAwait(event: AnalyticsEvent) {
        val job = trackTerminal(event) ?: return
        withTimeoutOrNull(TERMINAL_WAIT_MS) { job.join() }
    }

    /**
     * Досылает накопленное ПЕРЕД тем, как очередь станет чужой.
     *
     * Выход — единственный момент, когда записи гарантированно исчезают: сразу
     * после него OfflineDataCleaner зовёт keepOwned(null). Всё, что не дотянуло
     * до [BATCH_SIZE], уезжало в никуда, и в базе у человека оставался ровно
     * один logout — так это и выглядело на живых аккаунтах.
     *
     * Токен передаётся явно: к моменту отправки сессия уже пуста, и
     * перехватчик подставил бы пустоту.
     */
    private suspend fun drainBacklog(owner: Long, authorization: String) {
        flushMutex.withLock {
            repeat(MAX_DRAIN_BATCHES) {
                val batch = queue.take(BATCH_SIZE, owner)
                if (batch.isEmpty()) return@withLock
                try {
                    val result =
                        api.postEvents(EventsBody(batch.map { EventBody(it) }), authorization)
                    if (result.rejected > 0) {
                        Log.w(TAG, "на выходе отбраковано событий: ${result.rejected} из ${batch.size}")
                    }
                    queue.remove(batch.map { it.id }.toSet())
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    // Повторять негде: очередь вот-вот станет чужой. Молчим так
                    // же, как молчит сам logout.
                    Log.d(TAG, "хвост очереди перед выходом не ушёл", e)
                    return@withLock
                }
            }
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

    /**
     * Отправляет накопленное. Ошибка — не повод чистить очередь.
     *
     * Владелец и токен берутся ОДНИМ снимком сессии. Раньше владелец приходил
     * из `ownerUserId` — отдельного поля, которое двигает OfflineDataCleaner, —
     * а токен подставлял перехватчик из SessionStore. Это два независимых
     * источника, и в окне «A вышел → B вошёл» они расходятся: очередь A уезжала
     * под токеном B по любому поводу — таймеру, уходу в фон, возврату, порогу.
     * Один снимок закрывает это для всех путей разом.
     */
    suspend fun flush() {
        if (!ENABLED) return
        val snapshot = session.state.value ?: return
        val owner = snapshot.me?.id ?: return
        val token = snapshot.token ?: return
        flushOwned(owner, "Bearer " + token)
    }

    /**
     * Одна пачка КОНКРЕТНОГО владельца.
     *
     * [authorization] null — заголовок ставит перехватчик. Явное значение нужно
     * там, где владелец и токен известны точнее, чем текущее состояние сессии:
     * иначе записи одного человека уезжают под токеном другого.
     */
    private suspend fun flushOwned(owner: Long, authorization: String?) {
        flushMutex.withLock {
            val batch = queue.take(BATCH_SIZE, owner)
            if (batch.isEmpty()) return@withLock
            try {
                val result = api.postEvents(EventsBody(batch.map { EventBody(it) }), authorization)
                // 2xx с отказами внутри — не успех. Раньше результат не читался
                // вовсе, и отбракованное имя события выглядело доставленным.
                if (result.rejected > 0) {
                    Log.w(TAG, "сервер отбраковал событий: ${result.rejected} из ${batch.size}")
                }
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

        /** Тот же интервал, что у iOS: расхождение здесь уже стоило воронки. */
        const val FLUSH_INTERVAL_MS = 30_000L

        /**
         * Сколько пачек досылаем на выходе.
         *
         * Ограничение реальное: выхода теперь ЖДУТ (см. [trackTerminalAndAwait]),
         * и упереться в полную очередь на CAPACITY записей значило бы держать
         * человека в приложении, из которого он попросился выйти. Десять пачек
         * покрывают 200 записей — сеанс, после которого выходят, столько не
         * набирает.
         */
        const val MAX_DRAIN_BATCHES = 10

        /**
         * Сколько ждём отправки на выходе. Верхняя граница именно ожидания, а
         * не работы: не успели — уходим без хвоста.
         */
        const val TERMINAL_WAIT_MS = 3_000L
    }
}
