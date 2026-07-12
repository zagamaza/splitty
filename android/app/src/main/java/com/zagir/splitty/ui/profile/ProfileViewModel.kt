package com.zagir.splitty.ui.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxStore
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
 * VM вкладки «Профиль»: профиль из кэша сессии + актуализация GET /me,
 * правки настроек PATCH /me, адрес сервера и выход.
 * Порт ios/Splitty/Features/Account/AccountView.swift (логика).
 */
@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    outboxStore: OutboxStore,
) : ViewModel() {

    /** Профиль текущего пользователя (кэш сессии, обновляется после GET/PATCH /me). */
    val me: StateFlow<Me?> = sessionStore.state
        .map { it?.me }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me)

    /** Текущий адрес сервера — для карточки «Сервер». */
    /** Тема приложения (system/light/dark) — строка «Тема» в настройках. */
    val theme: StateFlow<String> = sessionStore.state
        .map { it?.theme ?: SessionStore.THEME_SYSTEM }
        .stateIn(viewModelScope, SharingStarted.Eagerly, SessionStore.THEME_SYSTEM)

    fun onThemeSelected(theme: String) {
        viewModelScope.launch { sessionStore.setTheme(theme) }
    }

    val baseUrl: StateFlow<String> = sessionStore.state
        .map { it?.baseUrl ?: SessionStore.DEFAULT_BASE_URL }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.currentBaseUrl())

    private val _errorMessage = MutableStateFlow<String?>(null)
    val errorMessage: StateFlow<String?> = _errorMessage.asStateFlow()

    private val _isSaving = MutableStateFlow(false)
    val isSaving: StateFlow<Boolean> = _isSaving.asStateFlow()

    /**
     * Сколько офлайн-операций ещё не отправлено: при > 0 подтверждение выхода
     * предупреждает, что они будут удалены (кеш и outbox чистятся при logout).
     */
    val pendingOutboxCount: StateFlow<Int> = outboxStore.entries
        .map { it.size }
        .stateIn(viewModelScope, SharingStarted.Eagerly, outboxStore.entries.value.size)

    init {
        // Актуализация профиля при открытии вкладки; ошибка тиха — показываем кэш.
        viewModelScope.launch {
            try {
                sessionStore.updateMe(repository.me().value)
            } catch (_: ApiException) {
                // Кэш профиля уже показан — сетевые проблемы не критичны.
            }
        }
    }

    /**
     * PATCH /me: null-поля не отправляются. При успехе профиль обновляется
     * в сессии; смена имени видна в группах — инвалидируем списки.
     */
    fun updateProfile(
        displayName: String? = null,
        lang: String? = null,
        notificationOn: Boolean? = null,
    ) {
        if (_isSaving.value) return
        viewModelScope.launch {
            _isSaving.value = true
            try {
                val updated = repository.updateMe(
                    displayName = displayName,
                    lang = lang,
                    notificationOn = notificationOn,
                )
                sessionStore.updateMe(updated)
                if (displayName != null) {
                    sessionStore.noteDataChanged()
                }
            } catch (e: ApiException) {
                // Экран откатит локальные значения к профилю (см. ProfileScreen).
                _errorMessage.value = e.message
            } finally {
                _isSaving.value = false
            }
        }
    }

    /** Выход: чистит токен и профиль — AppRoot сам покажет LoginScreen. */
    fun logout() {
        viewModelScope.launch { sessionStore.logout() }
    }

    fun dismissError() {
        _errorMessage.value = null
    }

    /** Локальная ошибка валидации (пустое имя) — в тот же alert. */
    fun showError(message: String) {
        _errorMessage.value = message
    }
}
