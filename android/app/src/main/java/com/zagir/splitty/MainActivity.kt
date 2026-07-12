package com.zagir.splitty

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.runtime.CompositionLocalProvider
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.AvatarStore
import com.zagir.splitty.data.OfflineDataCleaner
import com.zagir.splitty.ui.components.LocalAvatarStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.ui.AppRoot
import com.zagir.splitty.ui.theme.SplittyTheme
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/** Единственная Activity приложения; весь UI — Compose. */
@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    /** Досылка outbox; инжект здесь запускает наблюдение за сетью со старта. */
    @Inject lateinit var outboxSyncer: OutboxSyncer

    /** Eager-создание: чистит офлайн-кеш и outbox при выходе из аккаунта. */
    @Inject lateinit var offlineDataCleaner: OfflineDataCleaner

    /** Настройка темы (system/light/dark) читается из сессии. */
    @Inject lateinit var sessionStore: SessionStore

    /** Кеш аватаров Telegram — провайдится всем GradientAvatar. */
    @Inject lateinit var avatarStore: AvatarStore

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            val session by sessionStore.state.collectAsStateWithLifecycle()
            val darkTheme = when (session?.theme) {
                SessionStore.THEME_LIGHT -> false
                SessionStore.THEME_DARK -> true
                else -> isSystemInDarkTheme()
            }
            SplittyTheme(darkTheme = darkTheme) {
                CompositionLocalProvider(LocalAvatarStore provides avatarStore) {
                    AppRoot()
                }
            }
        }
    }

    override fun onStart() {
        super.onStart()
        // Триггер синка «приложение вернулось на передний план».
        outboxSyncer.syncNow()
    }
}
