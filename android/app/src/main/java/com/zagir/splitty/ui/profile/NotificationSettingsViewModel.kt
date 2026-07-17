package com.zagir.splitty.ui.profile

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * Состояние экрана «Уведомления»: мастер-тумблер (из профиля, PATCH /me),
 * категории событий (PATCH /me/notifications) и служебные флаги. Переходы —
 * чистые методы, покрыты юнит-тестами (порт логики iOS NotificationSettingsView).
 */
data class NotifyScreenState(
    /** Категории; null пока идёт первичная загрузка. */
    val settings: NotifySettings? = null,
    /** Мастер-тумблер: локальная копия `me.notificationOn`. */
    val masterOn: Boolean = true,
    /** true — PATCH в полёте; тумблеры задизейблены, чтобы быстрые тапы не
     *  порождали гонку запросов (последний ответ сервера побеждал бы). */
    val isSaving: Boolean = false,
    /** Полноэкранная ошибка первичной загрузки (когда [settings] == null). */
    val loadError: String? = null,
    /** Алерт поверх загруженной формы — ошибка сохранения (не молчаливый откат). */
    val alertMessage: String? = null,
) {
    /** Спиннер: ещё грузим и не упали. */
    val isLoading: Boolean get() = settings == null && loadError == null

    /** Категории действуют только при включённом мастере и вне сохранения. */
    val categoriesEnabled: Boolean get() = masterOn && !isSaving

    // --- Чистые переходы (тестируются без Android/сети) ---

    /** Оптимистично применить мастер и войти в «сохранение». */
    fun applyMaster(on: Boolean): NotifyScreenState =
        copy(masterOn = on, isSaving = true)

    /** Мастер сохранён сервером. */
    fun masterSaved(on: Boolean): NotifyScreenState =
        copy(masterOn = on, isSaving = false)

    /** Сохранение мастера упало: откат к прежнему значению + алерт. */
    fun masterFailed(previous: Boolean, message: String?): NotifyScreenState =
        copy(masterOn = previous, isSaving = false, alertMessage = message)

    /** Оптимистично применить категории и войти в «сохранение». */
    fun applyCategories(updated: NotifySettings): NotifyScreenState =
        copy(settings = updated, isSaving = true)

    /** Категории сохранены сервером. */
    fun categoriesSaved(saved: NotifySettings): NotifyScreenState =
        copy(settings = saved, isSaving = false)

    /** Сохранение категорий упало: откат к прежним + алерт. */
    fun categoriesFailed(previous: NotifySettings?, message: String?): NotifyScreenState =
        copy(settings = previous, isSaving = false, alertMessage = message)
}

/**
 * VM экрана «Уведомления»: GET /me/notifications при старте; мастер-тумблер
 * оптимистично PATCH /me, категории — PATCH /me/notifications. При ошибке —
 * откат к прежнему значению и алерт (раньше откат был молчаливым).
 */
@HiltViewModel
class NotificationSettingsViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
) : ViewModel() {

    private val _state = MutableStateFlow(
        NotifyScreenState(masterOn = sessionStore.state.value?.me?.notificationOn ?: true)
    )
    val state: StateFlow<NotifyScreenState> = _state.asStateFlow()

    init {
        viewModelScope.launch { load() }
    }

    fun retry() {
        _state.update { it.copy(settings = null, loadError = null) }
        viewModelScope.launch { load() }
    }

    fun dismissAlert() {
        _state.update { it.copy(alertMessage = null) }
    }

    /**
     * Мастер-тумблер (PATCH /me). Каскад: пока сохраняется — категории
     * задизейблены; при ошибке откат к прежнему значению профиля + алерт.
     * No-op, если значение совпадает с профилем (лишний PATCH не нужен).
     */
    fun setMaster(on: Boolean) {
        val current = _state.value
        val previous = sessionStore.state.value?.me?.notificationOn ?: current.masterOn
        if (on == previous || current.isSaving) return
        _state.value = current.applyMaster(on)
        viewModelScope.launch {
            try {
                val updated = repository.updateMe(notificationOn = on)
                sessionStore.updateMe(updated)
                _state.update { it.masterSaved(updated.notificationOn) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                _state.update { it.masterFailed(previous, e.message) }
            }
        }
    }

    /** Оптимистичное сохранение категорий: при ошибке откат + алерт. */
    fun saveCategories(updated: NotifySettings) {
        val current = _state.value
        val previous = current.settings ?: return
        if (current.isSaving) return
        _state.value = current.applyCategories(updated)
        viewModelScope.launch {
            try {
                _state.update { it.categoriesSaved(repository.updateNotifications(updated)) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: ApiException) {
                _state.update { it.categoriesFailed(previous, e.message) }
            }
        }
    }

    private suspend fun load() {
        try {
            val loaded = repository.notifications()
            _state.update { it.copy(settings = loaded, loadError = null) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            _state.update { it.copy(loadError = e.message) }
        }
    }
}
