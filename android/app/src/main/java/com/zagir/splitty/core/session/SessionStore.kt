package com.zagir.splitty.core.session

import android.util.Log
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
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
import com.zagir.splitty.core.ui.UiText
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.asStateFlow
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
    /**
     * Прочитать DataStore не удалось — это НЕ разлогин, а пустышка-заглушка,
     * чтобы UI не висел на null. Отличать обязательно: без флага сбой чтения
     * (в т.ч. CorruptionException, который ReplaceFileCorruptionHandler
     * превращает в пустые prefs) выглядит бит-в-бит как выход из аккаунта, и
     * OfflineDataCleaner необратимо стирал очередь неотправленных расходов.
     */
    val readFailed: Boolean = false,
    /**
     * true — `DELETE /me` упал ПОСЛЕ tombstone (`purge_incomplete`): аккаунт
     * удалён, но его PII (имя в снимках комнат, chat_state, bug_report,
     * push_outbox) осталась в базе. Доделать чистку может ТОЛЬКО повторный
     * `DELETE /me` этим же токеном — маршрут висит на `authDeleted` ровно ради
     * повтора, а войти заново нельзя: `SoftDeleteUser` вычистил все личности.
     *
     * Флаг персистится РЯДОМ С ТОКЕНОМ, потому что удержать токен на время
     * повтора локальной переменной экрана невозможно: аккаунт уже tombstone, и
     * КАЖДЫЙ следующий запрос к любому другому маршруту (все на `s.auth`)
     * отвечает 401 — отвязка push-токена, обновление профиля, открытие группы.
     * Без флага первый же такой 401 звал [SessionStore.notifyUnauthorized], тот
     * стирал токен, и единственный ключ к маршруту, который сервер держит
     * открытым, уничтожался самим клиентом — вместе с шансом когда-либо
     * доделать удаление (5.1.1(v)/GDPR). Пока флаг стоит,
     * [SessionEndReason.EXPIRED] токен НЕ трогает, а корень
     * (`AppRootViewModel`) сам повторяет `DELETE /me` до 204.
     */
    val purgePending: Boolean = false,
) {
    val isAuthenticated: Boolean get() = token != null
}

/**
 * Почему пропал токен. Для самого [SessionStore] разницы нет — он в обоих
 * случаях стирает одно и то же, — но она принципиальна для
 * [com.zagir.splitty.data.OfflineDataCleaner].
 *
 * [EXPIRED] — сессия протухла (401 из любого запроса, см.
 * [com.zagir.splitty.core.network.AuthInterceptor]). Человек войдёт ТЕМ ЖЕ
 * аккаунтом, и отложенное вступление по ссылке-приглашению обязано это
 * пережить: `AppRoot` при 401 оставляет намерение в хранилище именно затем,
 * чтобы вступление доехало после переавторизации, а чистка стирала бы его
 * следом — приглашение терялось молча и навсегда (порт поведения iOS
 * `expireSession`, который `PendingJoin` не трогает).
 *
 * [LOGOUT] — человек нажал «Выйти» (или удалил аккаунт). Здесь чистим всё:
 * следующий владелец устройства не должен молча вступить в чужую группу.
 */
