package com.zagir.splitty.ui.activity

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
    private val _errorMessage = MutableStateFlow<String?>(null)
    val errorMessage: StateFlow<String?> = _errorMessage.asStateFlow()

    /** id текущего пользователя — для позиций «Вы одолжили/должны/получили». */
    val myUserId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    private var hasMore = true

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

    private suspend fun reloadFirstPage() {
        try {
            val page = repository.activity(limit = PAGE_SIZE, offset = 0).value
            _state.value = UiState.Content(page)
            hasMore = page.size == PAGE_SIZE
        } catch (e: ApiException) {
            if (_state.value is UiState.Content) {
                _errorMessage.value = e.message
            } else {
                _state.value = UiState.Error(e.message)
            }
        }
    }

    private suspend fun loadMore() {
        if (!hasMore || _isLoadingMore.value) return
        val current = (_state.value as? UiState.Content)?.value ?: return
        _isLoadingMore.value = true
        try {
            val page = repository.activity(limit = PAGE_SIZE, offset = current.size).value
            // Страховка от дублей при сдвиге offset (новые операции сверху).
            val known = current.mapTo(HashSet()) { it.operation.id }
            _state.value = UiState.Content(current + page.filter { it.operation.id !in known })
            hasMore = page.size == PAGE_SIZE
        } catch (e: ApiException) {
            _errorMessage.value = e.message
        } finally {
            _isLoadingMore.value = false
        }
    }
}
