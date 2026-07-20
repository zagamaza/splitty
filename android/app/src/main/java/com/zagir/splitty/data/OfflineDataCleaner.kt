package com.zagir.splitty.data

import android.util.Log
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

private const val TAG = "OfflineDataCleaner"

/**
 * Очистка офлайн-данных при выходе: наблюдает за сессией и на переходе
 * «токен был → токена нет» (кнопка «Выйти» и глобальный разлогин по 401)
 * стирает офлайн-кеш GET-ответов и outbox — данные аккаунта не должны
 * пережить сессию. Создаётся при старте (инжектится в MainActivity).
 *
 * Второй триггер — СМЕНА аккаунта: если id профиля изменился, а перехода
 * «был → нет» мы не увидели (нерасшифровываемый шифротокен держит
 * hasStoredToken=true вечно), данные предыдущего пользователя иначе
 * достались бы новому. Проверка живёт здесь, а не в SessionStore: слой
 * сессии не должен знать про data-слой (иначе цикл в Hilt), а всё нужное —
 * `session.me?.id` — и так есть в наблюдаемом состоянии.
 */
@Singleton
class OfflineDataCleaner @Inject constructor(
    sessionStore: SessionStore,
    private val cache: ApiCache,
    private val outbox: OutboxStore,
    private val avatars: AvatarStore,
    @ApplicationScope scope: CoroutineScope,
) {
    init {
        scope.launch {
            var hadToken = false
            var lastUserId: Long? = null
            sessionStore.state.collect { session ->
                if (session == null) return@collect // DataStore ещё читается
                // Сбой чтения DataStore отдаёт пустую сессию-заглушку, неотличимую
                // от разлогина. Пропускаем её целиком (в т.ч. не трогаем hadToken):
                // иначе один IO-сбой или CorruptionException необратимо стирал
                // очередь неотправленных расходов при живых ключах на диске.
                if (session.readFailed) return@collect
                // Признак — наличие учётных данных на диске, а не расшифрованный
                // токен: transient-сбой Keystore даёт token = null при живых
                // ключах и стирал бы всю очередь неотправленных расходов.
                val hasToken = session.hasStoredToken
                val userId = session.me?.id
                // Сменился владелец учётных данных — данные прошлого стираем даже
                // без промежуточного «токена нет». null (профиль не прочитался)
                // сменой НЕ считается: это тот же transient-сбой, а не другой вход.
                val switchedAccount =
                    hasToken && userId != null && lastUserId != null && userId != lastUserId

                if ((hadToken && !hasToken) || switchedAccount) {
                    if (clearAll()) {
                        hadToken = hasToken
                        lastUserId = userId
                    }
                    // Хотя бы одна чистка не удалась — переход НЕ считаем
                    // обработанным: hadToken/lastUserId остаются прежними, и
                    // следующая эмиссия сессии повторит попытку. Иначе данные
                    // предыдущего аккаунта оставались на диске до переустановки.
                    return@collect
                }
                hadToken = hasToken
                if (userId != null) lastUserId = userId
            }
        }
    }

    /**
     * Стирает офлайн-данные аккаунта. Каждая чистка изолирована: без runCatching
     * ошибка записи в одной из них вылетала из collect и НАВСЕГДА убивала
     * подписку — все последующие разлогины переставали чистить что-либо.
     * true — все три прошли успешно.
     */
    private suspend fun clearAll(): Boolean {
        var ok = true
        suspend fun step(name: String, block: suspend () -> Unit) {
            runCatching { block() }.onFailure {
                ok = false
                Log.e(TAG, "не удалось очистить $name", it)
            }
        }
        step("api-cache") { cache.clear() }
        step("outbox") { outbox.clear() }
        step("avatars") { avatars.clear() }
        return ok
    }
}
