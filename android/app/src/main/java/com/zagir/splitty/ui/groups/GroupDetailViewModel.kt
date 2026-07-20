package com.zagir.splitty.ui.groups

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxEntry
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.YearMonth
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/** Секция операций одного месяца («Июль 2026»), новые месяцы первыми. */
data class MonthSection(
    val month: YearMonth,
    val title: String,
    val operations: List<Operation>,
)

/** Группирует операции по месяцам локального времени, новые первыми. */
internal fun groupOperationsByMonth(operations: List<Operation>): List<MonthSection> =
    operations
        .groupBy { GroupsDateFmt.yearMonth(it.createdAt) }
        .entries
        .sortedByDescending { it.key }
        .map { (month, monthOperations) ->
            MonthSection(
                month = month,
                title = GroupsDateFmt.monthYear(month),
                operations = monthOperations.sortedByDescending { it.createdAt },
            )
        }

/**
 * VM экрана группы: деталь комнаты (GET /rooms/{id}), операции по месяцам,
 * валюты и архив для настроек. Перезагрузка — по roomId + dataVersion
 * (collectLatest: свежая версия отменяет незавершённую загрузку).
 */
@HiltViewModel
class GroupDetailViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val outboxSyncer: OutboxSyncer,
    outboxStore: OutboxStore,
    networkMonitor: NetworkMonitor,
) : ViewModel() {

    private val roomIdFlow = MutableStateFlow<String?>(null)

    private val _room = MutableStateFlow<UiState<RoomDetail>>(UiState.Loading)

    /** Деталь комнаты — источник всех секций экрана. */
    val room: StateFlow<UiState<RoomDetail>> = _room.asStateFlow()

    private val _sections = MutableStateFlow<List<MonthSection>>(emptyList())

    /** Операции комнаты по месяцам (вычисляются при каждой загрузке). */
    val sections: StateFlow<List<MonthSection>> = _sections.asStateFlow()

    /** id текущего пользователя; null — профиль ещё не загружен. */
    val meId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    /**
     * Неотправленные (локальные) операции этой комнаты — сверху списка
     * с бейджем «не отправлено»; новые первыми, как серверные операции.
     */
    val localOperations: StateFlow<List<OutboxEntry>> =
        combine(outboxStore.entries, roomIdFlow) { entries, roomId ->
            entries
                .filter { it.roomId == roomId }
                .sortedByDescending { it.createdAt }
        }.stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    /** Онлайн-статус — для guard'а погашения (офлайн — алерт). */
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    private val _alertMessage = MutableStateFlow<String?>(null)
    val alertMessage: StateFlow<String?> = _alertMessage.asStateFlow()

    // --- Настройки группы ---

    private val _currencies = MutableStateFlow<UiState<List<CurrencyInfo>>>(UiState.Loading)

    /** Справочник валют GET /currencies (грузится при открытии настроек). */
    val currencies: StateFlow<UiState<List<CurrencyInfo>>> = _currencies.asStateFlow()

    private val _savingCurrency = MutableStateFlow<String?>(null)

    /** Код валюты, PUT которой сейчас в полёте (спиннер у строки пикера). */
    val savingCurrency: StateFlow<String?> = _savingCurrency.asStateFlow()

    private val _selectedCurrencyOverride = MutableStateFlow<String?>(null)

    /** Локально выбранная валюта — чекмарк обновляется сразу, до перезагрузки. */
    val selectedCurrencyOverride: StateFlow<String?> = _selectedCurrencyOverride.asStateFlow()

    private val _isArchiving = MutableStateFlow(false)
    val isArchiving: StateFlow<Boolean> = _isArchiving.asStateFlow()

    init {
        viewModelScope.launch {
            combine(roomIdFlow.filterNotNull(), sessionStore.dataVersion) { id, _ -> id }
                .collectLatest { id -> load(id) }
        }
    }

    /** Привязка экрана к комнате; повторные вызовы с тем же id — no-op. */
    fun start(roomId: String) {
        if (roomIdFlow.value != roomId) roomIdFlow.value = roomId
    }

    /** Pull-to-refresh экрана группы. */
    fun refresh() {
        val id = roomIdFlow.value ?: return
        outboxSyncer.syncNow() // pull-to-refresh — один из триггеров досылки outbox
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                load(id)
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    /** Повтор после ошибки первичной загрузки. */
    fun retry() {
        val id = roomIdFlow.value ?: return
        _room.value = UiState.Loading
        viewModelScope.launch { load(id) }
    }

    fun dismissAlert() {
        _alertMessage.value = null
    }

    /** Тап по «Погасить долг» без сети: погашения офлайн недоступны. */
    fun showSettleUpUnavailableOffline() {
        // Единый с iOS текст: «Нет соединения. Погашение долга доступно только онлайн».
        _alertMessage.value = OFFLINE_SETTLE_MESSAGE
    }

    companion object {
        /** Ключ строки group_settle_offline (VM без ресурсов — константа-дубль). */
        const val OFFLINE_SETTLE_MESSAGE =
            "Нет соединения. Погашение долга доступно только онлайн"
    }

    /** Загружает справочник валют для пикера (после ошибки можно повторить). */
    fun loadCurrencies() {
        if (_currencies.value is UiState.Content) return
        _currencies.value = UiState.Loading
        viewModelScope.launch {
            try {
                _currencies.value = UiState.Content(repository.currencies().value)
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                _currencies.value = UiState.Error(e.message)
            }
        }
    }

    /** PUT /rooms/{id}/currency: меняет валюту группы + единая инвалидация. */
    fun setCurrency(code: String) {
        val id = roomIdFlow.value ?: return
        val current = _selectedCurrencyOverride.value
            ?: (_room.value as? UiState.Content)?.value?.currency
        if (_savingCurrency.value != null || code == current) return
        viewModelScope.launch {
            _savingCurrency.value = code
            try {
                repository.setRoomCurrency(id, code)
                _selectedCurrencyOverride.value = code
                // Единая инвалидация: экран и списки перечитают суммы в новой валюте.
                sessionStore.noteDataChanged()
            } catch (e: ApiException) {
                _alertMessage.value = e.message
            } finally {
                _savingCurrency.value = null
            }
        }
    }

    /** Архивирует/разархивирует группу; успех — dataVersion bump и [onDone]. */
    fun toggleArchive(onDone: () -> Unit) {
        val detail = (_room.value as? UiState.Content)?.value ?: return
        if (_isArchiving.value) return
        viewModelScope.launch {
            _isArchiving.value = true
            try {
                if (detail.isArchived) {
                    repository.unarchiveRoom(detail.id)
                } else {
                    repository.archiveRoom(detail.id)
                }
                sessionStore.noteDataChanged()
                onDone()
            } catch (e: ApiException) {
                _alertMessage.value = e.message
            } finally {
                _isArchiving.value = false
            }
        }
    }

    private suspend fun load(roomId: String) {
        if ((_room.value as? UiState.Content)?.value?.id != roomId) {
            _room.value = UiState.Loading
            _sections.value = emptyList()
        }
        try {
            // Профиль мог не попасть в кэш (например, вход на другом устройстве) —
            // без meId подписи «вы должны/одолжили» показать нельзя.
            if (sessionStore.state.value?.me == null) {
                try {
                    sessionStore.updateMe(repository.me().value)
                } catch (e: CancellationException) {
                    throw e
                } catch (_: Throwable) {
                    // Не критично: экран покажет нейтральное состояние с повтором.
                    // Throwable, а не ApiException: updateMe пишет в DataStore и
                    // бросает IOException — необработанным он убивал процесс.
                }
            }
            val detail = repository.room(roomId).value
            _room.value = UiState.Content(detail)
            _sections.value = groupOperationsByMonth(detail.operations)
            _selectedCurrencyOverride.value = null // источник истины снова сервер
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            if (_room.value is UiState.Content) {
                _alertMessage.value = e.message
            } else {
                _room.value = UiState.Error(e.message)
            }
        }
    }
}
