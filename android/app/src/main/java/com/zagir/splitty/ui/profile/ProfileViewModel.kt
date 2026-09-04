package com.zagir.splitty.ui.profile

import com.zagir.splitty.core.analytics.AnalyticsEvent
import com.zagir.splitty.core.analytics.Analytics
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
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

private const val TAG = "ProfileViewModel"

/**
 * VM вкладки «Профиль»: профиль из кэша сессии + актуализация GET /me,
 * правки настроек PATCH /me, адрес сервера и выход.
 * Порт ios/Splitty/Features/Account/AccountView.swift (логика).
 */
@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val pushTokenRegistrar: PushTokenRegistrar,
    private val googleIdTokenProvider: GoogleIdTokenProvider,
    outboxStore: OutboxStore,
    private val analytics: Analytics,
) : ViewModel() {

    /** Экран открыт. Зовётся из composable один раз на вход. */
    fun trackScreen() = analytics.track(AnalyticsEvent.ScreenView("account"))

    /** Профиль текущего пользователя (кэш сессии, обновляется после GET/PATCH /me). */
    val me: StateFlow<Me?> = sessionStore.state
        .map { it?.me }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me)

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
                analytics.track(AnalyticsEvent.SettingsChanged("theme"))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _errorMessage.value = humanErrorText(e)
            }
        }
    }

    /** Текущий адрес сервера — для карточки «Сервер». */
    val baseUrl: StateFlow<String> = sessionStore.state
        .map { it?.baseUrl ?: SessionStore.DEFAULT_BASE_URL }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.currentBaseUrl())

    private val _errorMessage = MutableStateFlow<UiText?>(null)
    val errorMessage: StateFlow<UiText?> = _errorMessage.asStateFlow()

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
                val fetched = repository.me()
                // ТОЛЬКО свежий ответ сети: офлайн-кеш `me` мог быть записан
                // старой версией приложения, и тогда `linkedProviders` в нём
                // разбирается в emptyList(). Записав такой профиль в сессию, мы
                // бы стёрли реальные способы входа: секция показала бы «Google
                // не привязан» и запретила отвязку — до следующего успешного
                // GET /me, которого офлайн не будет.
                if (!fetched.fromCache) {
                    sessionStore.updateMe(fetched.value)
                }
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
        // Что именно правили — из закрытого списка. Один вызов может нести
        // несколько полей, поэтому событие на каждое непустое.
        displayName?.let { analytics.track(AnalyticsEvent.SettingsChanged("name")) }
        lang?.let { analytics.track(AnalyticsEvent.SettingsChanged("language")) }
        notificationOn?.let { analytics.track(AnalyticsEvent.SettingsChanged("notifications")) }
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
                analytics.track(AnalyticsEvent.AccountLinked("google"))
            } catch (e: CancellationException) {
                throw e
            } catch (e: GoogleSignInException) {
                _errorMessage.value = humanErrorText(e)
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
     * Задать или сменить пароль: POST /me/password. [current] нужен, только
     * если пароль уже был; 403 `invalid_password` — не сошёлся.
     * [onSuccess] закрывает диалог, и только после ответа сервера.
     */
    fun setPassword(current: String?, new: String, onSuccess: () -> Unit) {
        if (_isIdentityBusy.value) return
        _isIdentityBusy.value = true
        viewModelScope.launch {
            try {
                val hadPassword = current != null
                sessionStore.updateMe(repository.setPassword(current, new).user)
                sessionStore.confirm(
                    UiText.res(
                        if (hadPassword) R.string.toast_password_changed else R.string.toast_password_set
                    )
                )
                onSuccess()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                Log.e(TAG, "set password failed", e)
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
                analytics.track(AnalyticsEvent.AccountUnlinked(provider.id))
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
     *
     * ПОВТОР после `purge_incomplete` отличается ровно одним: отвязки
     * push-токена в нём НЕТ. Аккаунт уже tombstone, `DELETE /me/devices` висит
     * на обычном `s.auth` и отвечает 401, а [com.zagir.splitty.core.network.AuthInterceptor]
     * на этот 401 звал разлогин — токен, которым только и можно доделать
     * чистку, исчезал ровно перед повторным запросом. Повтор уходил без
     * Authorization, а войти заново нельзя: `SoftDeleteUser` вычистил все
     * личности, и PII человека оставалась в базе навсегда (5.1.1(v)/GDPR).
     * См. [com.zagir.splitty.core.session.Session.purgePending].
     */
    fun deleteAccount() {
        if (_isDeleting.value) return
        _isDeleting.value = true
        viewModelScope.launch {
            // Аккаунта уже нет (прошлая попытка упала после tombstone):
            // отвязывать push-токен нечему и НЕЛЬЗЯ.
            val isRetryAfterTombstone = sessionStore.isPurgePending()
            try {
                if (!isRetryAfterTombstone) {
                    // Отвязать FCM-токен, ПОКА JWT ещё валиден: после tombstone
                    // сервер отвергнет запрос, и токен устройства остался бы висеть.
                    // runCatching: отвязка best-effort по своему контракту, а сбой
                    // Firebase (не инициализирован, нет Play Services) не имеет
                    // права отменить удаление аккаунта — его требует Google Play.
                    runCatching { pushTokenRegistrar.unregisterCurrent() }
                        .onFailure { Log.w(TAG, "unregister push token failed", it) }
                }
                repository.deleteAccount()
                // 204: чистка доведена до конца. logout снимает и флаг повтора.
                sessionStore.logout()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                Log.e(TAG, "delete account failed", e)
                // Сбой ПОСЛЕ tombstone: поднимаем флаг ДО показа ошибки. С этого
                // момента ни один 401 не смеет стереть токен повтора, а корень
                // (AppRootViewModel) сам доведёт чистку до 204.
                if ((e as? ApiException)?.isPurgeIncomplete == true) {
                    runCatching { sessionStore.markPurgePending() }
                        .onFailure { Log.e(TAG, "не удалось отметить незавершённую чистку", it) }
                }
                _errorMessage.value = humanErrorText(e)
            } finally {
                _isDeleting.value = false
            }
        }
    }

    fun dismissNotice() {
        _noticeMessage.value = null
    }

    /**
     * «Выйти на всех устройствах»: отзывает ВСЕ выданные токены, включая
     * текущий. До этого токен жил 90 дней и не отзывался ничем, кроме смены
     * общего секрета — то есть разлогина всех.
     */
    fun revokeAllSessions() {
        viewModelScope.launch {
            try {
                repository.revokeTokens()
                // Текущий токен тоже отозван — закрываем сессию сами, не
                // дожидаясь 401 на следующем запросе
                sessionStore.logout()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _errorMessage.value = humanErrorText(e)
            }
        }
    }

    /** Выход: чистит токен и профиль — AppRoot сам покажет LoginScreen. */
    fun logout() {
        // До чистки: после неё владельца очереди уже нет, и записать выход
        // будет некуда.
        analytics.track(AnalyticsEvent.Logout)
        viewModelScope.launch {
            // Отвязка push-токена — best-effort и НЕ должна мешать выходу.
            // Раньше её падение (нет сервисов Google, отозван доступ) выбрасывало
            // человека в ошибку, а сессия оставалась: выйти было нельзя вообще,
            // и «Выйти» превращалось в кнопку, которая показывает ошибку
            runCatching { pushTokenRegistrar.unregisterCurrent() }
                .onFailure { if (it is CancellationException) throw it }
            try {
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
    fun showError(message: UiText) {
        _errorMessage.value = message
    }
}
