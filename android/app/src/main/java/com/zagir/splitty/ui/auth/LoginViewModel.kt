package com.zagir.splitty.ui.auth

import com.zagir.splitty.core.analytics.AnalyticsEvent
import com.zagir.splitty.core.analytics.Analytics
import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import android.content.Context
import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.auth.GoogleIdTokenProvider
import com.zagir.splitty.core.auth.GoogleSignInException
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.auth.TelegramAuthBus
import com.zagir.splitty.core.model.TelegramLoginBody
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
 * Проверки формы «email + пароль». Точный разбор адреса — за сервером
 * (`mail.ParseAddress`), здесь только чтобы не слать заведомый мусор.
 */
object EmailLoginForm {
    /** Минимум сервера (`minPasswordLen` в password_auth.go). */
    const val MIN_PASSWORD_LENGTH = 8

    /** bcrypt молча отбрасывает всё после 72 байт — длинные пароли совпадали
     *  бы по общему префиксу, поэтому дальше не пускаем. */
    const val MAX_PASSWORD_BYTES = 72

    fun normalizeEmail(raw: String): String = raw.trim().lowercase()

    fun isValidEmail(raw: String): Boolean {
        val parts = normalizeEmail(raw).split("@")
        if (parts.size != 2 || parts[0].isEmpty()) return false
        val domain = parts[1]
        return domain.length >= 3 && domain.contains(".") &&
            !domain.startsWith(".") && !domain.endsWith(".")
    }

    fun isValidPassword(password: String): Boolean =
        password.length >= MIN_PASSWORD_LENGTH &&
            password.toByteArray().size <= MAX_PASSWORD_BYTES
}

/** Состояние экрана входа. */
data class LoginUiState(
    val email: String = "",
    val password: String = "",
    val registerName: String = "",
    /** Та же форма работает и на вход, и на регистрацию. */
    val isRegistering: Boolean = false,
    val baseUrl: String = "",
    val isLoggingIn: Boolean = false,
    /** null — алерта нет; иначе показывается диалог «Ошибка». */
    val errorMessage: UiText? = null,
) {
    /** Для входа длина пароля не проверяется: он мог быть задан до правил. */
    val isEmailFormValid: Boolean
        get() = if (isRegistering) {
            EmailLoginForm.isValidEmail(email) &&
                EmailLoginForm.isValidPassword(password) &&
                registerName.trim().isNotEmpty()
        } else {
            EmailLoginForm.isValidEmail(email) && password.isNotEmpty()
        }
}

/**
 * Вход: POST /auth/google (Credential Manager), POST /auth/register и
 * POST /auth/login (email + пароль), POST /auth/code (код из Telegram);
 * поле «Сервер» персистится в SessionStore на каждое изменение.
 */
private const val TAG = "LoginViewModel"

