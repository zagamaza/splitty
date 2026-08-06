package com.zagir.splitty

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.runtime.CompositionLocalProvider
import com.zagir.splitty.core.auth.TelegramAuthBus
import com.zagir.splitty.core.auth.TelegramWebAuth
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.AvatarStore
import com.zagir.splitty.data.OfflineDataCleaner
import com.zagir.splitty.di.ApplicationScope
import com.zagir.splitty.ui.components.LocalAvatarStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.ui.AppRoot
import com.zagir.splitty.ui.groups.parseRoomCode
import com.zagir.splitty.ui.theme.SplittyTheme
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

private const val TAG = "MainActivity"

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

    /** Отложенное вступление в группу по ссылке-приглашению (исполняет AppRoot). */
    @Inject lateinit var pendingJoinStore: PendingJoinStore
    @Inject lateinit var telegramAuthBus: TelegramAuthBus

    /**
     * Скоуп приложения для записи диплинка: на `lifecycleScope` запись в
     * DataStore отменялась вместе с активити. Ровно этот путь и рвётся чаще
     * всего — по ссылке приложение стартует и почти сразу уходит в системный
     * лист входа, а слабое устройство может нас за это время убить; отменённая
     * корутина уносила приглашение до диска.
     */
    @Inject @ApplicationScope lateinit var appScope: CoroutineScope

    // Разрешение на пуши (Android 13+); отказ не критичен — токен всё равно
    // регистрируется, просто система не покажет баннер.
    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        requestNotificationPermissionIfNeeded()
        // Только на ПЕРВОМ создании: при повороте экрана система заново отдаёт
        // тот же VIEW-интент, и без проверки savedInstanceState приложение
        // повторяло бы вступление в группу на каждом пересоздании Activity.
        if (savedInstanceState == null) handleDeepLink(intent)
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

    /**
     * Ссылка пришла в ЖИВОЕ приложение. Вызывается только благодаря
     * `android:launchMode="singleTop"` в манифесте: при standard система
     * создала бы второй экземпляр Activity и позвала [onCreate].
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        // Обязательно: getIntent() иначе продолжит отдавать интент запуска, и
        // следующее пересоздание Activity разобрало бы устаревшую ссылку.
        setIntent(intent)
        handleDeepLink(intent)
    }

    override fun onStart() {
        super.onStart()
        // Триггер синка «приложение вернулось на передний план».
        outboxSyncer.syncNow()
    }

    /**
     * Разбор ссылки-приглашения (`https://<domain>/join/<roomId>` и
     * `splitty://join/<roomId>`).
     *
     * Здесь намерение только ЗАПОМИНАЕТСЯ, а исполняет его `AppRoot`: на
     * холодном старте по ссылке интент приходит раньше, чем собран корневой
     * экран и прочитана сессия, а без авторизации вступать всё равно некуда.
     * Гость увидит экран входа, и вступление доедет само сразу после входа.
     */
    private fun handleDeepLink(intent: Intent?) {
        if (intent == null) return
        // Перезапуск из «недавних»/лаунчера: система отдаёт ТОТ ЖЕ интент,
        // которым таск был создан. Без этой проверки человек, однажды пришедший
        // по ссылке, при каждом возврате в приложение снова вступал бы в ту же
        // группу и снова проваливался в неё с любого экрана.
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val data = intent.data ?: return
        // Помечаем интент израсходованным ДО разбора: если состояние активити
        // выбросят (savedInstanceState == null), система при следующем запуске
        // из лаунчера отдаст базовый VIEW-интент таска повторно. setIntent
        // обязателен — getIntent() иначе продолжит отдавать ссылку.
        intent.data = null
        setIntent(intent)

        // Возврат из Telegram Login Widget: не приглашение, а результат входа —
        // отдаём экрану входа и выходим (см. TelegramWebAuth, tg_callback.go).
        if (TelegramWebAuth.isCallback(data)) {
            TelegramWebAuth.decode(data)?.let(telegramAuthBus::post)
            return
        }

        val roomId = parseRoomCode(data.toString()) ?: return
        // Скоуп приложения, а не lifecycleScope: уничтожение активити (поворот,
        // уход в системный лист входа, отстрел процесса) отменяло запись, и
        // приглашение терялось между «тапнул по ссылке» и «диск ответил».
        // Владелец намерения — тот, кто в аккаунте ПРЯМО СЕЙЧАС (null у гостя:
        // намерение достанется первому вошедшему — это и есть путь приглашения).
        // Запоминается здесь, а не при входе: пока человек залогинен, сессия не
        // эмитит ничего нового, и «свести» намерение с ним потом будет негде.
        val ownerId = sessionStore.state.value?.me?.id
        appScope.launch {
            // Отдельная ссылка не стоит падения приложения: единственное
            // последствие сбоя записи — вступление придётся начать заново.
            runCatching { pendingJoinStore.set(roomId, ownerId) }
                .onFailure { Log.w(TAG, "не удалось запомнить приглашение из ссылки", it) }
        }
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
}
