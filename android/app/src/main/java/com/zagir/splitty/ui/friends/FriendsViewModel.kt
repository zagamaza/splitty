package com.zagir.splitty.ui.friends

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * VM вкладки «Друзья»: список друзей с нетто-балансами (GET /friends).
 * Первичная загрузка и перезагрузка после каждой мутации — через подписку
 * на [SessionStore.dataVersion] (первое значение приходит сразу).
 */
@HiltViewModel
class FriendsViewModel @Inject constructor(
    private val repository: SplittyRepository,
    sessionStore: SessionStore,
    private val outboxSyncer: OutboxSyncer,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<List<FriendBalance>>>(UiState.Loading)
    val state: StateFlow<UiState<List<FriendBalance>>> = _state.asStateFlow()

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    /** Ошибка обновления, когда список уже показан (alert поверх контента). */
    private val _errorMessage = MutableStateFlow<UiText?>(null)
    val errorMessage: StateFlow<UiText?> = _errorMessage.asStateFlow()

    init {
        viewModelScope.launch {
            sessionStore.dataVersion.collect { reload() }
        }
    }

    /** Повтор после ошибки первичной загрузки. */
    fun retry() {
        _state.value = UiState.Loading
        viewModelScope.launch { reload() }
    }

    /** Pull-to-refresh: обновление без скрытия списка. */
    fun refresh() {
        outboxSyncer.syncNow() // pull-to-refresh — один из триггеров досылки outbox
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                reload()
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    fun dismissError() {
        _errorMessage.value = null
    }

    private suspend fun reload() {
        try {
            _state.value = UiState.Content(repository.friends().value)
        } catch (e: ApiException) {
            // Контент уже есть — тихая ошибка в alert; нет — полноэкранная.
            if (_state.value is UiState.Content) {
                _errorMessage.value = humanErrorText(e)
            } else {
                _state.value = UiState.Error(e.message)
            }
        }
    }
}
