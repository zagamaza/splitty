package com.zagir.splitty.ui.main

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
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
import com.zagir.splitty.data.OutboxSyncer
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

/**
 * Маршруты главного графа навигации. Вкладки — [FRIENDS]/[GROUPS]/[ACTIVITY]/
 * [ACCOUNT]; остальное — детальные экраны поверх вкладок (нижний бар скрыт).
 */
private object MainRoutes {
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
    // clock — согласованно с empty state ленты (BarChart обещал графики).
    TabSpec(MainRoutes.ACTIVITY, Icons.Filled.Schedule, R.string.tab_activity),
    TabSpec(MainRoutes.ACCOUNT, Icons.Outlined.AccountCircle, R.string.tab_account),
)

/** VM главного экрана: онлайн-статус и активный синк для глобального баннера. */
@HiltViewModel
class MainScaffoldViewModel @Inject constructor(
    networkMonitor: NetworkMonitor,
    outboxSyncer: OutboxSyncer,
) : ViewModel() {
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline
    val isSyncing: StateFlow<Boolean> = outboxSyncer.isSyncing
}

/**
 * Главный экран: bottom bar на 5 позиций — Друзья, Группы, центральная
 * приподнятая кнопка «+» (форма добавления расхода), Активность, Профиль —
 * и NavHost со всеми экранами приложения. Аналог iOS MainTabView.
 * Сверху — глобальный тонкий баннер «Офлайн…»/«Отправка…».
 */
@Composable
fun MainScaffold(viewModel: MainScaffoldViewModel = hiltViewModel()) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()
    val isSyncing by viewModel.isSyncing.collectAsStateWithLifecycle()
    val colors = Splitty.colors

    fun switchTab(route: String) {
        navController.navigate(route) {
            popUpTo(navController.graph.findStartDestination().id) { saveState = true }
            launchSingleTop = true
            restoreState = true
        }
    }

    Scaffold(
        containerColor = colors.bg,
        bottomBar = {
            // Нижний бар — только на вкладках; на детальных экранах скрыт.
            if (currentRoute in MainRoutes.tabs) {
                NavigationBar(containerColor = colors.surface) {
                    LeftTabs.forEach { tab -> TabItem(tab, currentRoute, ::switchTab) }
                    // Центральная позиция — приподнятая кнопка «+».
                    NavigationBarItem(
                        selected = false,
                        onClick = { navController.navigate(MainRoutes.addExpense()) },
                        icon = { AddExpenseFab() },
                        colors = NavigationBarItemDefaults.colors(
                            indicatorColor = Color.Transparent,
                        ),
                    )
                    RightTabs.forEach { tab -> TabItem(tab, currentRoute, ::switchTab) }
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
        startDestination = MainRoutes.FRIENDS,
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

@Composable
private fun RowScope.TabItem(
    tab: TabSpec,
    currentRoute: String?,
    onClick: (String) -> Unit,
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
        icon = { Icon(tab.icon, contentDescription = null) },
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
private fun AddExpenseFab() {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .offset(y = (-12).dp)
            .size(58.dp)
            .shadow(
                elevation = 10.dp,
                shape = CircleShape,
                ambientColor = colors.accent.copy(alpha = 0.35f),
                spotColor = colors.accent.copy(alpha = 0.35f),
            )
            .background(
                brush = Brush.linearGradient(
                    colors = listOf(colors.accent, colors.accentPressed),
                ),
                shape = CircleShape,
            ),
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
