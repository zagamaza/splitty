package com.zagir.splitty.ui.auth

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * Одноразовый код входа из Telegram-бота: нормализация и валидация.
 * Чистая логика — покрыта юнит-тестами (порт iOS LoginCode).
 */
object LoginCode {
    /** Кнопка «Войти по коду» активна от 6 символов. */
    const val MIN_LENGTH = 6

    /** Убирает пробельные символы (вставка из чата) и приводит к верхнему регистру. */
    fun normalize(raw: String): String =
        raw.filterNot { it.isWhitespace() }.uppercase()

    /** true, когда нормализованный код достаточно длинный, чтобы отправлять. */
    fun isValid(raw: String): Boolean = normalize(raw).length >= MIN_LENGTH
}

/** Состояние экрана входа. */
data class LoginUiState(
    val code: String = "",
    val devTelegramId: String = "",
    val devDisplayName: String = "",
    val devUsername: String = "",
    val baseUrl: String = "",
    val isLoggingIn: Boolean = false,
    /** null — алерта нет; иначе показывается диалог «Ошибка». */
    val errorMessage: String? = null,
) {
    val isCodeValid: Boolean get() = LoginCode.isValid(code)
    val isDevFormValid: Boolean
        get() = (devTelegramId.trim().toLongOrNull() ?: 0L) > 0L &&
            devDisplayName.trim().isNotEmpty()
}

/**
 * Вход: POST /auth/code (код из Telegram) и POST /auth/dev (dev-режим);
 * поле «Сервер» персистится в SessionStore на каждое изменение.
 */
@HiltViewModel
class LoginViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
) : ViewModel() {

    private val _state = MutableStateFlow(
        LoginUiState(baseUrl = sessionStore.currentBaseUrl())
    )
    val state: StateFlow<LoginUiState> = _state.asStateFlow()

    fun onCodeChange(value: String) = _state.update { it.copy(code = value) }
    fun onDevTelegramIdChange(value: String) = _state.update { it.copy(devTelegramId = value) }
    fun onDevDisplayNameChange(value: String) = _state.update { it.copy(devDisplayName = value) }
    fun onDevUsernameChange(value: String) = _state.update { it.copy(devUsername = value) }
    fun dismissError() = _state.update { it.copy(errorMessage = null) }

    /** Изменение адреса сервера: сразу персистится (действует на следующие запросы). */
    fun onBaseUrlChange(value: String) {
        _state.update { it.copy(baseUrl = value) }
        viewModelScope.launch { sessionStore.setBaseUrl(value) }
    }

    /** Вход по коду из бота; 401 (invalid_code) — человеческое сообщение. */
    fun loginWithCode() {
        val code = LoginCode.normalize(_state.value.code)
        if (code.length < LoginCode.MIN_LENGTH || _state.value.isLoggingIn) return
        _state.update { it.copy(isLoggingIn = true) }
        viewModelScope.launch {
            try {
                val response = repository.loginWithCode(code)
                sessionStore.signIn(response.token, response.user)
            } catch (e: ApiException) {
                val message = if (e.isUnauthorized) {
                    "Неверный или просроченный код"
                } else {
                    e.message
                }
                _state.update { it.copy(errorMessage = message) }
            } finally {
                _state.update { it.copy(isLoggingIn = false) }
            }
        }
    }

    /** Dev-вход (только при API_DEV_AUTH=true на сервере). */
    fun loginDev() {
        val current = _state.value
        val userId = current.devTelegramId.trim().toLongOrNull()
        if (userId == null || userId <= 0) {
            _state.update { it.copy(errorMessage = "Введите числовой Telegram ID") }
            return
        }
        val name = current.devDisplayName.trim()
        if (name.isEmpty()) {
            _state.update { it.copy(errorMessage = "Введите имя") }
            return
        }
        if (current.isLoggingIn) return
        val username = current.devUsername.trim().ifEmpty { null }
        _state.update { it.copy(isLoggingIn = true) }
        viewModelScope.launch {
            try {
                val response = repository.loginDev(userId, name, username)
                sessionStore.signIn(response.token, response.user)
            } catch (e: ApiException) {
                _state.update { it.copy(errorMessage = e.message) }
            } finally {
                _state.update { it.copy(isLoggingIn = false) }
            }
        }
    }
}
