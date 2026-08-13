package com.zagir.splitty.ui.groups

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.DataFreshness
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import java.time.Instant
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * VM вкладки «Группы»: активные и архивные группы, создание/присоединение,
 * разархивирование. Перезагрузка — по SessionStore.dataVersion (первое
 * значение запускает первичную загрузку); отмена корутины — не ошибка.
 */
@HiltViewModel
class GroupsListViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val outboxSyncer: OutboxSyncer,
    outboxStore: OutboxStore,
) : ViewModel() {

    private val _rooms = MutableStateFlow<UiState<List<RoomSummary>>>(UiState.Loading)

    /** Активные (неархивные) группы. */
    val rooms: StateFlow<UiState<List<RoomSummary>>> = _rooms.asStateFlow()

    /**
     * Показан офлайн-кеш и когда данные последний раз приходили с сервера.
     * Признак «из кеша» вычислялся и раньше, но никуда не попадал — человек
     * смотрел на старые суммы, ничего об этом не зная, и «неправильный» баланс
     * выглядел как ошибка расчёта.
     */
    private val _freshness = MutableStateFlow(DataFreshness())
    val freshness: StateFlow<DataFreshness> = _freshness.asStateFlow()

    private val _archived = MutableStateFlow<UiState<List<RoomSummary>>>(UiState.Loading)

    /** Архивные группы (грузятся при первом заходе в раздел «Архив»). */
    val archived: StateFlow<UiState<List<RoomSummary>>> = _archived.asStateFlow()

    private val _isRefreshing = MutableStateFlow(false)

    /** true во время pull-to-refresh (спиннер индикатора). */
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    private val _isMutating = MutableStateFlow(false)

    /** true, пока в полёте мутация (создание/join/разархивирование). */
    val isMutating: StateFlow<Boolean> = _isMutating.asStateFlow()

    private val _alertMessage = MutableStateFlow<UiText?>(null)

    /** Текст алерта «Ошибка» (мутации и тихие обновления поверх контента). */
    val alertMessage: StateFlow<UiText?> = _alertMessage.asStateFlow()

    /**
     * id комнат с неотправленными (локальными) операциями — для бейджа
     * «есть неотправленные операции» на карточке группы (порт iOS
     * `session.outbox.entries(roomId:)`, но одним множеством на весь список).
     */
    val pendingRoomIds: StateFlow<Set<String>> =
        outboxStore.entries
            .map { entries -> entries.mapTo(mutableSetOf()) { it.roomId } }
            .stateIn(viewModelScope, SharingStarted.Eagerly, emptySet())

    /** Архив грузим лениво — только после первого захода в раздел. */
    private var isArchiveRequested = false

    init {
        viewModelScope.launch {
            // Единая инвалидация: первичная загрузка (версия 0) и перезагрузка
            // после каждой успешной мутации данных в любом экране.
            sessionStore.dataVersion.collectLatest {
                loadRooms()
                if (isArchiveRequested) loadArchive()
            }
        }
    }

    /** Pull-to-refresh активного списка (и архива, если он уже открывался). */
    fun refresh() {
        outboxSyncer.syncNow() // pull-to-refresh — один из триггеров досылки outbox
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                loadRooms()
                if (isArchiveRequested) loadArchive()
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    /** Повтор после ошибки первичной загрузки списка. */
    fun retry() {
        _rooms.value = UiState.Loading
        viewModelScope.launch { loadRooms() }
    }

    /** Заход в раздел «Архив» (и повтор после ошибки): (пере)загрузка архива. */
    fun onArchiveOpened() {
        isArchiveRequested = true
        if (_archived.value is UiState.Error) _archived.value = UiState.Loading
        viewModelScope.launch { loadArchive() }
    }

    fun dismissAlert() {
        _alertMessage.value = null
    }

    /** POST /rooms; успех — dataVersion bump (список обновится сам) и [onSuccess]. */
    fun createGroup(name: String, onSuccess: () -> Unit) {
        val trimmed = name.trim()
        if (trimmed.isEmpty()) return
        mutate(onSuccess) { repository.createRoom(trimmed) }
    }

    /** POST /rooms/{id}/join по коду или ссылке-приглашению. */
    fun joinGroup(codeInput: String, onSuccess: () -> Unit) {
        val roomId = parseRoomCode(codeInput) ?: return
        mutate(onSuccess) { repository.joinRoom(roomId) }
    }

    /** Возвращает группу из архива (оба списка обновятся по dataVersion). */
    fun unarchive(roomId: String) {
        mutate(onSuccess = {}) { repository.unarchiveRoom(roomId) }
    }

    private fun mutate(onSuccess: () -> Unit, block: suspend () -> Any?) {
        if (_isMutating.value) return
        viewModelScope.launch {
            _isMutating.value = true
            try {
                block()
                // Единая инвалидация: все экраны-списки перезагрузятся сами.
                sessionStore.noteDataChanged()
                onSuccess()
            } catch (e: ApiException) {
                _alertMessage.value = humanErrorText(e)
            } finally {
                _isMutating.value = false
            }
        }
    }

    private suspend fun loadRooms() {
        try {
            val fetched = repository.rooms(archived = false)
            _rooms.value = UiState.Content(fetched.value)
            // Список пуст — проверяем архив: заархивировав ПОСЛЕДНЮЮ группу,
            // человек терял единственный вход в архив, и достать её обратно
            // было нельзя. Строка «Архив» рисуется по этому списку
            if (fetched.value.isEmpty()) loadArchive()
            _freshness.value = if (fetched.fromCache) {
                _freshness.value.copy(fromCache = true)
            } else {
                DataFreshness(fromCache = false, updatedAt = Instant.now())
            }
        } catch (e: CancellationException) {
            throw e // отмена (ушли с экрана / новая версия данных) — не ошибка
        } catch (e: ApiException) {
            if (_rooms.value is UiState.Content) {
                _alertMessage.value = humanErrorText(e) // тихое обновление поверх контента
            } else {
                _rooms.value = UiState.Error(e.message)
            }
        }
    }

    private suspend fun loadArchive() {
        try {
            _archived.value = UiState.Content(repository.rooms(archived = true).value)
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            if (_archived.value is UiState.Content) {
                _alertMessage.value = humanErrorText(e)
            } else {
                _archived.value = UiState.Error(e.message)
            }
        }
    }
}

/**
 * Длина кода группы: комнаты адресуются mongo ObjectID, а это ровно 24
 * hex-символа (`primitive.ObjectIDFromHex` в `internal/rest/handlers.go`).
 */
internal const val ROOM_CODE_LENGTH = 24

/**
 * Маркеры, после которых в строке начинается код. Порядок важен: `start=room`
 * проверяется первым, иначе ссылка бота, в имени которого встретился `/join/`,
 * разобралась бы не с того места.
 */
private val ROOM_CODE_MARKERS = listOf("start=room", "/join/")

/**
 * Единственный разборщик приглашения в группу (порт iOS `RoomCodeParser`).
 * Второго заводить нельзя: код приходит четырьмя дорогами, и правила у них общие.
 *
 * - universal link `https://<domain>/join/<roomId>` — страница приглашения
 *   (`internal/rest/deeplink.go`);
 * - кастомная схема `splitty://join/<roomId>` — кнопка «Открыть в приложении»
 *   на той же странице (и единственный канал до покупки домена);
 * - легаси-ссылка бота `https://t.me/<bot>?start=room<roomId>`, а также форма
 *   `room<roomId>`, как её кладёт в буфер бот;
 * - «голый» код, вставленный из буфера.
 *
 * Возвращает null, если кода нужного формата в строке нет.
 *
 * Результат — `String?`, а НЕ пустая строка в роли «не распознано»: диплинку
 * нужно отличать одно от другого, а пустая строка в этой роли требует помнить
 * о ней на каждом вызове.
 *
 * Код — РОВНО 24 hex-символа, а не «любой hex-префикс» (тот же формат, что у
 * iOS `RoomCodeParser`): недобранный код гасит кнопку «Присоединиться» до
 * запроса, а мусорный путь из внешней ссылки не превращается в запрос к
 * серверу, который тот всё равно отвергнет 404-м.
 */
internal fun parseRoomCode(input: String): String? {
    val text = input.trim()
    if (text.isEmpty()) return null
    for (marker in ROOM_CODE_MARKERS) {
        val index = text.indexOf(marker, ignoreCase = true)
        if (index >= 0) return hexCode(text.substring(index + marker.length))
    }
    // Форма «room<hex>» — только с начала строки: в середине это уже часть ссылки.
    if (text.startsWith("room", ignoreCase = true)) return hexCode(text.substring(4))
    return hexCode(text)
}

/**
 * Берёт hex-префикс (обрезая «хвост» ссылки — слеш, `?utm=…`, `#`) и принимает
 * его, только если это код целиком.
 *
 * Диапазоны сравниваются посимвольно по ASCII, а не через `Char.isDigit()`:
 * последний считает цифрами и арабо-индийские (`٠`…`٩`), и такой «код» уехал бы
 * на сервер мусором.
 */
private fun hexCode(tail: String): String? {
    val hex = tail.takeWhile { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }
    // ObjectID из Go всегда в нижнем регистре; приводим и вставленный вручную,
    // чтобы один и тот же код не выглядел двумя разными.
    return if (hex.length == ROOM_CODE_LENGTH) hex.lowercase() else null
}

