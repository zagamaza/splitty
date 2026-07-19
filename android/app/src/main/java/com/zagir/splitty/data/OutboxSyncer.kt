package com.zagir.splitty.data

import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex

/**
 * Досылка outbox на сервер. Триггеры: появление сети (isOnline → true),
 * старт/возврат приложения (MainActivity.onStart) и pull-to-refresh экранов.
 * Последовательный FIFO; параллельные запуски исключены Mutex'ом.
 *
 * Судьба записи: успех → удалить + SessionStore.noteDataChanged();
 * HTTP 4xx (кроме 401) → status=failed(текст сервера), очередь продолжается;
 * сеть/5xx → запись остаётся pending, синк прерывается до следующего триггера;
 * 401 → синк прерывается (глобальный разлогин делает AuthInterceptor).
 *
 * Идемпотентность: create шлёт clientOpId = localId записи — повтор после
 * потерянного ответа возвращает существующую операцию, а не создаёт дубль.
 */
@Singleton
class OutboxSyncer internal constructor(
    private val outbox: OutboxStore,
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    /**
     * Признак сети как поток, а не сам NetworkMonitor: монитор Android-зависим
     * (ConnectivityManager), а ветвление судьбы записи — чистая логика, и её
     * надо гонять юнит-тестами без Robolectric.
     */
    private val isOnline: StateFlow<Boolean>,
    private val scope: CoroutineScope,
) {
    @Inject
    constructor(
        outbox: OutboxStore,
        repository: SplittyRepository,
        sessionStore: SessionStore,
        networkMonitor: NetworkMonitor,
        @ApplicationScope scope: CoroutineScope,
    ) : this(outbox, repository, sessionStore, networkMonitor.isOnline, scope)

    private val mutex = Mutex()

    private val _isSyncing = MutableStateFlow(false)

    /** true, пока идёт досылка непустой очереди (баннер «Отправка…»). */
    val isSyncing: StateFlow<Boolean> = _isSyncing.asStateFlow()

    init {
        scope.launch {
            // Прогреваем outbox (entries-StateFlow наполняется до первого экрана)
            // и досылаем на каждое появление сети, включая старт приложения.
            outbox.awaitLoaded()
            isOnline.collect { online ->
                if (online) sync()
            }
        }
    }

    /** Запустить досылку в фоне (ON_START, pull-to-refresh). Идемпотентно. */
    fun syncNow() {
        scope.launch { sync() }
    }

    private suspend fun sync() {
        if (!mutex.tryLock()) return // синк уже идёт
        try {
            if (sessionStore.currentToken() == null) return
            if (!isOnline.value) return
            outbox.awaitLoaded()
            val queue = outbox.entries.value.filter { it.status == OutboxStatus.PENDING }
            if (queue.isEmpty()) return

            _isSyncing.value = true
            var anySucceeded = false
            for (entry in queue) {
                try {
                    send(entry)
                    outbox.remove(entry.localId)
                    anySucceeded = true
                } catch (e: CancellationException) {
                    throw e
                } catch (e: ApiException) {
                    val status = e.status
                    when {
                        // Мёртвая сессия: разлогин уже запущен интерцептором.
                        status == 401 -> break

                        // Сервер отверг именно эту запись — помечаем и идём дальше.
                        status != null && status in 400..499 -> {
                            outbox.markFailed(entry.localId, e.message)
                        }

                        // Сеть/5xx: остаёмся pending, ждём следующего триггера.
                        else -> break
                    }
                }
            }
            if (anySucceeded) {
                // Единая инвалидация: экраны перечитают данные (и обновят кеш).
                sessionStore.noteDataChanged()
            }
        } finally {
            _isSyncing.value = false
            mutex.unlock()
        }
    }

    private suspend fun send(entry: OutboxEntry) {
        val p = entry.payload
        when (entry.kind) {
            OutboxKind.CREATE -> repository.addOperation(
                roomId = entry.roomId,
                description = p.description,
                sum = p.sum,
                donorId = p.donorId,
                split = p.toSplit(),
                clientOpId = entry.localId,
            )

            // v1 не ставит update/delete в очередь (правка синхронизированных
            // офлайн недоступна); защита от неизвестных записей в файле.
            OutboxKind.UPDATE, OutboxKind.DELETE -> throw ApiException(
                status = 400,
                code = "unsupported",
                message = "Неподдерживаемая офлайн-операция",
            )
        }
    }
}
