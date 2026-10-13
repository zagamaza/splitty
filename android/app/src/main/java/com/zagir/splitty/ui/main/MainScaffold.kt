package com.zagir.splitty.ui.main

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import com.zagir.splitty.core.ui.UiText
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.foundation.layout.offset
import com.zagir.splitty.R
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PushEventBus
import com.zagir.splitty.push.PushRoute
import com.zagir.splitty.ui.activity.ActivityScreen
import com.zagir.splitty.ui.expense.AddExpenseScreen
import com.zagir.splitty.ui.friends.FriendDetailScreen
import com.zagir.splitty.ui.friends.FriendsListScreen
import com.zagir.splitty.ui.groups.GroupDetailScreen
import com.zagir.splitty.ui.groups.GroupsListScreen
import com.zagir.splitty.ui.groups.OperationDetailScreen
import com.zagir.splitty.ui.profile.ProfileScreen
import com.zagir.splitty.ui.profile.NotificationSettingsScreen
import com.zagir.splitty.ui.settleup.SettleUpScreen
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.theme.Splitty
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

/**
 * Маршруты главного графа навигации. Вкладки — [FRIENDS]/[GROUPS]/[ACTIVITY]/
 * [ACCOUNT]; остальное — детальные экраны поверх вкладок (нижний бар скрыт).
 *
 * internal, а не private: тем же адресам, которые здесь собираются, обязан
 * соответствовать переход по тапу на push — тест сверяет его именно с ними.
 */
internal object MainRoutes {
    const val FRIENDS = "friends"
    const val GROUPS = "groups"
    const val ACTIVITY = "activity"
    const val ACCOUNT = "account"

    const val FRIEND_DETAIL = "friend/{userId}?name={name}"
    const val ROOM = "room/{roomId}"
    const val OPERATION = "room/{roomId}/operation/{operationId}"
    const val ADD_EXPENSE = "expense?roomId={roomId}&operationId={operationId}&localId={localId}"
    const val SETTLE_UP = "settleup/{roomId}?debtorId={debtorId}&lenderId={lenderId}"
    const val NOTIFICATIONS = "notifications"

    /** Вкладки: на них виден нижний бар и работает switchTab. */
    val tabs = setOf(FRIENDS, GROUPS, ACTIVITY, ACCOUNT)

    /** Вкладка, на которой открывается приложение (см. комментарий у NavHost). */
    const val START = GROUPS

    fun friendDetail(userId: Long, name: String) = "friend/$userId?name=${Uri.encode(name)}"
    fun room(roomId: String) = "room/$roomId"
    fun operation(roomId: String, operationId: String) = "room/$roomId/operation/$operationId"
    fun addExpense(roomId: String? = null) =
        if (roomId != null) "expense?roomId=$roomId" else "expense"
    fun editExpense(roomId: String, operationId: String) =
        "expense?roomId=$roomId&operationId=$operationId"

    /** Правка неотправленной (локальной) записи outbox. */
    fun editLocalExpense(roomId: String, localId: String) =
        "expense?roomId=$roomId&localId=$localId"

    fun settleUp(roomId: String) = "settleup/$roomId"

    /** Погашение с предвыбором конкретного долга (переход из строки балансов). */
    fun settleUpDebt(roomId: String, debtorId: Long, lenderId: Long) =
        "settleup/$roomId?debtorId=$debtorId&lenderId=$lenderId"
}

/**
 * Переход по тапу на push-уведомление.
 *
 * Карточка операции ложится ПОВЕРХ комнаты, а не вместо неё: «назад» с карточки
 * обязано вести в группу — иначе удалённая операция запирает человека на экране
 * «не найдено» без пути обратно.
 *
 * Повторный тап не должен наслаивать одинаковые экраны, а `launchSingleTop`
 * один этого не даёт: при переходе «комната → операция» наверху оказывается
 * операция, и комната легла бы копией. Поэтому решает не флаг, а текущий экран:
 * уже открытое не открывается заново. Отсюда же следствие — пуш о долге,
 * пришедший, когда человек и так внутри этой комнаты, никуда его не дёргает.
 */
