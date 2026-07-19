package com.zagir.splitty.core.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.stringPreferencesKey
import com.zagir.splitty.BuildConfig
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.flow.channelFlow
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
    /**
     * В DataStore лежат учётные данные (шифротекст или plain-токен) — независимо
     * от того, удалось ли их расшифровать. Отличает НАСТОЯЩИЙ разлогин (ключи
     * стёрты) от сбоя расшифровки: transient-ошибка Keystore даёт token = null,
     * и очистка офлайн-данных по этому признаку уничтожала бы неотправленные
     * расходы при живом токене на диске. См. OfflineDataCleaner.
     */
    val hasStoredToken: Boolean = false,
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
    private val tokenCipher: TokenCipher,
    @ApplicationScope private val scope: CoroutineScope,
) {
    companion object {
        /**
         * Сервер по умолчанию. В release — HTTPS-плейсхолдер прод-домена
         * (боевой сервер к релизу обязан быть на HTTPS; cleartext из релиза
         * убран, см. network_security_config release-варианта). В debug —
         * дев-сервер по голому HTTP-IP (cleartext разрешён только в debug).
         * Пользователь может переопределить адрес на экране входа.
         */
        val DEFAULT_BASE_URL: String
            get() = if (BuildConfig.DEBUG) "http://138.124.18.189:18002" else "https://api.splitty.app"

        const val THEME_SYSTEM = "system"
        const val THEME_LIGHT = "light"
        const val THEME_DARK = "dark"

        /** Старый ключ с plaintext-токеном — остаётся только для dual-read миграции. */
        private val KEY_TOKEN = stringPreferencesKey("token")

        /** Новый ключ: шифротекст токена (AES-GCM поверх Keystore). */
        private val KEY_TOKEN_ENC = stringPreferencesKey("token_enc")
        private val KEY_BASE_URL = stringPreferencesKey("base_url")
        private val KEY_ME = stringPreferencesKey("me_json")
        private val KEY_THEME = stringPreferencesKey("theme")

        /** Повторы чтения DataStore при транзиентной ошибке ввода-вывода. */
        private const val RETRY_DELAY_MS = 200L

        /** Потолок множителя паузы между попытками чтения DataStore. */
        private const val RETRY_BACKOFF_STEPS = 25L
    }

    init {
        // Миграция без разлогина: на первом старте после апдейта переносим
        // старый plaintext-токен в шифротекст. Чтение (map ниже) уже понимает
        // оба формата — гонка миграции и первого чтения токен не теряет.
        scope.launch { migrateTokenIfNeeded() }
    }

    /** Текущая сессия; null — DataStore ещё не прочитан (первый кадр приложения). */
    val state: StateFlow<Session?> = channelFlow {
        // Сбор пересоздаётся в цикле, а не через retryWhen + catch: catch
        // терминален — после него upstream завершён и stateIn не эмитит НИЧЕГО
        // до перезапуска процесса. Пользователя выбрасывало на экран входа, он
        // успешно входил, но state не обновлялся и все запросы шли без токена.
        // Ограниченный retry только снижал вероятность: постоянная ошибка (и
        // CorruptionException, который тоже IOException) всё равно доводила до
        // терминального catch.
        var attempt = 0L
        while (isActive) {
            try {
                dataStore.data.collect { send(it) }
                return@channelFlow // upstream завершился штатно
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // Пустые настройки = разлогин: восстановимо, в отличие от
                // зависания на null («ещё читаем» → пустой экран без выхода).
                // Дальше продолжаем пытаться — успешное чтение или запись при
                // входе вернут сессию без перезапуска приложения.
                send(emptyPreferences())
                delay(RETRY_DELAY_MS * (attempt + 1).coerceAtMost(RETRY_BACKOFF_STEPS))
                attempt++
            }
        }
    }
        .map { prefs ->
            Session(
                // Dual-read: сначала новый зашифрованный ключ, иначе старый plain
                // (ещё не мигрировано либо чистая старая установка). Если шифротекст
                // не расшифровался (ключ Keystore пропал) — токена нет → разлогин.
                token = prefs[KEY_TOKEN_ENC]?.let { tokenCipher.decrypt(it) } ?: prefs[KEY_TOKEN],
                hasStoredToken = prefs[KEY_TOKEN_ENC] != null || prefs[KEY_TOKEN] != null,
                baseUrl = usableBaseUrl(prefs[KEY_BASE_URL]),
                me = prefs[KEY_ME]?.let { raw ->
                    runCatching { SplittyJson.decodeFromString(Me.serializer(), raw) }.getOrNull()
                },
                theme = prefs[KEY_THEME] ?: THEME_SYSTEM,
            )
        }
        .stateIn(scope, SharingStarted.Eagerly, null)

    /**
     * Сохранённый адрес сервера, пригодный для текущего варианта сборки.
     *
     * В release cleartext запрещён network_security_config, поэтому http-адрес,
     * оставшийся в DataStore от debug-сборки (пакет у вариантов общий), делал бы
     * приложение неработоспособным: любой запрос падал бы в «Нет соединения»,
     * а поле смены сервера доступно только в debug — выхода из состояния нет.
     */
    private fun usableBaseUrl(saved: String?): String {
        val url = saved?.takeIf { it.isNotBlank() } ?: return DEFAULT_BASE_URL
        if (!BuildConfig.DEBUG && url.startsWith("http://")) return DEFAULT_BASE_URL
        return url
    }

    /**
     * Переносит plaintext-токен старого формата в шифротекст ровно один раз.
     * Транзакция [DataStore.edit] атомарна: одновременного шифрованного и
     * plain-ключа наружу не видно.
     */
    private suspend fun migrateTokenIfNeeded() {
        // runCatching: encrypt ходит в Keystore, а тот на части OEM-прошивок
        // бросает ProviderException/KeyStoreException. Без перехвата исключение
        // уходит в SupervisorJob-скоуп (он НЕ глотает ошибки) и убивает процесс
        // на КАЖДОМ старте — миграция не доезжает, крэш-луп без выхода.
        // Не мигрировали — не страшно: dual-read ниже читает и plaintext.
        runCatching {
            dataStore.edit { prefs ->
                val plain = prefs[KEY_TOKEN]
                if (prefs[KEY_TOKEN_ENC] == null && plain != null) {
                    prefs[KEY_TOKEN_ENC] = tokenCipher.encrypt(plain)
                    prefs.remove(KEY_TOKEN)
                }
            }
        }
    }

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

    /** Успешный вход: сохранить токен (зашифрованным) и профиль. */
    suspend fun signIn(token: String, me: Me) {
        dataStore.edit { prefs ->
            prefs[KEY_TOKEN_ENC] = tokenCipher.encrypt(token)
            prefs.remove(KEY_TOKEN) // страховка: чистый вход не должен оставлять plain-ключ
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

    /**
     * Выход: чистит токен (оба формата), профиль и ключ Keystore; адрес сервера
     * сохраняется. Офлайн-кеш/outbox чистит [com.zagir.splitty.data.OfflineDataCleaner]
     * по переходу «токен был → токена нет».
     */
    suspend fun logout() {
        dataStore.edit { prefs ->
            prefs.remove(KEY_TOKEN_ENC)
            prefs.remove(KEY_TOKEN)
            prefs.remove(KEY_ME)
        }
        tokenCipher.clearKey()
    }

    /**
     * Глобальный разлогин по 401 из ЛЮБОГО запроса (зовёт [com.zagir.splitty.core.network.AuthInterceptor]
     * с потока OkHttp — поэтому fire-and-forget в application-scope).
     */
    fun notifyUnauthorized() {
        scope.launch { logout() }
    }
}
