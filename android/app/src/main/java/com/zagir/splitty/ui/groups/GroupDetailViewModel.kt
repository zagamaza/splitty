package com.zagir.splitty.ui.groups

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.DataFreshness
import java.time.Instant
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.InviteStatus
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

    private val _alertMessage = MutableStateFlow<UiText?>(null)
    val alertMessage: StateFlow<UiText?> = _alertMessage.asStateFlow()

    /** Свежесть показанных данных — для подписи «данные сохранённые…». */
    private val _freshness = MutableStateFlow(DataFreshness())
    val freshness: StateFlow<DataFreshness> = _freshness.asStateFlow()

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
        _alertMessage.value = UiText.res(R.string.error_settle_offline)
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
                _alertMessage.value = humanErrorText(e)
            } finally {
                _savingCurrency.value = null
            }
        }
    }

    /** Друзья для выбора при приглашении (без тех, кто уже в группе). */
    private val _friends = MutableStateFlow<List<FriendBalance>>(emptyList())
    val friends: StateFlow<List<FriendBalance>> = _friends.asStateFlow()

    fun loadFriends() {
        viewModelScope.launch {
            runCatching { repository.friends().value }
                .onSuccess { _friends.value = it }
                .onFailure { if (it is ApiException) _alertMessage.value = humanErrorText(it) }
        }
    }

    /**
     * Позвать выбранных друзей: их id уже известен, код никому не нужен.
     *
     * Каждый приглашается отдельным запросом, и сбой на одном не отменяет
     * остальных: общий try обрывал приглашение на первой же ошибке, а человек
     * видел один текст и не знал, кто в итоге позван. Поэтому — поимённо, кто
     * не прошёл. Ответ `pending` (тот, кто выходил, должен согласиться сам)
     * называем отдельно: молчаливое «готово» обмануло бы — его в группе нет.
     */
    fun inviteFriends(userIds: Set<Long>, onDone: () -> Unit) {
        val detail = (_room.value as? UiState.Content)?.value ?: return
        viewModelScope.launch {
            val names = _friends.value.associate { it.user.id to it.user.displayName }
            fun name(id: Long) = names[id] ?: id.toString()

            val pending = mutableListOf<String>()
            val added = mutableListOf<String>()
            val failed = mutableListOf<String>()
            var invited = 0
            userIds.forEach { id ->
                try {
                    if (repository.addMember(detail.id, id) == InviteStatus.PENDING) {
                        pending += name(id)
                    } else {
                        added += name(id)
                    }
                    invited++
                } catch (e: ApiException) {
                    failed += name(id)
                }
            }

            if (invited > 0) {
                sessionStore.noteDataChanged()
                refresh()
            }
            // Сбои важнее ожидания согласия: они требуют повтора, а pending — нет.
            // Успех тоже говорим вслух: раньше шит просто закрывался, и человек
            // не знал, добавил он кого-то или нет
            _alertMessage.value = when {
                failed.isNotEmpty() ->
                    UiText.res(R.string.invite_friends_failed, failed.joinToString(", "))
                pending.isNotEmpty() ->
                    UiText.res(R.string.invite_friends_pending, pending.joinToString(", "))
                added.isNotEmpty() ->
                    UiText.res(R.string.invite_friends_added, added.joinToString(", "))
                else -> null
            }
            // Шит закрываем, только если приглашены все: иначе человеку нужно
            // видеть список и повторить для тех, кто не прошёл.
            if (failed.isEmpty()) onDone()
        }
    }

    /**
     * Выйти из группы.
     *
     * Сервер отклонит выход, пока на человеке висят расходы (409
     * has_operations) или пока он последний участник (`last_member`). Тексты
     * обоих отказов — свои, из ресурсов (`humanErrorText`): серверный `message`
     * всегда по-русски и не годится немцу с испанцем.
     */
    fun leaveRoom(onDone: () -> Unit) {
        val detail = (_room.value as? UiState.Content)?.value ?: return
        viewModelScope.launch {
            try {
                repository.leaveRoom(detail.id)
                sessionStore.noteDataChanged()
                sessionStore.confirm(UiText.res(R.string.toast_left_group))
                onDone()
            } catch (e: ApiException) {
                _alertMessage.value = humanErrorText(e)
            }
        }
    }

    /**
     * Убрать участника — лекарство от «позвал не того».
     *
     * `isSelf = false`: отказ `has_operations` здесь про ДРУГОГО человека, и текст
     * «уберите себя из расходов» отправлял бы искать свои расходы вместо его.
     */
    fun removeMember(userId: Long) {
        val detail = (_room.value as? UiState.Content)?.value ?: return
        viewModelScope.launch {
            try {
                val removed = detail.members.firstOrNull { it.id == userId }?.displayName
                repository.removeMember(detail.id, userId)
                sessionStore.noteDataChanged()
                if (removed != null) {
                    sessionStore.confirm(UiText.res(R.string.toast_member_removed, removed))
                }
                refresh()
            } catch (e: ApiException) {
                _alertMessage.value = humanErrorText(e, isSelf = false)
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
                _alertMessage.value = humanErrorText(e)
            } finally {
                _isArchiving.value = false
            }
        }
    }

    /**
     * Открытая группа прочитана: гасим счётчик на её карточке в списке.
     *
     * Отправляем `seenThrough` ИЗ ОТВЕТА, а не своё «сейчас», — иначе погас бы
     * и расход, добавленный между ответом и отметкой. Комнату из кеша не
     * отмечаем (её `seenThrough` описывает прошлый визит, да и запрос офлайн
     * всё равно не уйдёт).
     *
     * Best-effort и молча: человек этого не просил, алерт поверх открытой
     * группы был бы шумом. Список групп перечитается сам — экран списка
     * грузится заново при возврате (порт iOS GroupDetailViewModel.markSeen).
     */
    private suspend fun markSeen(detail: RoomDetail) {
        val through = detail.seenThrough ?: return
        try {
            repository.markRoomSeen(detail.id, through)
        } catch (e: CancellationException) {
            throw e
        } catch (_: ApiException) {
            // счётчик просто погаснет при следующем открытии группы
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
            val fetched = repository.room(roomId)
            val detail = fetched.value
            _room.value = UiState.Content(detail)
            _sections.value = groupOperationsByMonth(detail.operations)
            _selectedCurrencyOverride.value = null // источник истины снова сервер
            if (!fetched.fromCache) markSeen(detail)
            _freshness.value = if (fetched.fromCache) {
                _freshness.value.copy(fromCache = true)
            } else {
                DataFreshness(fromCache = false, updatedAt = Instant.now())
            }

        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            if (_room.value is UiState.Content) {
                _alertMessage.value = humanErrorText(e)
            } else {
                _room.value = UiState.Error(e.message)
            }
        }
    }
}
