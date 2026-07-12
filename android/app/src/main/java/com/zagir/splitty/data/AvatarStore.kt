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

    /** Запросить аватар, если его ещё нет в кеше (fire-and-forget). */
    fun request(userId: Long) {
        synchronized(this) {
            if (_images.value.containsKey(userId) ||
                userId in missing || userId in inflight
            ) {
                return
            }
            inflight += userId
        }
        scope.launch {
            try {
                val bytes = repository.userAvatar(userId)
                val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
                synchronized(this@AvatarStore) {
                    if (bitmap != null) {
                        _images.value = _images.value + (userId to bitmap.asImageBitmap())
                    } else {
                        missing += userId
                    }
                }
            } catch (e: ApiException) {
                if (e.status == 404) {
                    synchronized(this@AvatarStore) { missing += userId }
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
            _images.value = emptyMap()
            missing.clear()
        }
    }
}