internal fun NavHostController.openPushRoute(route: PushRoute) {
    when (route) {
        is PushRoute.Notifications -> {
            if (isAtRoute(MainRoutes.ACTIVITY)) return
            navigate(MainRoutes.ACTIVITY) {
                // Те же опции, что у обычного переключения таба: стек вкладок
                // сохраняется, второй копии раздела не появляется.
                popUpTo(graph.findStartDestination().id) { saveState = true }
                launchSingleTop = true
                restoreState = true
            }
        }

        is PushRoute.Room -> {
            if (isInRoom(route.roomId)) return
            navigate(MainRoutes.room(route.roomId)) { launchSingleTop = true }
        }

        is PushRoute.Operation -> {
            if (isAtOperation(route.roomId, route.operationId)) return
            if (!isInRoom(route.roomId)) {
                navigate(MainRoutes.room(route.roomId)) { launchSingleTop = true }
            }
            navigate(MainRoutes.operation(route.roomId, route.operationId)) {
                launchSingleTop = true
            }
        }
    }
}

private fun NavHostController.isAtRoute(route: String): Boolean =
    currentBackStackEntry?.destination?.route == route

/** Человек уже внутри этой комнаты — на её экране или на карточке её операции. */
private fun NavHostController.isInRoom(roomId: String): Boolean {
    val entry = currentBackStackEntry ?: return false
    val route = entry.destination.route
    if (route != MainRoutes.ROOM && route != MainRoutes.OPERATION) return false
    return entry.arguments?.getString("roomId") == roomId
}

private fun NavHostController.isAtOperation(roomId: String, operationId: String): Boolean {
    val entry = currentBackStackEntry ?: return false
    if (entry.destination.route != MainRoutes.OPERATION) return false
    val args = entry.arguments ?: return false
    return args.getString("roomId") == roomId && args.getString("operationId") == operationId
}

private data class TabSpec(
    val route: String,
    val icon: ImageVector,
    val labelRes: Int,
)

private val LeftTabs = listOf(
    TabSpec(MainRoutes.FRIENDS, Icons.Filled.Group, R.string.tab_friends),
    TabSpec(MainRoutes.GROUPS, Icons.Filled.Groups, R.string.tab_groups),
)

private val RightTabs = listOf(
    // Колокол, а не часы: раздел перестал быть журналом — в нём лежат
    // приглашения с кнопками. Маршрут остаётся ACTIVITY: имя NOTIFICATIONS
    // уже занято экраном настроек уведомлений в профиле.
    TabSpec(MainRoutes.ACTIVITY, Icons.Filled.Notifications, R.string.tab_activity),
    TabSpec(MainRoutes.ACCOUNT, Icons.Outlined.AccountCircle, R.string.tab_account),
)

/**
 * VM главного экрана: онлайн-статус и активный синк для глобального баннера
 * плюс счётчик непрочитанного для бейджа на табе.
 */
@HiltViewModel
class MainScaffoldViewModel @Inject constructor(
    networkMonitor: NetworkMonitor,
    outboxSyncer: OutboxSyncer,
    pushEventBus: PushEventBus,
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
) : ViewModel() {
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline
    val isSyncing: StateFlow<Boolean> = outboxSyncer.isSyncing

    /** Бейдж на табе «Уведомления»; источник — сессия, см. SessionStore. */
    val unreadNotifications: StateFlow<Int> = sessionStore.unreadNotifications

    /** Подтверждение последнего успешного действия (общий снекбар). */
    val successToast: StateFlow<UiText?> = sessionStore.successToast

    fun dismissToast() = sessionStore.dismissToast()

    init {
        // Пуш пришёл в ОТКРЫТОЕ приложение: `ON_START` до следующего
        // сворачивания уже не сработает, и бейдж оставался бы вчерашним —
        // человек видит баннер о новом расходе, а на колоколе прежнее число
        // (порт iOS .splittyPushReceived).
        viewModelScope.launch {
            pushEventBus.received.collect {
                // Не только счётчик: пуш означает, что данные на сервере
                // изменились — расход добавили, долг погасили. Раньше менялся
                // бейдж, а открытый экран продолжал показывать старые суммы,
                // пока человек не потянет список
                sessionStore.noteDataChanged()
                refreshUnreadCount()
            }
        }
    }

    /**
     * Перечитать счётчик. Зовётся на старте и на каждом возврате из фона —
     * бейдж обязан появляться ДО открытия раздела, иначе он показывался бы
     * ровно в момент, когда раздел его гасит. Тихо: сбой ничем не мигает.
     */
    /** Возврат из фона: данные могли измениться, пока приложение не смотрело. */
    fun onReturnedToForeground() {
        sessionStore.noteDataChanged()
        refreshUnreadCount()
    }

    fun refreshUnreadCount() {
        if (sessionStore.currentToken() == null) return
        viewModelScope.launch {
            runCatching { repository.unreadNotificationCount() }
                .onSuccess { sessionStore.setUnreadNotifications(it) }
        }
    }
}

