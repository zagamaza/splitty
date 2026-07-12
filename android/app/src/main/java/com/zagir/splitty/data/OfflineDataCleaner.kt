package com.zagir.splitty.data

import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * Очистка офлайн-данных при выходе: наблюдает за сессией и на переходе
 * «токен был → токена нет» (кнопка «Выйти» и глобальный разлогин по 401)
 * стирает офлайн-кеш GET-ответов и outbox — данные аккаунта не должны
 * пережить сессию. Создаётся при старте (инжектится в MainActivity).
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
            sessionStore.state.collect { session ->
                if (session == null) return@collect // DataStore ещё читается
                val hasToken = session.token != null
                if (hadToken && !hasToken) {
                    cache.clear()
                    outbox.clear()
                    avatars.clear()
                }
                hadToken = hasToken
            }
        }
    }
}
