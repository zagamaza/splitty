package com.zagir.splitty.ui.groups

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Statistics
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * VM дашборда «Итоги»: GET /rooms/{id}/statistics; перезагрузка —
 * по dataVersion (новый расход сразу меняет графики).
 */
@HiltViewModel
class GroupDashboardViewModel @Inject constructor(
    private val repository: SplittyRepository,
    sessionStore: SessionStore,
) : ViewModel() {

    private val roomIdFlow = MutableStateFlow<String?>(null)

    private val _statistics = MutableStateFlow<UiState<Statistics>>(UiState.Loading)

    /** Статистика группы; все суммы — в statistics.currency. */
    val statistics: StateFlow<UiState<Statistics>> = _statistics.asStateFlow()

    /** id текущего пользователя для плиток «Я заплатил»/«Моя доля». */
    val meId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    init {
        viewModelScope.launch {
            combine(roomIdFlow.filterNotNull(), sessionStore.dataVersion) { id, _ -> id }
                .collectLatest { id -> load(id) }
        }
    }

    /** Привязка к комнате; повторные вызовы с тем же id — no-op. */
    fun start(roomId: String) {
        if (roomIdFlow.value != roomId) roomIdFlow.value = roomId
    }

    /** Повтор после ошибки загрузки. */
    fun retry() {
        val id = roomIdFlow.value ?: return
        _statistics.value = UiState.Loading
        viewModelScope.launch { load(id) }
    }

    private suspend fun load(roomId: String) {
        try {
            _statistics.value = UiState.Content(repository.statistics(roomId).value)
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            if (_statistics.value !is UiState.Content) {
                _statistics.value = UiState.Error(e.message)
            }
        }
    }
}
