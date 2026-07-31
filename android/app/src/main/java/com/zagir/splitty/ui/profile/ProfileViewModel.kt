package com.zagir.splitty.ui.profile

import android.content.Context
import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.auth.GoogleSignInException
import com.zagir.splitty.core.model.LoginProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PushTokenRegistrar
import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.ui.components.identityErrorText
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
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
private const val TAG = "ProfileViewModel"

@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val pushTokenRegistrar: PushTokenRegistrar,
    private val googleIdTokenProvider: GoogleIdTokenProvider,
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
        viewModelScope.launch {
            // setTheme пишет в DataStore и бросает IOException, а не ApiException:
            // необработанным он убивал процесс из viewModelScope (тот же фикс уже
            // сделан в LoginViewModel и NotificationSettingsViewModel).
            try {
                sessionStore.setTheme(theme)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _errorMessage.value = humanErrorText(e)
            }
        }
    }

    val baseUrl: StateFlow<String> = sessionStore.state
        .map { it?.baseUrl ?: SessionStore.DEFAULT_BASE_URL }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.currentBaseUrl())

    private val _errorMessage = MutableStateFlow<String?>(null)
    val errorMessage: StateFlow<String?> = _errorMessage.asStateFlow()

    private val _isSaving = MutableStateFlow(false)
    val isSaving: StateFlow<Boolean> = _isSaving.asStateFlow()

    /**
     * Предупреждение сервера (отвязка Telegram) — отдельный диалог, а не
     * [errorMessage]: это не ошибка, но и не «просто получилось», и молча
     * проглатывать его нельзя — там про потерю групп в боте.
     */
    private val _noticeMessage = MutableStateFlow<String?>(null)
    val noticeMessage: StateFlow<String?> = _noticeMessage.asStateFlow()

    /** Идёт привязка или отвязка способа входа — кнопки секции гаснут. */
    private val _isIdentityBusy = MutableStateFlow(false)
    val isIdentityBusy: StateFlow<Boolean> = _isIdentityBusy.asStateFlow()

    /** Идёт удаление аккаунта — кнопка гаснет и показывает прогресс. */
    private val _isDeleting = MutableStateFlow(false)
    val isDeleting: StateFlow<Boolean> = _isDeleting.asStateFlow()

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
            } catch (e: CancellationException) {
                throw e
            } catch (_: Throwable) {
                // Кэш профиля уже показан — сетевые проблемы не критичны.
                // Ловим Throwable, а не ApiException: updateMe пишет в DataStore
                // и бросает IOException, который из viewModelScope убивал процесс
                // (тот же фикс, что в onThemeSelected выше).
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
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // Экран откатит локальные значения к профилю (см. ProfileScreen).
                // Ловим Throwable, а не только ApiException: updateMe пишет
                // в DataStore и роняет процесс своим IOException.
                _errorMessage.value = humanErrorText(e)
            } finally {
                _isSaving.value = false
            }
        }
    }

    /**
     * Привязка Google к текущему аккаунту: системный лист Credential Manager →
     * id-токен → POST /me/link/google. Профиль в сессии обновляется ОТВЕТОМ
     * сервера — `linkedProviders` приезжает оттуда, а не досочиняется здесь.
     *
     * [activityContext] — контекст активити: лист рисуется поверх неё.
     * Отмена (провайдер вернул null) молчит: человек и так знает, что закрыл лист.
     */
    fun linkGoogle(activityContext: Context) {
        if (_isIdentityBusy.value) return
        _isIdentityBusy.value = true
        viewModelScope.launch {
            try {
                val idToken = googleIdTokenProvider.idToken(activityContext) ?: return@launch
                sessionStore.updateMe(repository.linkGoogle(idToken).user)
            } catch (e: CancellationException) {
                throw e
            } catch (e: GoogleSignInException) {
                _errorMessage.value = e.message
            } catch (e: Throwable) {
                // Throwable, а не ApiException: updateMe пишет в DataStore и
                // бросает IOException мимо обёртки репозитория — из
                // viewModelScope он убивал бы процесс (см. onThemeSelected).
                Log.e(TAG, "link google failed", e)
                _errorMessage.value = identityErrorText(e)
            } finally {
                _isIdentityBusy.value = false
            }
        }
    }

    /**
     * Отвязка способа входа: DELETE /me/link/{provider}. Экран не пускает сюда
     * последний способ (кнопка гаснет), 409 `last_identity` — вторая линия.
     */
    fun unlink(provider: LoginProvider) {
        if (_isIdentityBusy.value) return
        _isIdentityBusy.value = true
        viewModelScope.launch {
            try {
                val response = repository.unlinkProvider(provider)
                sessionStore.updateMe(response.user)
                response.warning?.takeIf { it.isNotBlank() }?.let { _noticeMessage.value = it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                Log.e(TAG, "unlink ${provider.id} failed", e)
                _errorMessage.value = identityErrorText(e)
            } finally {
                _isIdentityBusy.value = false
            }
        }
    }

    /**
     * Удаление аккаунта: DELETE /me и полный разлогин ТОЛЬКО при успехе.
     *
     * При сетевой ошибке аккаунт жив, и выбрасывать человека на экран входа
     * значило бы соврать ему, что удаление прошло. Локальные данные (кеш,
     * outbox, аватары, отложенное вступление по ссылке) стирает существующий
     * [com.zagir.splitty.data.OfflineDataCleaner] по пропаже токена — второй
     * копии чистки не заводим.
     */
    fun deleteAccount() {
        if (_isDeleting.value) return
        _isDeleting.value = true
        viewModelScope.launch {
            try {
                // Отвязать FCM-токен, ПОКА JWT ещё валиден: после tombstone
                // сервер отвергнет запрос, и токен устройства остался бы висеть.
                // runCatching: отвязка best-effort по своему контракту, а сбой
                // Firebase (не инициализирован, нет Play Services) не имеет
                // права отменить удаление аккаунта — его требует Google Play.
                runCatching { pushTokenRegistrar.unregisterCurrent() }
                    .onFailure { Log.w(TAG, "unregister push token failed", it) }
                repository.deleteAccount()
                sessionStore.logout()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                Log.e(TAG, "delete account failed", e)
                _errorMessage.value = humanErrorText(e)
            } finally {
                _isDeleting.value = false
            }
        }
    }

    fun dismissNotice() {
        _noticeMessage.value = null
    }

    /** Выход: чистит токен и профиль — AppRoot сам покажет LoginScreen. */
    fun logout() {
        viewModelScope.launch {
            try {
                // Отвязать FCM-токен ПОКА JWT ещё валиден (best-effort, не блокирует выход).
                pushTokenRegistrar.unregisterCurrent()
                sessionStore.logout()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _errorMessage.value = humanErrorText(e)
            }
        }
    }

    fun dismissError() {
        _errorMessage.value = null
    }

    /** Локальная ошибка валидации (пустое имя) — в тот же alert. */
    fun showError(message: String) {
        _errorMessage.value = message
    }
}
