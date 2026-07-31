package com.zagir.splitty.core.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

/**
 * Отложенное намерение вступить в группу: код комнаты плюс id владельца.
 *
 * [ownerId] — тот, кто был в аккаунте, когда пришла ссылка; null — ссылку
 * открыл гость, и намерение достанется первому же вошедшему (ровно это и есть
 * сценарий приглашения). Хранить владельца обязательно: без него намерение
 * переживает протухшую сессию и достаётся ДРУГОМУ аккаунту — см.
 * [PendingJoinStore.reconcileOwner].
 */
data class PendingJoin(val roomId: String, val ownerId: Long?)

/**
 * Отложенное намерение вступить в группу: код из диплинка, который ещё не
 * исполнен — потому что пользователь не авторизован либо приложение только
 * стартует и корневой экран ещё не собран. Порт iOS `PendingJoin`.
 *
 * Переживает перезапуск процесса (DataStore) намеренно: путь «тап по ссылке →
 * экран входа → вход через Google» уводит в системный лист и в другое
 * приложение, и на слабом устройстве нас за это время выгружают. Потерять
 * намерение здесь — значит показать человеку, пришедшему по приглашению, пустой
 * список групп без единого объяснения.
 *
 * Хранится код комнаты — публичный идентификатор из ссылки, которую и так
 * переслали в мессенджере, — и id владельца намерения. Намерение обязано
 * умирать вместе с ЧУЖОЙ сессией: чистит его
 * [com.zagir.splitty.data.OfflineDataCleaner], иначе следующий человек на этом
 * устройстве молча вступил бы в чужую группу.
 */
@Singleton
class PendingJoinStore @Inject constructor(
    private val dataStore: DataStore<Preferences>,
) {
    companion object {
        private val KEY_PENDING_JOIN = stringPreferencesKey("pending_join_room_id")

        /**
         * Владелец намерения. Персистентный, а не поле в памяти: между
         * протуханием сессии и следующим входом процесс успевает умереть
         * (тап по ссылке уводит в системный лист входа), и любой in-memory
         * признак «кто был до этого» к моменту входа уже потерян.
         */
        private val KEY_PENDING_JOIN_OWNER = longPreferencesKey("pending_join_owner_id")
    }

    /**
     * Намерение, ожидающее исполнения; null — ждать нечего.
     *
     * Поток, а не разовое чтение: ссылка приходит и в ЖИВОЕ приложение
     * (`onNewIntent`), и корень обязан отреагировать на неё, не пересоздаваясь.
     * Сбой чтения DataStore отдаём как «намерения нет»: висящий диплинк — не
     * та причина, по которой стоит ронять корень приложения.
     */
    val pending: Flow<PendingJoin?> = dataStore.data
        .catch { emit(emptyPreferences()) }
        .map { prefs ->
            prefs[KEY_PENDING_JOIN]?.let { PendingJoin(it, prefs[KEY_PENDING_JOIN_OWNER]) }
        }
        .distinctUntilChanged()

    /**
     * Запомнить намерение (пришла ссылка). [ownerId] — id вошедшего сейчас
     * пользователя; null, если ссылку открыл гость.
     */
    suspend fun set(roomId: String, ownerId: Long? = null) {
        dataStore.edit { prefs ->
            prefs[KEY_PENDING_JOIN] = roomId
            if (ownerId != null) {
                prefs[KEY_PENDING_JOIN_OWNER] = ownerId
            } else {
                prefs.remove(KEY_PENDING_JOIN_OWNER)
            }
        }
    }

    /**
     * Забыть намерение: выход из аккаунта, исполненное вступление либо
     * терминальный отказ сервера.
     *
     * Парного «забрать и сразу стереть» здесь НЕТ намеренно: намерение читается
     * потоком [pending], а стирается только после ответа сервера. Пока чтение и
     * очистка были одним действием, одна попытка вступить без сети сжигала
     * приглашение навсегда — ссылку присылают в мессенджере один раз
     * (см. `AppRoot.joinPending`).
     */
    suspend fun clear() {
        dataStore.edit { prefs ->
            prefs.remove(KEY_PENDING_JOIN)
            prefs.remove(KEY_PENDING_JOIN_OWNER)
        }
    }

    /**
     * Свести намерение с вошедшим пользователем — порт iOS `adoptOwner`.
     *
     * Владельца ещё нет (ссылку открыл гость) — намерение достаётся [userId]:
     * это и есть штатный путь приглашения «ссылка → экран входа → вступление».
     * Владелец ДРУГОЙ — намерение выбрасывается: сессия предыдущего человека
     * могла протухнуть (её чистка приглашение намеренно СОХРАНЯЕТ, см.
     * [SessionEndReason.EXPIRED]), а вошёл на устройстве уже кто-то другой —
     * и без этой проверки он молча вступил бы в чужую приватную группу.
     *
     * Проверка и запись — в одной транзакции [DataStore.edit]; лишней записи на
     * диск при совпадении владельца не будет: DataStore не пишет файл, когда
     * содержимое не изменилось.
     */
    suspend fun reconcileOwner(userId: Long) {
        dataStore.edit { prefs ->
            if (prefs[KEY_PENDING_JOIN] == null) return@edit
            when (prefs[KEY_PENDING_JOIN_OWNER]) {
                userId -> {} // намерение уже наше
                null -> prefs[KEY_PENDING_JOIN_OWNER] = userId
                else -> {
                    prefs.remove(KEY_PENDING_JOIN)
                    prefs.remove(KEY_PENDING_JOIN_OWNER)
                }
            }
        }
    }
}
