package com.zagir.splitty.ui.activity

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.model.InviteCard
import com.zagir.splitty.core.model.InviteStatus
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * VM вкладки «Уведомления»: лента операций всех групп (GET /activity)
 * с пагинацией offset/limit по скроллу. Первичная загрузка и перезагрузка
 * первой страницы после каждой мутации — по [SessionStore.dataVersion].
 * Порт ios/Splitty/Features/Activity/ActivityViewModel.swift.
 *
 * Лента отдаётся экрану как есть: раздел стал входящими, счётчик
 * непрочитанного на сервере считает адресованное вам (`notifiesUser`), и
 * фильтр «Только мои», переключавший ленту между «мне» и «всё подряд»,
 * этому противоречил.
 */
@HiltViewModel
class ActivityViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val outboxSyncer: OutboxSyncer,
) : ViewModel() {

    companion object {
        private const val PAGE_SIZE = 30

        /** За сколько строк до конца списка начинать подгрузку следующей страницы. */
        private const val PREFETCH_THRESHOLD = 5
    }

    private val _state = MutableStateFlow<UiState<List<ActivityItem>>>(UiState.Loading)
    val state: StateFlow<UiState<List<ActivityItem>>> = _state.asStateFlow()

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    private val _isLoadingMore = MutableStateFlow(false)
    val isLoadingMore: StateFlow<Boolean> = _isLoadingMore.asStateFlow()

    /** Ошибка обновления/подгрузки, когда лента уже показана (alert). */
    /** Закреплённые карточки приглашений над лентой. */
    private val _invites = MutableStateFlow<List<InviteCard>>(emptyList())
    val invites: StateFlow<List<InviteCard>> = _invites.asStateFlow()

    /** Время формирования последнего ответа — его же шлём при отметке
     *  прочитанного, чтобы не погасить пришедшее позже. */
    private var seenThrough: java.time.Instant? = null

    /** Счётчик из последнего ответа: нечего гасить — незачем и ходить на сервер. */
    private var unreadCount = 0

    /**
     * Экран на виду. Отметка прочитанного зависит от ДВУХ событий — открытия
     * раздела и прихода ленты, — и первый визит всегда наступает раньше
     * второго: `seenThrough` берётся из ответа, до него отмечать нечего.
     * Поэтому оба пути ведут в [markSeen], а флаг связывает их.
     */
    private var isScreenVisible = false

    private val _errorMessage = MutableStateFlow<UiText?>(null)
    val errorMessage: StateFlow<UiText?> = _errorMessage.asStateFlow()

    /** id текущего пользователя — для позиций «Вы одолжили/должны/получили». */
    val myUserId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    private var hasMore = true

    /**
     * Сколько строк реально отдал сервер. Именно это, а не размер списка в UI —
     * корректный offset: дубликаты (новые операции сверху сдвигают окно) из
     * списка выбрасываются, и offset по его размеру отставал. Когда сверху
     * появлялась целая страница новых операций, следующая страница приходила
     * полностью дублирующей, размер не менялся, hasMore оставался true — и тот
     * же offset запрашивался бесконечно.
     */
    private var loadedCount = 0

    /** Поколение ленты: подгрузка, стартовавшая до refresh, не должна вернуть старый список. */
    private var generation = 0

    init {
        viewModelScope.launch {
            sessionStore.dataVersion.collect { reloadFirstPage() }
        }
    }

    /** Повтор после ошибки первичной загрузки. */
    fun retry() {
        _state.value = UiState.Loading
        viewModelScope.launch { reloadFirstPage() }
    }

    /** Pull-to-refresh: перезагрузка первой страницы без скрытия ленты. */
    fun refresh() {
        outboxSyncer.syncNow() // pull-to-refresh — один из триггеров досылки outbox
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                reloadFirstPage()
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    /**
     * Зовётся при показе строки с индексом [index] (snapshotFlow по
     * LazyListState): у конца списка подгружает следующую страницу.
     */
    fun onItemShown(index: Int) {
        val items = (_state.value as? UiState.Content)?.value ?: return
        if (!hasMore || _isLoadingMore.value) return
        if (index < items.size - PREFETCH_THRESHOLD) return
        viewModelScope.launch { loadMore() }
    }

    fun dismissError() {
        _errorMessage.value = null
    }

    /**
     * Раздел открыт — значит человек всё это увидел.
     *
     * Перезагружаем ленту, а не просто отмечаем прочитанное: VM переживает
     * переключение табов (`saveState`/`restoreState`), и на повторном входе
     * `seenThrough` с `unreadCount` остались бы от прошлого визита. Пришедшее
     * в фоне приглашение не показалось бы вовсе, а бейдж, поднятый обновлением
     * счётчика на старте, гасить было бы нечем — `markSeen` уходил бы в no-op.
     * Отметку пошлёт сам [reloadFirstPage], когда приедет ответ.
     */
    fun onScreenVisible() {
        isScreenVisible = true
        viewModelScope.launch { reloadFirstPage() }
    }

    fun onScreenHidden() {
        isScreenVisible = false
    }

    /** Отметить прочитанным всё, что было в последнем ответе. */
    fun markSeen() {
        val through = seenThrough ?: return
        if (unreadCount == 0) return
        viewModelScope.launch {
            // Фоновое действие: сбой не должен ничем мигать пользователю.
            runCatching { repository.markNotificationsSeen(through) }
            // Бейдж гасим только после подтверждённой отметки: иначе непрочитанное
            // исчезло бы с таба, оставшись непрочитанным на сервере.
                .onSuccess {
                    // Не ноль: `pending`-приглашения сервер считает непрочитанными,
                    // пока на них не ответили, — обнулив бейдж, мы бы разошлись
                    // с ним до следующего обновления (паритет с iOS).
                    unreadCount = _invites.value.count { it.status == InviteStatus.PENDING }
                    sessionStore.setUnreadNotifications(unreadCount)
                }
        }
    }

    fun acceptInvite(card: InviteCard) = actOnInvite(card) { repository.acceptInvite(card.roomId) }

    fun declineInvite(card: InviteCard) = actOnInvite(card) { repository.declineInvite(card.roomId) }

    /**
     * Выйти из группы прямо с карточки «вас добавили».
     *
     * Кнопка обязана быть здесь: человека добавили, не спросив, и если
     * единственное действие — «Открыть», отказаться можно только разыскав
     * настройки группы.
     */
    fun leaveFromCard(card: InviteCard) = actOnInvite(card) { repository.leaveRoom(card.roomId) }

    private fun actOnInvite(card: InviteCard, action: suspend () -> Unit) {
        viewModelScope.launch {
            try {
                action()
                _invites.value = _invites.value.filterNot { it.roomId == card.roomId }
                // Не только своя лента: принятое приглашение добавляет группу, а
                // выход убирает. Списки групп и друзей перезагружаются лишь по
                // dataVersion, а вкладки переживают переключение — без этого
                // «Группы» показывали бы старое до pull-to-refresh.
                sessionStore.noteDataChanged()
                reloadFirstPage()
            } catch (e: ApiException) {
                _errorMessage.value = humanErrorText(e)
            }
        }
    }

    private suspend fun reloadFirstPage() {
        try {
            val fetched = repository.notificationFeed(limit = PAGE_SIZE, offset = 0)
            val feed = fetched.value
            val page = feed.items
            generation++ // подгрузки, стартовавшие до этого момента, свой результат выбросят
            _invites.value = feed.invites
            seenThrough = feed.seenThrough
            unreadCount = feed.unreadCount
            sessionStore.setUnreadNotifications(feed.unreadCount)
            _state.value = UiState.Content(page)
            loadedCount = page.size
            // Из кеша листать некуда: следующая страница есть только на сервере,
            // и попытка её взять офлайн заканчивалась алертом «нет соединения»
            // поверх нормально показанной ленты (порт iOS ActivityViewModel).
            hasMore = !fetched.fromCache && page.size == PAGE_SIZE
            // Первый визит: экран открылся раньше, чем приехал ответ, и отметить
            // прочитанное до этой строки было нечем.
            if (isScreenVisible) markSeen()
        } catch (e: ApiException) {
            if (_state.value is UiState.Content) {
                _errorMessage.value = humanErrorText(e)
            } else {
                _state.value = UiState.Error(e.message)
            }
        }
    }

    private suspend fun loadMore() {
        if (!hasMore || _isLoadingMore.value) return
        val current = (_state.value as? UiState.Content)?.value ?: return
        val startedAt = generation
        _isLoadingMore.value = true
        try {
            val page = repository.activity(limit = PAGE_SIZE, offset = loadedCount).value
            if (startedAt != generation) return // лента перезагрузилась — страница устарела
            val base = (_state.value as? UiState.Content)?.value ?: current
            // Страховка от дублей при сдвиге offset (новые операции сверху).
            val known = base.mapTo(HashSet()) { it.operation.id }
            val fresh = page.filter { it.operation.id !in known }
            _state.value = UiState.Content(base + fresh)
            loadedCount += page.size
            // Страница целиком из дублей означает, что вниз двигаться некуда:
            // без этой отсечки цикл повторял бы один и тот же запрос.
            hasMore = page.size == PAGE_SIZE && fresh.isNotEmpty()
        } catch (e: ApiException) {
            _errorMessage.value = humanErrorText(e)
        } finally {
            _isLoadingMore.value = false
        }
    }
}
