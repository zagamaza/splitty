package com.zagir.splitty.core.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * Текущая сессия. null-состояние [SessionStore.state] означает
 * «ещё читаем DataStore» — UI в этот момент показывает пустой фон.
 */
data class Session(
    /** JWT; null — пользователь не вошёл. */
    val token: String? = null,
    /** Базовый адрес сервера (меняется на экране входа). */
    val baseUrl: String = SessionStore.DEFAULT_BASE_URL,
    /** Профиль (кэш последнего /me или ответа авторизации). */
    val me: Me? = null,
    /** Тема приложения: system / light / dark. */
    val theme: String = SessionStore.THEME_SYSTEM,
) {
    val isAuthenticated: Boolean get() = token != null
}

/**
 * Хранилище сессии: токен, адрес сервера и кэш профиля в Jetpack DataStore;
 * наружу — StateFlow. Плюс [dataVersion] — «версия данных», растёт после
 * каждой успешной мутации (аналог iOS SessionStore.dataVersion): экраны-списки
 * перезагружаются, когда она меняется.
 */
@Singleton
class SessionStore @Inject constructor(
    private val dataStore: DataStore<Preferences>,
    @ApplicationScope private val scope: CoroutineScope,
) {
    companion object {
        /** Прод-сервер по умолчанию; на эмуляторе для локального бэкенда — http://10.0.2.2:7171. */
        const val DEFAULT_BASE_URL = "http://138.124.18.189:18002"

        const val THEME_SYSTEM = "system"
        const val THEME_LIGHT = "light"
        const val THEME_DARK = "dark"

        private val KEY_TOKEN = stringPreferencesKey("token")
        private val KEY_BASE_URL = stringPreferencesKey("base_url")
        private val KEY_ME = stringPreferencesKey("me_json")
        private val KEY_THEME = stringPreferencesKey("theme")
    }

    /** Текущая сессия; null — DataStore ещё не прочитан (первый кадр приложения). */
    val state: StateFlow<Session?> = dataStore.data
        .map { prefs ->
            Session(
                token = prefs[KEY_TOKEN],
                baseUrl = prefs[KEY_BASE_URL]?.takeIf { it.isNotBlank() } ?: DEFAULT_BASE_URL,
                me = prefs[KEY_ME]?.let { raw ->
                    runCatching { SplittyJson.decodeFromString(Me.serializer(), raw) }.getOrNull()
                },
                theme = prefs[KEY_THEME] ?: THEME_SYSTEM,
            )
        }
        .stateIn(scope, SharingStarted.Eagerly, null)

    private val _dataVersion = MutableStateFlow(0)

    /**
     * Версия данных: экраны-списки (Группы, Друзья, Активность, Группа)
     * подписываются и перезагружаются при изменении.
     */
    val dataVersion: StateFlow<Int> = _dataVersion

    /** Звать после КАЖДОЙ успешной мутации (расход/платёж/комната/архив/join). */
    fun noteDataChanged() {
        _dataVersion.update { it + 1 }
    }

    /** Текущий адрес сервера — для OkHttp-интерцептора (синхронно). */
    fun currentBaseUrl(): String = state.value?.baseUrl ?: DEFAULT_BASE_URL

    /** Текущий токен — для OkHttp-интерцептора (синхронно). */
    fun currentToken(): String? = state.value?.token

    /** Успешный вход: сохранить токен и профиль. */
    suspend fun signIn(token: String, me: Me) {
        dataStore.edit { prefs ->
            prefs[KEY_TOKEN] = token
            prefs[KEY_ME] = SplittyJson.encodeToString(Me.serializer(), me)
        }
    }

    /** Обновить кэш профиля (после GET/PATCH /me). */
    suspend fun updateMe(me: Me) {
        dataStore.edit { prefs ->
            prefs[KEY_ME] = SplittyJson.encodeToString(Me.serializer(), me)
        }
    }

    /** Сменить тему приложения (system / light / dark, персистится). */
    suspend fun setTheme(theme: String) {
        dataStore.edit { prefs -> prefs[KEY_THEME] = theme }
    }

    /** Сменить адрес сервера (персистится; действует на все последующие запросы). */
    suspend fun setBaseUrl(url: String) {
        dataStore.edit { prefs -> prefs[KEY_BASE_URL] = url.trim() }
    }

    /** Выход: чистит токен и профиль, адрес сервера сохраняется. */
    suspend fun logout() {
        dataStore.edit { prefs ->
            prefs.remove(KEY_TOKEN)
            prefs.remove(KEY_ME)
        }
    }

    /**
     * Глобальный разлогин по 401 из ЛЮБОГО запроса (зовёт [com.zagir.splitty.core.network.AuthInterceptor]
     * с потока OkHttp — поэтому fire-and-forget в application-scope).
     */
    fun notifyUnauthorized() {
        scope.launch { logout() }
    }
}
