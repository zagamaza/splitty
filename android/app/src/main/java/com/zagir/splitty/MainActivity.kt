package com.zagir.splitty

import com.zagir.splitty.core.analytics.AnalyticsEvent
import com.zagir.splitty.core.analytics.Analytics
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
import com.zagir.splitty.push.PushEventBus
import com.zagir.splitty.push.PushRoute
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

    /** Продуктовые события: возврат на передний план и отправка накопленного. */
    @Inject lateinit var analytics: Analytics

    /** Eager-создание: чистит офлайн-кеш и outbox при выходе из аккаунта. */
    @Inject lateinit var offlineDataCleaner: OfflineDataCleaner

    /** Настройка темы (system/light/dark) читается из сессии. */
    @Inject lateinit var sessionStore: SessionStore

    /** Кеш аватаров Telegram — провайдится всем GradientAvatar. */
    @Inject lateinit var avatarStore: AvatarStore

    /** Отложенное вступление в группу по ссылке-приглашению (исполняет AppRoot). */
    @Inject lateinit var pendingJoinStore: PendingJoinStore
    @Inject lateinit var telegramAuthBus: TelegramAuthBus

    /** Переход по тапу на push-уведомление (исполняет MainScaffold). */
    @Inject lateinit var pushEventBus: PushEventBus

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
        if (savedInstanceState == null) {
            handleDeepLink(intent)
            handlePushTap(intent)
        }
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
     * Ссылка или тап по пушу пришли в ЖИВОЕ приложение. Вызывается только
     * благодаря `android:launchMode="singleTop"` в манифесте: при standard
     * система создала бы второй экземпляр Activity и позвала [onCreate].
     *
     * Для пушей это ОСНОВНОЙ путь: приложение почти всегда уже запущено, и
     * разбор только в [onCreate] означал бы «тап не делает ничего».
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        // Обязательно: getIntent() иначе продолжит отдавать интент запуска, и
        // следующее пересоздание Activity разобрало бы устаревшую ссылку.
        setIntent(intent)
        handleDeepLink(intent)
        handlePushTap(intent)
    }

    override fun onStart() {
        super.onStart()
        // Триггер синка «приложение вернулось на передний план».
        outboxSyncer.syncNow()
        // Холодный старт ловит Application.onCreate, но он бывает один раз за
        // жизнь процесса: без этого «открыл приложение» считалось бы только у
        // тех, у кого систему выгрузила память.
        if (startedOnce) analytics.track(AnalyticsEvent.AppOpen(cold = false))
        startedOnce = true
    }

    /** Первый onStart идёт сразу за холодным стартом — его уже посчитали. */
    private var startedOnce = false

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
            val payload = TelegramWebAuth.decode(data)
            if (payload != null) telegramAuthBus.post(payload) else telegramAuthBus.postFailure()
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

    /**
     * Тап по push-уведомлению: комната или карточка операции из extras.
     *
     * Extras кладём либо мы сами (форграунд, [com.zagir.splitty.push.SplittyMessagingService]),
     * либо FCM, когда уведомление рисовал системный трей, — ключи одни и те же.
     * Как и приглашение по ссылке, намерение здесь только ЗАПОМИНАЕТСЯ: на
     * холодном старте активити просыпается раньше, чем прочитана сессия и
     * собран корневой экран, а без входа переходить всё равно некуда. Исполняет
     * его `MainScaffold` — сразу же либо после входа.
     */
    private fun handlePushTap(intent: Intent?) {
        if (intent == null) return
        // Перезапуск из «недавних»: система отдаёт ТОТ ЖЕ интент, которым таск
        // был создан, — иначе однажды тапнутый пуш уносил бы в ту же комнату
        // при каждом возврате в приложение (та же болезнь, что у ссылок выше).
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val route = PushRoute.fromIntent(intent) ?: return
        // Помечаем интент израсходованным: он переживает пересоздание активити
        // (поворот экрана) и без очистки открывал бы ту же комнату заново.
        intent.removeExtra(PushRoute.KEY_ROOM_ID)
        intent.removeExtra(PushRoute.KEY_OPERATION_ID)
        intent.removeExtra(PushRoute.KEY_TYPE)
        setIntent(intent)
        // Владелец намерения — тот, кто в аккаунте ПРЯМО СЕЙЧАС (null на
        // холодном старте: сессия ещё читается с диска). Дальше см.
        // `AppRootViewModel.pushRoute` — чужому аккаунту намерение не достанется.
        pushEventBus.postRoute(route, sessionStore.state.value?.me?.id)
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
