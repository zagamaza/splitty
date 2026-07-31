package com.zagir.splitty.core.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.stringPreferencesKey
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

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
 * Хранится только код комнаты — публичный идентификатор из ссылки, которую и
 * так переслали в мессенджере; ничего от личности пользователя тут нет. Тем не
 * менее намерение обязано умирать вместе с сессией — чистит его
 * [com.zagir.splitty.data.OfflineDataCleaner], иначе следующий человек на этом
 * устройстве молча вступил бы в чужую группу.
 */
@Singleton
class PendingJoinStore @Inject constructor(
    private val dataStore: DataStore<Preferences>,
) {
    companion object {
        private val KEY_PENDING_JOIN = stringPreferencesKey("pending_join_room_id")
    }

    /**
     * Код комнаты, ожидающий вступления; null — ждать нечего.
     *
     * Поток, а не разовое чтение: ссылка приходит и в ЖИВОЕ приложение
     * (`onNewIntent`), и корень обязан отреагировать на неё, не пересоздаваясь.
     * Сбой чтения DataStore отдаём как «намерения нет»: висящий диплинк — не
     * та причина, по которой стоит ронять корень приложения.
     */
    val pending: Flow<String?> = dataStore.data
        .catch { emit(emptyPreferences()) }
        .map { it[KEY_PENDING_JOIN] }
        .distinctUntilChanged()

    /** Запомнить намерение (пришла ссылка). */
    suspend fun set(roomId: String) {
        dataStore.edit { it[KEY_PENDING_JOIN] = roomId }
    }

    /**
     * Забрать намерение и сразу очистить. Чтение и очистка — в ОДНОЙ транзакции
     * [DataStore.edit] (она сериализуется): иначе две подписки, проснувшиеся на
     * одной эмиссии, отправили бы два запроса на вступление.
     */
    suspend fun take(): String? {
        var taken: String? = null
        dataStore.edit { prefs ->
            taken = prefs[KEY_PENDING_JOIN]
            prefs.remove(KEY_PENDING_JOIN)
        }
        return taken
    }

    /** Забыть намерение (выход из аккаунта). */
    suspend fun clear() {
        dataStore.edit { it.remove(KEY_PENDING_JOIN) }
    }
}
