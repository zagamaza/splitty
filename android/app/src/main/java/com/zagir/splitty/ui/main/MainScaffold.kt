package com.zagir.splitty.ui.main

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
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
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
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
                // Кнопку «+» рисуем ПОВЕРХ бара в Box (Box не клипует), а не как
                // icon у NavigationBarItem: NavigationBar клипует контент по своей
                // высоте, из-за чего приподнятая на -12dp кнопка обрезалась сверху.
                Box {
                    NavigationBar(containerColor = colors.surface) {
                        LeftTabs.forEach { tab -> TabItem(tab, currentRoute, ::switchTab) }
                        // Плейсхолдер центральной позиции: держит раскладку табов
                        // (сама кнопка — оверлеем ниже).
                        NavigationBarItem(
                            selected = false,
                            onClick = {
                                haptics.tap()
                                navController.navigate(MainRoutes.addExpense())
                            },
                            icon = { Spacer(Modifier.size(58.dp)) },
                            colors = NavigationBarItemDefaults.colors(
                                indicatorColor = Color.Transparent,
                            ),
                        )
                        RightTabs.forEach { tab ->
                            TabItem(
                                tab,
                                currentRoute,
                                ::switchTab,
                                badgeCount = if (tab.route == MainRoutes.ACTIVITY) unread else 0,
                            )
                        }
                    }
                    // Приподнятая кнопка «+» поверх бара, по центру; верхняя часть
                    // выступает над баром и больше не режется (порт iOS addExpenseButton).
                    AddExpenseFab(
                        onClick = {
                            // Тот же отклик, что у табов рядом (порт iOS
                            // MainTabView addExpenseButton).
                            haptics.tap()
                            navController.navigate(MainRoutes.addExpense())
                        },
                        modifier = Modifier.align(Alignment.TopCenter),
                    )
                }
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

@Composable
private fun RowScope.TabItem(
    tab: TabSpec,
    currentRoute: String?,
    onClick: (String) -> Unit,
    badgeCount: Int = 0,
) {
    val colors = Splitty.colors
    val haptics = rememberHaptics()
    NavigationBarItem(
        selected = currentRoute == tab.route,
        onClick = {
            // Лёгкий отклик на смену таба (порт iOS MainTabView Haptics.tap()).
            if (currentRoute != tab.route) haptics.tap()
            onClick(tab.route)
        },
        icon = {
            val badgeText = badgeLabel(badgeCount, stringResource(R.string.notifications_badge_overflow))
            if (badgeText != null) {
                BadgedBox(badge = { Badge { Text(badgeText) } }) {
                    Icon(tab.icon, contentDescription = null)
                }
            } else {
                Icon(tab.icon, contentDescription = null)
            }
        },
        label = {
            Text(text = stringResource(tab.labelRes), fontSize = 11.sp, maxLines = 1)
        },
        colors = NavigationBarItemDefaults.colors(
            selectedIconColor = colors.accent,
            selectedTextColor = colors.accent,
            unselectedIconColor = colors.inkSecondary,
            unselectedTextColor = colors.inkSecondary,
            indicatorColor = colors.accent.copy(alpha = 0.14f),
        ),
    )
}

/**
 * Центральная приподнятая кнопка добавления расхода: изумрудный градиент,
 * мягкая цветная тень, белый plus (порт addExpenseButton из iOS MainTabView).
 */
@Composable
private fun AddExpenseFab(onClick: () -> Unit, modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Box(
        modifier = modifier
            .offset(y = (-12).dp)
            .size(58.dp)
            .shadow(
                elevation = 10.dp,
                shape = CircleShape,
                ambientColor = colors.accent.copy(alpha = 0.35f),
                spotColor = colors.accent.copy(alpha = 0.35f),
            )
            .clip(CircleShape)
            .background(
                brush = Brush.linearGradient(
                    colors = listOf(colors.accent, colors.accentPressed),
                ),
                shape = CircleShape,
            )
            .clickable(role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = Icons.Filled.Add,
            contentDescription = stringResource(R.string.tab_add),
            tint = Color.White,
            modifier = Modifier.size(28.dp),
        )
    }
}