enum class SessionEndReason { LOGOUT, EXPIRED }

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
        private const val TAG = "SessionStore"

        /**
         * Сервер по умолчанию — боевой бэкенд под TLS (Caddy + Let's Encrypt).
         * Cleartext из release-варианта убран целиком (см.
         * src/release/res/xml/network_security_config.xml), инвариант стережёт
         * SecurityConfigTest. Пользователь может переопределить адрес на экране
         * входа — схему проверяет [usableBaseUrl].
         */
        val DEFAULT_BASE_URL: String
            get() = "https://splitor.zagirnur.dev"

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

        /** Незавершённая после tombstone чистка — см. [Session.purgePending]. */
        private val KEY_PURGE_PENDING = booleanPreferencesKey("purge_pending")
        private val KEY_AI_DISCLOSURE_SEEN = booleanPreferencesKey("ai_disclosure_seen")
        private val KEY_WELCOME_SEEN = stringSetPreferencesKey("welcome_seen_accounts")

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
                dataStore.data.collect { send(it to false) }
                return@channelFlow // upstream завершился штатно
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                // Пустые настройки = разлогин: восстановимо, в отличие от
                // зависания на null («ещё читаем» → пустой экран без выхода).
                // Дальше продолжаем пытаться — успешное чтение или запись при
                // входе вернут сессию без перезапуска приложения.
                // readFailed=true: заглушка для UI, но НЕ разлогин — потребители,
                // стирающие данные аккаунта, обязаны её пропускать.
                send(emptyPreferences() to true)
                delay(RETRY_DELAY_MS * (attempt + 1).coerceAtMost(RETRY_BACKOFF_STEPS))
                attempt++
            }
        }
    }
        .map { (prefs, readFailed) ->
            Session(
                readFailed = readFailed,
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
                // Переживает перезапуск процесса намеренно: «человек закрыл
                // приложение вместо повтора» иначе навсегда оставлял бы его
                // PII в базе — фонового реконсилятора на сервере нет.
                purgePending = prefs[KEY_PURGE_PENDING] == true,
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

    /**
     * Подтверждение последнего успешного действия («Погашение записано»).
     *
     * Ни погашение, ни выход из группы, ни смена пароля ничего не отвечали:
     * человек не понимал, случилось ли действие, и повторял его. Одно место на
     * всё приложение, а не пять разных плашек.
     */
    private val _successToast = MutableStateFlow<UiText?>(null)
    val successToast: StateFlow<UiText?> = _successToast.asStateFlow()

    /** Показать подтверждение действия. */
    fun confirm(text: UiText) {
        _successToast.value = text
    }

    /** Скрыть подтверждение (после показа). */
    fun dismissToast() {
        _successToast.value = null
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

    private val _unreadNotifications = MutableStateFlow(0)

    /**
     * Непрочитанное для бейджа на табе «Уведомления».
     *
     * Живёт в сессии, а не во вью-модели раздела: раздел счётчик как раз
     * гасит, и держи мы его там — бейдж появлялся бы ровно в тот момент,
     * когда его уже погасили. Порт iOS SessionStore.unreadNotifications.
     */
    val unreadNotifications: StateFlow<Int> = _unreadNotifications

    fun setUnreadNotifications(count: Int) {
        _unreadNotifications.value = count
    }

    /** Текущий адрес сервера — для OkHttp-интерцептора (синхронно). */
    fun currentBaseUrl(): String = state.value?.baseUrl ?: DEFAULT_BASE_URL

    /** Текущий токен — для OkHttp-интерцептора (синхронно). */
    fun currentToken(): String? = state.value?.token

    /** Номер текущего человека — аналитике, чтобы не писать событие ничьим. */
    fun currentUserId(): Long? = state.value?.me?.id

    /**
     * Чистка после tombstone не доведена до конца — см. [Session.purgePending].
     * Синхронно: читается из [endSession], который зовёт [notifyUnauthorized]
     * с потока OkHttp.
     */
    fun isPurgePending(): Boolean = state.value?.purgePending == true

    /**
     * Отметить, что `DELETE /me` упал после tombstone. С этого момента 401 от
     * любого маршрута НЕ стирает токен: им и только им доделывается чистка.
     */
    suspend fun markPurgePending() {
        dataStore.edit { prefs -> prefs[KEY_PURGE_PENDING] = true }
    }

    private val _lastSessionEndReason = MutableStateFlow<SessionEndReason?>(null)

    /**
     * Причина последнего исчезновения токена; null — сессию ещё не завершали.
     *
     * Читается подписчиком [state] в тот момент, когда он УВИДЕЛ переход
     * «токен был → токена нет». Это корректно, потому что флаг ставится строго
     * ДО записи в DataStore: к моменту эмиссии он уже опубликован
     * (`StateFlow.value` — volatile-запись), и гонки «увидели пропажу токена,
     * но ещё не увидели причину» быть не может.
     */
    val lastSessionEndReason: StateFlow<SessionEndReason?> = _lastSessionEndReason

    /** Успешный вход: сохранить токен (зашифрованным) и профиль. */
    suspend fun signIn(token: String, me: Me) {
        // Новая сессия — старая причина завершения больше ни о чём не говорит.
        _lastSessionEndReason.value = null
        // Keystore на части прошивок бросает ProviderException/KeyStoreException —
        // ровно поэтому migrateTokenIfNeeded обёрнут в runCatching. Здесь падение
        // было фатальным: сервер уже принял одноразовый код, а приложение
        // крэшилось до записи токена, и войти становилось невозможно в принципе.
        // Дегрейд на plain-ключ безопаснее разлогина: dual-read его понимает.
        val encrypted = runCatching { tokenCipher.encrypt(token) }
            .onFailure {
                // Деградация молчаливой быть не должна: сырой JWT ляжет в
                // незашифрованный DataStore и останется там до следующего входа.
                // Это последняя линия обороны (иначе войти вообще нельзя), но в
                // логах она обязана быть видна — иначе проблему Keystore на
                // прошивке тестера не отличить от нормальной работы.
                Log.w(TAG, "Keystore недоступен — токен сохраняется В ОТКРЫТОМ ВИДЕ", it)
            }
            .getOrNull()
        dataStore.edit { prefs ->
            if (encrypted != null) {
                prefs[KEY_TOKEN_ENC] = encrypted
                prefs.remove(KEY_TOKEN)
            } else {
                prefs[KEY_TOKEN] = token
                prefs.remove(KEY_TOKEN_ENC)
            }
            prefs[KEY_ME] = SplittyJson.encodeToString(Me.serializer(), me)
            // Новый вход — чужая незавершённая чистка к нему отношения не
            // имеет: оставшийся флаг гасил бы разлогин по 401 у живого
            // аккаунта, то есть ровно ту защиту, ради которой он и заведён.
            prefs.remove(KEY_PURGE_PENDING)
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

    /**
     * Показано ли разовое пояснение о том, что распознавание идёт на сервере.
     * Флаг устройства, а не аккаунта: правило одно для всех, кто им пользуется.
     */
    val aiDisclosureSeen: Flow<Boolean> =
        dataStore.data.map { prefs -> prefs[KEY_AI_DISCLOSURE_SEEN] ?: false }

    suspend fun markAiDisclosureSeen() {
        dataStore.edit { prefs -> prefs[KEY_AI_DISCLOSURE_SEEN] = true }
    }

    /**
     * Видел ли этот аккаунт разовое приветствие.
     *
     * Храним НАБОР номеров аккаунтов, а не один флаг на устройство: вход другим
     * человеком на том же телефоне обязан показать приветствие снова — иначе
     * новый пользователь молча теряет единственное объяснение продукта.
     */
    fun welcomeSeen(userId: Long): Flow<Boolean> =
        dataStore.data.map { prefs -> prefs[KEY_WELCOME_SEEN]?.contains(userId.toString()) == true }

    /** Пропуск — это ответ «не показывай больше», поэтому зовётся и из «Пропустить». */
    suspend fun markWelcomeSeen(userId: Long) {
        dataStore.edit { prefs ->
            prefs[KEY_WELCOME_SEEN] = (prefs[KEY_WELCOME_SEEN] ?: emptySet()) + userId.toString()
        }
    }

    /** Сменить адрес сервера (персистится; действует на все последующие запросы). */
    suspend fun setBaseUrl(url: String) {
        dataStore.edit { prefs -> prefs[KEY_BASE_URL] = url.trim() }
    }

    /**
     * ЯВНЫЙ выход пользователя: чистит токен (оба формата), профиль и ключ
     * Keystore; адрес сервера сохраняется. Офлайн-кеш/outbox чистит
     * [com.zagir.splitty.data.OfflineDataCleaner] по переходу «токен был →
     * токена нет».
     */
    suspend fun logout() = endSession(SessionEndReason.LOGOUT)

    /**
     * Общая часть выхода и протухания сессии: разница только в [reason],
     * который читает [com.zagir.splitty.data.OfflineDataCleaner].
     */
    private suspend fun endSession(reason: SessionEndReason) {
        // Незавершённая после tombstone чистка — единственное исключение.
        // Аккаунт удалён, поэтому 401 отвечает КАЖДЫЙ маршрут на `s.auth`
        // (отвязка push-токена, обновление профиля, открытие группы), и
        // обычное протухание уничтожило бы токен, которым только и можно
        // доделать удаление: войти заново нельзя, личности вычищены. Держим
        // токен до подтверждённого 204 — повторяет `AppRootViewModel`.
        //
        // ЯВНЫЙ выход ([SessionEndReason.LOGOUT]) исключением не является: его
        // делает сам человек, и им же завершается успешное удаление.
        if (reason == SessionEndReason.EXPIRED && isPurgePending()) return
        // Причину публикуем ДО записи: подписчик увидит пропажу токена уже
        // после неё (см. [lastSessionEndReason]).
        _lastSessionEndReason.value = reason
        // Чужой счётчик на табе следующего вошедшего — не косметика: он
        // показывает, сколько входящих было у предыдущего владельца.
        _unreadNotifications.value = 0
        dataStore.edit { prefs ->
            prefs.remove(KEY_TOKEN_ENC)
            prefs.remove(KEY_TOKEN)
            prefs.remove(KEY_ME)
            // Повторять больше нечего: либо чистка подтверждена 204, либо
            // человек вышел сам. Флаг обязан уйти вместе с токеном — иначе
            // следующая сессия унаследовала бы чужую защиту от 401.
            prefs.remove(KEY_PURGE_PENDING)
        }
        tokenCipher.clearKey()
    }

    /**
     * Глобальный разлогин по 401 из ЛЮБОГО запроса (зовёт [com.zagir.splitty.core.network.AuthInterceptor]
     * с потока OkHttp — поэтому fire-and-forget в application-scope).
     *
     * Это НЕ выход: сессия протухла, человек вернётся тем же аккаунтом —
     * см. [SessionEndReason.EXPIRED].
     */
    fun notifyUnauthorized() {
        scope.launch { endSession(SessionEndReason.EXPIRED) }
    }
}
