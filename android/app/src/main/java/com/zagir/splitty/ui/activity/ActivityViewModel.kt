package com.zagir.splitty.ui.activity

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ActivityItem
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
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * VM вкладки «Активность»: лента операций всех групп (GET /activity)
 * с пагинацией offset/limit по скроллу. Первичная загрузка и перезагрузка
 * первой страницы после каждой мутации — по [SessionStore.dataVersion].
 * Порт ios/Splitty/Features/Activity/ActivityViewModel.swift.
 */
@HiltViewModel
class ActivityViewModel @Inject constructor(
    private val repository: SplittyRepository,
    sessionStore: SessionStore,
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
    private val _errorMessage = MutableStateFlow<UiText?>(null)
    val errorMessage: StateFlow<UiText?> = _errorMessage.asStateFlow()

    /** id текущего пользователя — для позиций «Вы одолжили/должны/получили». */
    val myUserId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    /** Фильтр «Только мои»: операции, где я донор или в получателях. */
    private val _isMineOnly = MutableStateFlow(false)
    val isMineOnly: StateFlow<Boolean> = _isMineOnly.asStateFlow()

    /** Лента с учётом фильтра «Только мои». */
    val displayItems: StateFlow<UiState<List<ActivityItem>>> =
        combine(_state, _isMineOnly, myUserId) { state, mineOnly, meId ->
            if (!mineOnly || meId == null || state !is UiState.Content) {
                state
            } else {
                UiState.Content(state.value.filter { it.operation.involves(meId) })
            }
        }.stateIn(viewModelScope, SharingStarted.Eagerly, UiState.Loading)

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
        // Порог — по ВИДИМОМУ списку: с фильтром «Только мои» конец исходного
        // списка по индексам отфильтрованных строк иначе никогда не наступает.
        val items = (displayItems.value as? UiState.Content)?.value ?: return
        if (!hasMore || _isLoadingMore.value) return
        if (index < items.size - PREFETCH_THRESHOLD) return
        viewModelScope.launch { loadMore() }
    }

    fun dismissError() {
        _errorMessage.value = null
    }

    /**
     * Переключение фильтра. После включения отфильтрованных строк может быть
     * меньше страницы — добираем следующие страницы (максимум несколько за
     * раз, чтобы не выкачать всю историю).
     */
    fun toggleMineOnly() {
        _isMineOnly.value = !_isMineOnly.value
        if (!_isMineOnly.value) return
        viewModelScope.launch {
            var attempts = 0
            while (hasMore && !_isLoadingMore.value && attempts < 5 &&
                filteredCount() < PAGE_SIZE
            ) {
                attempts++
                loadMore()
            }
        }
    }

    private fun filteredCount(): Int {
        val content = (displayItems.value as? UiState.Content)?.value ?: return 0
        return content.size
    }

    private suspend fun reloadFirstPage() {
        try {
            val page = repository.activity(limit = PAGE_SIZE, offset = 0).value
            generation++ // подгрузки, стартовавшие до этого момента, свой результат выбросят
            _state.value = UiState.Content(page)
            loadedCount = page.size
            hasMore = page.size == PAGE_SIZE
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