/**
 * Главный экран: bottom bar на 5 позиций — Друзья, Группы, центральная
 * приподнятая кнопка «+» (форма добавления расхода), Активность, Профиль —
 * и NavHost со всеми экранами приложения. Аналог iOS MainTabView.
 * Сверху — глобальный тонкий баннер «Офлайн…»/«Отправка…».
 */
@Composable
fun MainScaffold(
    viewModel: MainScaffoldViewModel = hiltViewModel(),
    openRoomId: String? = null,
    onRoomOpened: () -> Unit = {},
    pushRoute: PushRoute? = null,
    onPushRouteHandled: () -> Unit = {},
) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()
    val isSyncing by viewModel.isSyncing.collectAsStateWithLifecycle()
    val unread by viewModel.unreadNotifications.collectAsStateWithLifecycle()
    val colors = Splitty.colors
    val haptics = rememberHaptics()

    // Старт и каждый возврат из фона: бейдж обязан быть виден до того, как
    // человек откроет раздел (порт iOS .task + scenePhase в SplittyApp).
    LifecycleEventEffect(Lifecycle.Event.ON_START) {
        // Пока приложение было в фоне, в группах могли появиться расходы и
        // погашения: обновляем не только бейдж, но и сами экраны
        viewModel.onReturnedToForeground()
    }

    // Вступили в группу по ссылке-приглашению — открываем её.
    LaunchedEffect(openRoomId) {
        if (openRoomId == null) return@LaunchedEffect
        // launchSingleTop: повторный диплинк в ту же группу (onNewIntent при
        // живом приложении) иначе клал бы в стек второй, третий… одинаковых
        // экрана группы — и «назад» пришлось бы жать столько же раз.
        navController.navigate(MainRoutes.room(openRoomId)) { launchSingleTop = true }
        // Гасим намерение сразу: иначе оно доживёт до следующего пересоздания
        // корня и комната откроется второй раз поверх первой.
        onRoomOpened()
    }

    // Тап по push-уведомлению: комната, карточка операции или раздел
    // «Уведомления» (приглашение).
    LaunchedEffect(pushRoute) {
        if (pushRoute == null) return@LaunchedEffect
        navController.openPushRoute(pushRoute)
        // Гасим сразу — по тем же причинам, что и намерение из ссылки выше.
        onPushRouteHandled()
    }

    fun switchTab(route: String) {
        navController.navigate(route) {
            popUpTo(navController.graph.findStartDestination().id) { saveState = true }
            launchSingleTop = true
            restoreState = true
        }
    }

    // Одно место на всё приложение: подтверждение действия («Погашение
    // записано») вместо пяти разных плашек или, как было, молчания
    val successToast by viewModel.successToast.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    val toastContext = LocalContext.current
    val toastText = successToast?.resolve(toastContext)
    LaunchedEffect(successToast) {
        val text = toastText ?: return@LaunchedEffect
        snackbarHostState.showSnackbar(text, duration = SnackbarDuration.Short)
        viewModel.dismissToast()
    }

    Scaffold(
        containerColor = colors.bg,
        snackbarHost = { SnackbarHost(snackbarHostState) },
        bottomBar = {
            // Нижний бар — только на вкладках; на детальных экранах скрыт.
            if (currentRoute in MainRoutes.tabs) {
                // Строка чека вместо Material NavigationBar: вкладки — текстом,
                // «+» — обычный элемент ряда (для TalkBack одно действие).
                ReceiptTabBar(
                    currentRoute = currentRoute,
                    unread = unread,
                    onTab = ::switchTab,
                    onAdd = {
                        haptics.tap()
                        navController.navigate(MainRoutes.addExpense())
                    },
                )
            }
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .background(colors.bg),
        ) {
            ConnectivityBanner(isOnline = isOnline, isSyncing = isSyncing)
            MainNavHost(
                navController = navController,
                onSwitchTab = ::switchTab,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            )
        }
    }
}

