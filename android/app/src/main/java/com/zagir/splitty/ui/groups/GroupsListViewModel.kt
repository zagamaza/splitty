package com.zagir.splitty.ui.groups

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
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
) : ViewModel() {

    private val _rooms = MutableStateFlow<UiState<List<RoomSummary>>>(UiState.Loading)

    /** Активные (неархивные) группы. */
    val rooms: StateFlow<UiState<List<RoomSummary>>> = _rooms.asStateFlow()

    private val _archived = MutableStateFlow<UiState<List<RoomSummary>>>(UiState.Loading)

    /** Архивные группы (грузятся при первом заходе в раздел «Архив»). */
    val archived: StateFlow<UiState<List<RoomSummary>>> = _archived.asStateFlow()

    private val _isRefreshing = MutableStateFlow(false)

    /** true во время pull-to-refresh (спиннер индикатора). */
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    private val _isMutating = MutableStateFlow(false)

    /** true, пока в полёте мутация (создание/join/разархивирование). */
    val isMutating: StateFlow<Boolean> = _isMutating.asStateFlow()

    private val _alertMessage = MutableStateFlow<String?>(null)

    /** Текст алерта «Ошибка» (мутации и тихие обновления поверх контента). */
    val alertMessage: StateFlow<String?> = _alertMessage.asStateFlow()

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
        val roomId = parseRoomCode(codeInput)
        if (roomId.isEmpty()) return
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
                _alertMessage.value = e.message
            } finally {
                _isMutating.value = false
            }
        }
    }

    private suspend fun loadRooms() {
        try {
            _rooms.value = UiState.Content(repository.rooms(archived = false).value)
        } catch (e: CancellationException) {
            throw e // отмена (ушли с экрана / новая версия данных) — не ошибка
        } catch (e: ApiException) {
            if (_rooms.value is UiState.Content) {
                _alertMessage.value = e.message // тихое обновление поверх контента
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
                _alertMessage.value = e.message
            } else {
                _archived.value = UiState.Error(e.message)
            }
        }
    }
}

/**
 * Код группы из ввода: принимает «голый» hex-код, `room<id>` и целиком
 * ссылку-приглашение вида t.me/split_money_bot?start=room<id>
 * (порт iOS JoinGroupView.roomId).
 */
internal fun parseRoomCode(input: String): String {
    var text = input.trim()
    val marker = "start=room"
    val markerIndex = text.indexOf(marker, ignoreCase = true)
    text = when {
        markerIndex >= 0 -> text.substring(markerIndex + marker.length)
        text.startsWith("room", ignoreCase = true) -> text.substring(4)
        else -> text
    }
    // Обрезаем возможный «хвост» ссылки: id — hex-строка ObjectID.
    return text.takeWhile { it.isDigit() || it.lowercaseChar() in 'a'..'f' }
}
