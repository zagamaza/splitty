package com.zagir.splitty.ui.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * VM экрана «Уведомления»: GET /me/notifications при старте, оптимистичный
 * PATCH при переключении тумблера (UI сразу, при ошибке — откат).
 */
@HiltViewModel
class NotificationSettingsViewModel @Inject constructor(
    private val repository: SplittyRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<NotifySettings>>(UiState.Loading)
    val state: StateFlow<UiState<NotifySettings>> = _state.asStateFlow()

    init {
        viewModelScope.launch { load() }
    }

    fun retry() {
        _state.value = UiState.Loading
        viewModelScope.launch { load() }
    }

    /** Оптимистичное сохранение: UI обновляется сразу, при ошибке — откат. */
    fun save(updated: NotifySettings) {
        val previous = (_state.value as? UiState.Content)?.value ?: return
        _state.value = UiState.Content(updated)
        viewModelScope.launch {
            try {
                _state.value = UiState.Content(repository.updateNotifications(updated))
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                _state.value = UiState.Content(previous)
            }
        }
    }

    private suspend fun load() {
        try {
            _state.value = UiState.Content(repository.notifications())
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            _state.value = UiState.Error(e.message)
        }
    }
}
