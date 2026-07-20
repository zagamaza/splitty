package com.zagir.splitty.data

import android.graphics.BitmapFactory
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * In-memory кеш аватаров из Telegram (GET /users/{id}/avatar через бэкенд).
 * 404 («нет фото») кешируется в [missing] — без повторных походов на каждый
 * список; сетевые ошибки НЕ кешируются, попробуем при следующем показе.
 * Порт ios/Splitty/Core/AvatarStore.swift. Чистится при logout
 * (см. [OfflineDataCleaner]).
 */
@Singleton
class AvatarStore @Inject constructor(
    private val repository: SplittyRepository,
    @ApplicationScope private val scope: CoroutineScope,
) {
    private val _images = MutableStateFlow<Map<Long, ImageBitmap>>(emptyMap())

    /** Загруженные аватары по telegram user id. */
    val images: StateFlow<Map<Long, ImageBitmap>> = _images.asStateFlow()

    private val missing = mutableSetOf<Long>()
    private val inflight = mutableSetOf<Long>()

    // Поколение кеша. clear() его увеличивает, и загрузка, стартовавшая до
    // разлогина, свой результат уже не запишет: иначе ответ, пришедший через
    // секунду после logout, возвращал в кеш аватар ПРЕДЫДУЩЕГО аккаунта —
    // ровно то, что OfflineDataCleaner был должен стереть.
    private var generation = 0

    /** Запросить аватар, если его ещё нет в кеше (fire-and-forget). */
    fun request(userId: Long) {
        val started: Int
        synchronized(this) {
            if (_images.value.containsKey(userId) ||
                userId in missing || userId in inflight
            ) {
                return
            }
            inflight += userId
            started = generation
        }
        scope.launch {
            try {
                val bytes = repository.userAvatar(userId)
                val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
                synchronized(this@AvatarStore) {
                    if (started != generation) return@synchronized
                    if (bitmap != null) {
                        _images.value = _images.value + (userId to bitmap.asImageBitmap())
                    } else {
                        missing += userId
                    }
                }
            } catch (e: ApiException) {
                if (e.status == 404) {
                    synchronized(this@AvatarStore) {
                        if (started == generation) missing += userId
                    }
                }
                // прочие ошибки (сеть) — не кешируем, попробуем ещё раз
            } finally {
                synchronized(this@AvatarStore) { inflight -= userId }
            }
        }
    }

    /** Полная очистка (logout). */
    fun clear() {
        synchronized(this) {
            generation++
            _images.value = emptyMap()
            missing.clear()
            // inflight не чистим: запущенные загрузки сами снимут свои id в
            // finally, а их результат отсечёт проверка поколения.
        }
    }
}