/**
 * Глобальный тонкий баннер состояния сети: офлайн — «изменения сохраняются
 * локально» (wifi-off), при активной досылке outbox — «Отправка…».
 * Онлайн без синка — баннера нет.
 */
@Composable
private fun ConnectivityBanner(isOnline: Boolean, isSyncing: Boolean) {
    val colors = Splitty.colors
    val (icon, textRes) = when {
        !isOnline -> Icons.Outlined.WifiOff to R.string.offline_banner_offline
        isSyncing -> Icons.Outlined.CloudUpload to R.string.offline_banner_syncing
        else -> return
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.size(14.dp),
        )
        Text(
            text = stringResource(textRes),
            modifier = Modifier.padding(start = 6.dp),
            fontSize = 13.sp,
            color = colors.inkSecondary,
            maxLines = 1,
        )
    }
}

/** Все destinations приложения; детальные экраны — поверх вкладок. */
@Composable
private fun MainNavHost(
    navController: NavHostController,
    onSwitchTab: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    NavHost(
        navController = navController,
        // Стартовая вкладка — «Группы»: новый аккаунт открывался на «Друзьях»,
        // где по определению пусто. Диплинки (room/operation) перебивают её.
        startDestination = MainRoutes.START,
        modifier = modifier,
    ) {
        // --- Вкладки ---

        composable(MainRoutes.FRIENDS) {
            FriendsListScreen(
                onOpenFriend = { friend ->
                    navController.navigate(
                        MainRoutes.friendDetail(friend.user.id, friend.user.displayName),
                    )
                },
                // Друзья появляются из общих групп — «Создать группу» ведёт на вкладку.
                onCreateGroup = { onSwitchTab(MainRoutes.GROUPS) },
            )
        }

        composable(MainRoutes.GROUPS) {
            GroupsListScreen(
                onOpenRoom = { roomId -> navController.navigate(MainRoutes.room(roomId)) },
            )
        }

        composable(MainRoutes.ACTIVITY) {
            ActivityScreen(
                onOpenRoom = { roomId -> navController.navigate(MainRoutes.room(roomId)) },
            )
        }

        composable(MainRoutes.ACCOUNT) {
            ProfileScreen(
                onOpenNotifications = { navController.navigate(MainRoutes.NOTIFICATIONS) },
            )
        }

        composable(MainRoutes.NOTIFICATIONS) {
            NotificationSettingsScreen(onBack = { navController.popBackStack() })
        }

        // --- Детальные экраны ---

        composable(
            route = MainRoutes.FRIEND_DETAIL,
            arguments = listOf(
                navArgument("userId") { type = NavType.LongType },
                navArgument("name") {
                    type = NavType.StringType
                    defaultValue = ""
                },
            ),
        ) {
            // userId/name читает FriendDetailViewModel из SavedStateHandle.
            FriendDetailScreen(
                onBack = { navController.popBackStack() },
                onOpenRoom = { roomId -> navController.navigate(MainRoutes.room(roomId)) },
                onSettleUp = { roomId -> navController.navigate(MainRoutes.settleUp(roomId)) },
            )
        }

        composable(
            route = MainRoutes.ROOM,
            arguments = listOf(navArgument("roomId") { type = NavType.StringType }),
        ) { entry ->
            val roomId = entry.arguments?.getString("roomId").orEmpty()
            GroupDetailScreen(
                roomId = roomId,
                onBack = { navController.popBackStack() },
                onSettleUp = { id -> navController.navigate(MainRoutes.settleUp(id)) },
                onSettleUpDebt = { id, debtorId, lenderId ->
                    navController.navigate(MainRoutes.settleUpDebt(id, debtorId, lenderId))
                },
                onAddExpense = { id -> navController.navigate(MainRoutes.addExpense(id)) },
                onOpenOperation = { rid, operationId ->
                    navController.navigate(MainRoutes.operation(rid, operationId))
                },
                onEditLocalOperation = { rid, localId ->
                    navController.navigate(MainRoutes.editLocalExpense(rid, localId))
                },
            )
        }

        composable(
            route = MainRoutes.OPERATION,
            arguments = listOf(
                navArgument("roomId") { type = NavType.StringType },
                navArgument("operationId") { type = NavType.StringType },
            ),
        ) { entry ->
            OperationDetailScreen(
                roomId = entry.arguments?.getString("roomId").orEmpty(),
                operationId = entry.arguments?.getString("operationId").orEmpty(),
                onBack = { navController.popBackStack() },
                onEdit = { rid, opId ->
                    navController.navigate(MainRoutes.editExpense(rid, opId))
                },
            )
        }

        composable(
            route = MainRoutes.ADD_EXPENSE,
            arguments = listOf(
                navArgument("roomId") {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
                navArgument("operationId") {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
                navArgument("localId") {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) { entry ->
            AddExpenseScreen(
                roomId = entry.arguments?.getString("roomId"),
                operationId = entry.arguments?.getString("operationId"),
                localId = entry.arguments?.getString("localId"),
                onDone = { navController.popBackStack() },
            )
        }

        composable(
            route = MainRoutes.SETTLE_UP,
            arguments = listOf(
                navArgument("roomId") { type = NavType.StringType },
                // Предвыбор долга: -1 = аргумент не передан (обычный вход).
                navArgument("debtorId") {
                    type = NavType.LongType
                    defaultValue = -1L
                },
                navArgument("lenderId") {
                    type = NavType.LongType
                    defaultValue = -1L
                },
            ),
        ) { entry ->
            // debtorId/lenderId читает SettleUpViewModel из SavedStateHandle.
            SettleUpScreen(
                roomId = entry.arguments?.getString("roomId").orEmpty(),
                onDone = { navController.popBackStack() },
            )
        }
    }
}

/** Последнее ТОЧНОЕ значение счётчика (maxUnreadCount на сервере). */
private const val MAX_EXACT_UNREAD = 99

/**
 * Текст бейджа: null — бейджа нет вовсе.
 *
 * Сервер отдаёт точное число до 99, а 100 означает «больше 99»
 * (maxUnreadCount в internal/rest/notifications_feed.go). Рисовать потолок
 * числом нельзя: «100» выглядело бы точным количеством, которого никто не
 * считал. Порт iOS MainTabView.badgeLabel — правило обязано совпадать.
 */
internal fun badgeLabel(count: Int, overflow: String): String? = when {
    count <= 0 -> null
    count > MAX_EXACT_UNREAD -> overflow
    else -> count.toString()
}

/**
 * Нижняя строка чека вместо системной навигации (паритет iOS ReceiptTabBar):
 * вкладки — текстом, как в референсе docs/design/receipt/receipt/groups.png;
 * выбранная — точкой и плотным начертанием, остальные — подчёркнутыми
 * ссылками. «+» — плоская печать цвета действия вровень со строкой. Не
 * помещаются подписи (крупный шрифт, длинный язык) — строка переходит на значки.
 */
@Composable
private fun ReceiptTabBar(
    currentRoute: String?,
    unread: Int,
    onTab: (String) -> Unit,
    onAdd: () -> Unit,
) {
    val colors = Splitty.colors
    val haptics = rememberHaptics()
    val density = LocalDensity.current
    val measurer = rememberTextMeasurer()
    val badge = badgeLabel(unread, stringResource(R.string.notifications_badge_overflow))
    val tabs = LeftTabs + RightTabs
    val labels = tabs.map { stringResource(it.labelRes) }
    val style = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
    Column(
        Modifier
            .fillMaxWidth()
            .background(colors.bg)
            .windowInsetsPadding(WindowInsets.navigationBars),
    ) {
        HorizontalDivider(thickness = 1.dp, color = colors.hairline)
        BoxWithConstraints(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp)) {
            // Вместимость — по настоящим ширинам, и та же, что займут пункты:
            // бейдж «99+» зарезервирован всегда (см. ReceiptTab). Иначе
            // режим и зазоры менялись от выбора и счётчика, и «+» ездил.
            val badgeWidth = with(density) { measurer.measure(BADGE_RESERVE, BadgeStyle).size.width.toDp() } + 8.dp + 5.dp
            val tabsWidth = tabs.mapIndexed { index, tab ->
                val label = with(density) { measurer.measure(labels[index], style).size.width.toDp() }
                val withBadge = if (tab.route == MainRoutes.ACTIVITY) badgeWidth else 0.dp
                maxOf(48.dp, label + withBadge)
            }.fold(0.dp) { acc, w -> acc + w }
            val iconsOnly = tabsWidth + 48.dp + 6.dp * tabs.size > maxWidth
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                tabs.forEachIndexed { index, tab ->
                    if (index == LeftTabs.size) ReceiptAddButton(onAdd)
                    ReceiptTab(
                        label = labels[index],
                        icon = tab.icon,
                        selected = currentRoute == tab.route,
                        iconsOnly = iconsOnly,
                        badge = if (tab.route == MainRoutes.ACTIVITY) badge else null,
                        reservesBadge = tab.route == MainRoutes.ACTIVITY,
                        onClick = {
                            if (currentRoute != tab.route) haptics.tap()
                            onTab(tab.route)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun ReceiptTab(
    label: String,
    icon: ImageVector,
    selected: Boolean,
    iconsOnly: Boolean,
    badge: String?,
    reservesBadge: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .defaultMinSize(minWidth = 48.dp, minHeight = 48.dp)
            .semantics(mergeDescendants = true) { this.selected = selected }
            .clickable(role = Role.Tab, onClick = onClick),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp, Alignment.CenterHorizontally),
    ) {
        // Ширина пункта не зависит ни от выбора, ни от счётчика: жирное
        // начертание и бейдж «99+» занимают место всегда.
        Box(contentAlignment = Alignment.CenterStart) {
            if (iconsOnly) {
                Icon(icon, contentDescription = label, tint = if (selected) colors.ink else colors.inkSecondary)
            } else {
                Text(text = label, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, modifier = Modifier.alpha(0f).clearAndSetSemantics {})
                Text(
                    text = label,
                    color = colors.ink,
                    fontSize = 14.sp,
                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                    textDecoration = if (selected) null else TextDecoration.Underline,
                    maxLines = 1,
                )
            }
            // Точка — в зазоре перед подписью, ширины не занимает; и в режиме
            // значков: выбор виден формой, а не только цветом.
            if (selected) {
                Box(Modifier.offset(x = (-9).dp).size(5.dp).clip(CircleShape).background(colors.ink))
            }
        }
        if (reservesBadge) {
            Box {
                TabBadge(BADGE_RESERVE, Modifier.alpha(0f).clearAndSetSemantics {})
                if (badge != null) TabBadge(badge)
            }
        }
    }
}

@Composable
private fun TabBadge(text: String, modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Box(
        modifier = modifier
            .defaultMinSize(minWidth = 16.dp, minHeight = 16.dp)
            .clip(RoundedCornerShape(2.dp))
            .background(colors.accent)
            .padding(horizontal = 4.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, color = colors.onAccent, style = BadgeStyle)
    }
}

/** Самый широкий бейдж: под него место у «Уведомлений» занято всегда. */
private const val BADGE_RESERVE = "99+"

private val BadgeStyle = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Bold, fontFamily = FontFamily.Monospace)

/** «+» в строке: плоская печать 44×36 цвета действия в зоне нажатия 48×48. */
@Composable
private fun ReceiptAddButton(onClick: () -> Unit) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .size(48.dp)
            .clickable(role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            modifier = Modifier
                .size(width = 44.dp, height = 36.dp)
                .clip(RoundedCornerShape(3.dp))
                .background(colors.accent),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Filled.Add,
                contentDescription = stringResource(R.string.tab_add),
                tint = colors.onAccent,
                modifier = Modifier.size(22.dp),
            )
        }
    }
}
