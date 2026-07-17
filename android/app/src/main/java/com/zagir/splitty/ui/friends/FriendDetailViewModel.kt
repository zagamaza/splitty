package com.zagir.splitty.ui.friends

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * VM экрана друга: свежий [FriendBalance] — GET /friends + поиск по id
 * (nav-аргумент `userId`; `name` — фолбэк имени, если друга уже нет в списке:
 * все долги погашены). Перезагрузка — по [SessionStore.dataVersion].
 */
@HiltViewModel
class FriendDetailViewModel @Inject constructor(
    private val repository: SplittyRepository,
    sessionStore: SessionStore,
    networkMonitor: NetworkMonitor,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    /** Онлайн-статус — гейт CTA «Погасить» (офлайн — алерт, как iOS). */
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline

    private val userId: Long = checkNotNull(savedStateHandle["userId"]) {
        "FriendDetail требует nav-аргумент userId"
    }
    private val fallbackName: String = savedStateHandle.get<String>("name").orEmpty()

    private val _state = MutableStateFlow<UiState<FriendBalance>>(UiState.Loading)
    val state: StateFlow<UiState<FriendBalance>> = _state.asStateFlow()

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    /** Ошибка обновления, когда контент уже показан (alert). */
    private val _errorMessage = MutableStateFlow<String?>(null)
    val errorMessage: StateFlow<String?> = _errorMessage.asStateFlow()

    init {
        viewModelScope.launch {
            sessionStore.dataVersion.collect { reload() }
        }
    }

    fun retry() {
        _state.value = UiState.Loading
        viewModelScope.launch { reload() }
    }

    fun refresh() {
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

    /** Тап «Погасить» без сети: погашение доступно только онлайн (алерт, как iOS). */
    fun showOfflineSettleError() {
        _errorMessage.value = "Нет соединения. Погашение долга доступно только онлайн"
    }

    private suspend fun reload() {
        try {
            val friends = repository.friends().value
            val known = (_state.value as? UiState.Content)?.value?.user
            val friend = friends.firstOrNull { it.user.id == userId }
                // Друга больше нет в списке — все долги погашены.
                ?: FriendBalance(
                    user = known ?: User(id = userId, username = null, displayName = fallbackName),
                )
            _state.value = UiState.Content(friend)
        } catch (e: ApiException) {
            if (_state.value is UiState.Content) {
                _errorMessage.value = e.message
            } else {
                _state.value = UiState.Error(e.message)
            }
        }
    }
}