@HiltViewModel
class LoginViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val googleIdTokenProvider: GoogleIdTokenProvider,
    private val telegramAuthBus: TelegramAuthBus,
    private val analytics: Analytics,
) : ViewModel() {

    /** Результаты входа через Telegram — их приносит MainActivity по deep link. */
    val telegramPayloads = telegramAuthBus.payloads

    private val _state = MutableStateFlow(
        LoginUiState(baseUrl = sessionStore.currentBaseUrl())
    )
    val state: StateFlow<LoginUiState> = _state.asStateFlow()

    fun onEmailChange(value: String) = _state.update { it.copy(email = value) }
    fun onPasswordChange(value: String) = _state.update { it.copy(password = value) }
    fun onRegisterNameChange(value: String) = _state.update { it.copy(registerName = value) }
    fun toggleRegistering() = _state.update { it.copy(isRegistering = !it.isRegistering) }
    fun dismissError() = _state.update { it.copy(errorMessage = null) }

    /**
     * Локальная (не сетевая) ошибка экрана в тот же алерт — сейчас это
     * единственный случай «нет активити для системного листа Google».
     */
    fun showError(message: UiText) = _state.update { it.copy(errorMessage = message) }

    /** Изменение адреса сервера: сразу персистится (действует на следующие запросы). */
    fun onBaseUrlChange(value: String) {
        _state.update { it.copy(baseUrl = value) }
        viewModelScope.launch {
            runCatching { sessionStore.setBaseUrl(value) }
                .onFailure { Log.e(TAG, "persist base url failed", it) }
        }
    }

    init {
        // Знаменатель воронки: сколько человек вообще дошли до экрана входа.
        // Здесь, а не в composable: экран пересобирается при повороте и смене
        // темы, и событие считалось бы по числу перерисовок.
        analytics.trackAnonymous(AnalyticsEvent.LoginShown)
    }

    /**
     * Причина неудачи из закрытого множества контракта. Сеть и сервер
     * разделены намеренно: первое чинится у человека, второе у нас.
     */
    private fun failureReason(e: ApiException): String = when {
        e.code == ApiException.CODE_TRANSPORT -> "network"
        e.isUnauthorized -> "invalid"
        else -> "server"
    }

    /**
     * Вход через Google: системный лист выбора аккаунта (Credential Manager) →
     * id-токен → POST /auth/google.
     *
     * [activityContext] — контекст активити: лист рисуется поверх неё.
     * Отмена пользователем не показывает ошибку — он и так знает, что закрыл
     * лист, а алерт поверх собственного действия читается как сбой.
     */
    fun loginWithGoogle(activityContext: Context) {
        if (_state.value.isLoggingIn) return
        _state.update { it.copy(isLoggingIn = true) }
        analytics.trackAnonymous(AnalyticsEvent.LoginStarted(method = "google"))
        viewModelScope.launch {
            try {
                val idToken = googleIdTokenProvider.idToken(activityContext) ?: run {
                    // null — человек закрыл лист выбора аккаунта.
                    analytics.trackAnonymous(AnalyticsEvent.LoginFailed("google", "cancelled"))
                    return@launch
                }
                val response = repository.loginWithGoogle(idToken)
                sessionStore.signIn(response.token, response.user)
                analytics.track(AnalyticsEvent.LoginCompleted(method = "google"))
            } catch (e: CancellationException) {
                // Обязательно ДО общего catch (e: Exception): CancellationException
                // наследует IllegalStateException и попадала в него, превращая
                // штатную отмену (успешный вход снёс экран вместе с его scope)
                // в алерт «Не удалось сохранить сессию» — а сессия сохранена.
                throw e
            } catch (e: GoogleSignInException) {
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("google", "provider"))
                _state.update { it.copy(errorMessage = humanErrorText(e)) }
            } catch (e: ApiException) {
                // 401 здесь — «сервер отверг id-токен» (протухший/чужой aud),
                // а не «неверный код»: пользователю сообщаем ровно это.
                val message = if (e.isUnauthorized) {
                    UiText.res(R.string.error_google_failed)
                } else {
                    humanErrorText(e)
                }
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("google", failureReason(e)))
                _state.update { it.copy(errorMessage = message) }
            } catch (e: Exception) {
                // См. комментарий в loginWithTelegram: signIn пишет в
                // DataStore/Keystore мимо ApiException-обёртки.
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("google", "server"))
                Log.e(TAG, "google login failed", e)
                _state.update { it.copy(errorMessage = UiText.res(R.string.error_session_save)) }
            } finally {
                _state.update { it.copy(isLoggingIn = false) }
            }
        }
    }

    /**
     * Вход через Telegram Login Widget: payload из `splitty://tg-callback`
     * (см. core/auth/TelegramWebAuth) обменивается на сессию — POST /auth/telegram.
     * 401 — подпись не сошлась: чинить это человеку нечем, кроме «попробуйте ещё раз».
     */
    /** Нажата кнопка Telegram: браузер открывает экран, ответ придёт шиной. */
    fun onTelegramStarted() {
        analytics.trackAnonymous(AnalyticsEvent.LoginStarted(method = "telegram"))
    }

    /** Результат из шины: успех — меняем на сессию, провал разбора — говорим вслух. */
    fun onTelegramResult(result: Result<TelegramLoginBody>) {
        telegramAuthBus.consume()
        result.fold(
            onSuccess = ::loginWithTelegram,
            onFailure = {
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("telegram", "provider"))
                _state.update { it.copy(errorMessage = UiText.res(R.string.error_telegram_rejected)) }
            },
        )
    }

    fun loginWithTelegram(payload: TelegramLoginBody) {
        if (_state.value.isLoggingIn) return
        _state.update { it.copy(isLoggingIn = true) }
        viewModelScope.launch {
            try {
                val response = repository.loginWithTelegram(payload)
                sessionStore.signIn(response.token, response.user)
                // Раньше этого события тут не было вовсе: последняя ступень
                // воронки теряла всех, кто вошёл через Telegram.
                analytics.track(AnalyticsEvent.LoginCompleted(method = "telegram"))
            } catch (e: CancellationException) {
                throw e // см. комментарий в loginWithGoogle
            } catch (e: ApiException) {
                val message = if (e.isUnauthorized) {
                    UiText.res(R.string.error_telegram_rejected)
                } else {
                    humanErrorText(e)
                }
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("telegram", failureReason(e)))
                _state.update { it.copy(errorMessage = message) }
            } catch (e: Exception) {
                // signIn пишет в DataStore/Keystore мимо SplittyRepository.call —
                // IOException не оборачивается в ApiException и раньше улетал из
                // viewModelScope (обработчик стоит только на @ApplicationScope),
                // роняя процесс прямо на экране входа.
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("telegram", "server"))
                Log.e(TAG, "telegram login failed", e)
                _state.update { it.copy(errorMessage = UiText.res(R.string.error_session_save)) }
            } finally {
                _state.update { it.copy(isLoggingIn = false) }
            }
        }
    }

    /**
     * Вход или регистрация по email — POST /auth/login или /auth/register.
     * Текст ошибки берём с сервера: он намеренно отвечает одинаково на неверный
     * пароль и незнакомый адрес, а на занятый адрес — понятным 409.
     */
    fun submitEmailForm() {
        val current = _state.value
        if (!current.isEmailFormValid || current.isLoggingIn) return
        val email = EmailLoginForm.normalizeEmail(current.email)
        val password = current.password
        val name = current.registerName.trim()
        val registering = current.isRegistering

        _state.update { it.copy(isLoggingIn = true) }
        // Здесь, а не на экране: форму отправляют и кнопкой, и клавишей ввода.
        analytics.trackAnonymous(AnalyticsEvent.LoginStarted(method = "password"))
        viewModelScope.launch {
            try {
                val response = if (registering) {
                    repository.register(email, password, name)
                } else {
                    repository.loginWithPassword(email, password)
                }
                sessionStore.signIn(response.token, response.user)
                analytics.track(AnalyticsEvent.LoginCompleted(method = "password"))
                _state.update { it.copy(password = "") }
            } catch (e: CancellationException) {
                throw e // см. комментарий в loginWithGoogle
            } catch (e: ApiException) {
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("password", failureReason(e)))
                _state.update { it.copy(errorMessage = humanErrorText(e)) }
            } catch (e: Exception) {
                analytics.trackAnonymous(AnalyticsEvent.LoginFailed("password", "server"))
                Log.e(TAG, "password login failed", e)
                _state.update { it.copy(errorMessage = UiText.res(R.string.error_session_save)) }
            } finally {
                _state.update { it.copy(isLoggingIn = false) }
            }
        }
    }
}
