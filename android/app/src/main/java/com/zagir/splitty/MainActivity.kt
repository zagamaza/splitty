package com.zagir.splitty

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.zagir.splitty.data.OfflineDataCleaner
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

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent {
            SplittyTheme {
                AppRoot()
            }
        }
    }

    override fun onStart() {
        super.onStart()
        // Триггер синка «приложение вернулось на передний план».
        outboxSyncer.syncNow()
    }
}
