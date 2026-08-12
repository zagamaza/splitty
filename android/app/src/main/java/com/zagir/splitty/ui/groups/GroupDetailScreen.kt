package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.ui.resolve
import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.offset
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.material.icons.outlined.PieChart
import androidx.compose.material.icons.automirrored.outlined.ReceiptLong
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.Attachment
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.PersonAddAlt
import androidx.compose.material.icons.outlined.Payments
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Unarchive
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.TextButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.res.pluralStringResource
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.operationsBlockingLeave
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.money.money
import com.zagir.splitty.data.OutboxEntry
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.theme.Splitty

/**
 * Экран группы: hero-карточка долга, чипы «Погасить долг»/«Балансы»/«Итоги»,
 * операции карточными секциями по месяцам, FAB «+ Расход», настройки.
 * Порт iOS GroupDetailView.
 *
 * [onSettleUp]/[onAddExpense] — открыть погашение долга / форму расхода
 * данной комнаты; [onOpenOperation] — карточка операции (roomId, operationId);
 * [onEditLocalOperation] — правка неотправленной записи outbox (roomId, localId).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GroupDetailScreen(
    roomId: String,
    onBack: () -> Unit,
    onSettleUp: (String) -> Unit,
    onSettleUpDebt: (String, Long, Long) -> Unit,
    onAddExpense: (String) -> Unit,
    onOpenOperation: (String, String) -> Unit,
    onEditLocalOperation: (String, String) -> Unit,
    viewModel: GroupDetailViewModel = hiltViewModel(),
) {
    LaunchedEffect(roomId) { viewModel.start(roomId) }

    val state by viewModel.room.collectAsStateWithLifecycle()
    val sections by viewModel.sections.collectAsStateWithLifecycle()
    val meIdState by viewModel.meId.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val alertMessage by viewModel.alertMessage.collectAsStateWithLifecycle()
    val localOperations by viewModel.localOperations.collectAsStateWithLifecycle()
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()

    // Погашения офлайн недоступны — алерт вместо перехода (дизайн v1).
    val settleUp: () -> Unit = {
        if (isOnline) onSettleUp(roomId) else viewModel.showSettleUpUnavailableOffline()
    }
    // Погашение конкретного долга из строки балансов (с предвыбором).
    val settleUpDebt: (Debt) -> Unit = { debt ->
        if (isOnline) {
            onSettleUpDebt(roomId, debt.debtor.id, debt.lender.id)
        } else {
            viewModel.showSettleUpUnavailableOffline()
        }
    }

    // Вкладка нижнего бара тусы: операции / балансы / итоги / настройки.
    var tusaTab by rememberSaveable { mutableStateOf(TUSA_TAB_OPS) }
    // Шит приглашения — открывается из баннера, участников и настроек.
    var isInvitePresented by rememberSaveable { mutableStateOf(false) }
    var isInviteFriendsPresented by rememberSaveable { mutableStateOf(false) }

    val colors = Splitty.colors
    val detail = (state as? UiState.Content)?.value
    val meId = meIdState

    Scaffold(
        containerColor = colors.bg,
        // Экран вложен в Scaffold MainScaffold, который системные инсеты УЖЕ
        // применил (на детальных экранах его bottomBar скрыт, и нижний инсет
        // уходит в innerPadding). Повторное применение здесь поднимало TusaBar
        // на высоту системной навигации — бар «прыгал» относительно вкладок.
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        topBar = {
            TopAppBar(
                windowInsets = WindowInsets(0, 0, 0, 0),
                title = {
                    Text(
                        text = detail?.name ?: stringResource(R.string.group_fallback_title),
                        fontWeight = FontWeight.Bold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.common_back),
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                    navigationIconContentColor = colors.ink,
                    actionIconContentColor = colors.ink,
                ),
            )
        },
        bottomBar = {
            TusaBar(
                selected = tusaTab,
                enabled = detail != null,
                onSelect = { tusaTab = it },
                onAdd = { onAddExpense(roomId) },
            )
        },
    ) { innerPadding ->
        when (val current = state) {
            UiState.Loading -> GroupsLoading(Modifier.padding(innerPadding))

            is UiState.Error -> GroupsFullScreenError(
                message = current.message,
                onRetry = viewModel::retry,
                modifier = Modifier.padding(innerPadding),
            )

            is UiState.Content -> if (meId == null) {
                // Профиль не загружен — нейтральное состояние вместо неверных
                // подписей «не участвует» с фейковым id.
                GroupsFullScreenError(
                    message = stringResource(R.string.group_profile_missing),
                    onRetry = viewModel::retry,
                    modifier = Modifier.padding(innerPadding),
                )
            } else {
                when (tusaTab) {
                    TUSA_TAB_BALANCES -> GroupBalancesTab(
                        room = current.value,
                        meId = meId,
                        onSettle = settleUpDebt,
                        modifier = Modifier.padding(innerPadding),
                    )

                    TUSA_TAB_TOTALS -> {
                        val dashboardViewModel: GroupDashboardViewModel = hiltViewModel()
                        LaunchedEffect(roomId) { dashboardViewModel.start(roomId) }
                        val dashboardState by dashboardViewModel.statistics.collectAsStateWithLifecycle()
                        val dashboardMeId by dashboardViewModel.meId.collectAsStateWithLifecycle()
                        GroupDashboardContent(
                            state = dashboardState,
                            meId = dashboardMeId,
                            onRetry = dashboardViewModel::retry,
                            modifier = Modifier
                                .padding(innerPadding)
                                .fillMaxSize(),
                        )
                    }

                    TUSA_TAB_SETTINGS -> GroupSettingsTab(
                        room = current.value,
                        meId = meId,
                        viewModel = viewModel,
                        onInvite = { isInvitePresented = true },
                        onInviteFriends = { isInviteFriendsPresented = true },
                        onLeft = onBack,
                        modifier = Modifier.padding(innerPadding),
                    )

                    else -> GroupDetailContent(
                        room = current.value,
                        sections = sections,
                        localOperations = localOperations,
                        meId = meId,
                        isRefreshing = isRefreshing,
                        onRefresh = viewModel::refresh,
                        onSettleUp = settleUp,
                        onAddExpense = { onAddExpense(roomId) },
                        // Выбор друзей, а не шит со ссылкой: главный призыв
                        // группы вёл мимо основного способа позвать человека.
                        // Ссылка осталась внутри пикера — она нужна тем, с кем
                        // расходы ещё не делили
                        onInvite = { isInviteFriendsPresented = true },
                        onOpenOperation = { operationId -> onOpenOperation(roomId, operationId) },
                        onEditLocalOperation = { localId -> onEditLocalOperation(roomId, localId) },
                        modifier = Modifier.padding(innerPadding),
                    )
                }
            }
        }
    }

    if (isInviteFriendsPresented && detail != null) {
        InviteFriendsSheet(
            room = detail,
            viewModel = viewModel,
            onLink = {
                isInviteFriendsPresented = false
                isInvitePresented = true
            },
            onDismiss = { isInviteFriendsPresented = false },
        )
    }
    if (isInvitePresented && detail != null) {
        InviteBottomSheet(room = detail, onDismiss = { isInvitePresented = false })
    }

    GroupsAlertDialog(alertMessage, viewModel::dismissAlert)
}

/**
 * Шит приглашения: главное действие — поделиться ссылкой (друг открывает её и
 * вступает в один тап), код — вторичный способ (тап копирует).
 * Порт iOS InviteGroupView.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InviteBottomSheet(room: RoomDetail, onDismiss: () -> Unit) {
    val colors = Splitty.colors
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val haptics = rememberHaptics()
    var copied by remember { mutableStateOf(false) }
    // Ссылка с сервера — единственная, которую понимает app link приложения
    // (`https://<domain>/join/<roomId>`, см. AndroidManifest): получатель
    // попадает сразу в группу, а без установленного приложения — на страницу
    // /join. Пока публичный домен у бэкенда не настроен, поля нет, и остаётся
    // легаси-ссылка бота: она работает всегда, но уводит в Telegram — поэтому
    // именно фолбэк, а не основной вариант.
    val inviteLink = room.inviteUrl?.takeIf { it.isNotBlank() }
        ?: "https://t.me/split_money_bot?start=room${room.id}"
    val inviteMessage = stringResource(R.string.group_invite_message, room.name, inviteLink, room.id)

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .padding(bottom = 36.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text(
                text = stringResource(R.string.invite_sheet_title),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
            PrimaryPillButton(
                text = stringResource(R.string.invite_share_link),
                onClick = {
                    val send = Intent(Intent.ACTION_SEND).apply {
                        type = "text/plain"
                        putExtra(Intent.EXTRA_TEXT, inviteMessage)
                    }
                    context.startActivity(Intent.createChooser(send, null))
                },
            )
            // Код — вторичный способ: строка, тап копирует в буфер.
            SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable {
                            clipboard.setText(AnnotatedString(room.id))
                            haptics.tap()
                            copied = true
                        }
                        .padding(horizontal = 16.dp, vertical = 14.dp),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = room.id,
                        fontSize = 13.sp,
                        fontFamily = FontFamily.Monospace,
                        color = colors.inkSecondary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(
                        imageVector = if (copied) Icons.Filled.Check else Icons.Outlined.ContentCopy,
                        contentDescription = null,
                        tint = colors.accent,
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
    }
}

/** Баннер «В группе только вы»: зовёт добавить друзей. Порт iOS inviteBanner. */
@Composable
internal fun InviteBanner(onClick: () -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(
                Brush.linearGradient(listOf(colors.accent, colors.accentPressed))
            )
            .clickable(onClick = onClick)
            .padding(16.dp),
        horizontalArrangement = Arrangement.spacedBy(13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.PersonAddAlt,
            contentDescription = null,
            tint = Color.White,
            modifier = Modifier.size(22.dp),
        )
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = stringResource(R.string.invite_banner_title),
                fontSize = 15.sp,
                fontWeight = FontWeight.Bold,
                color = Color.White,
            )
            Text(
                text = stringResource(R.string.invite_banner_subtitle),
                fontSize = 12.5.sp,
                color = Color.White.copy(alpha = 0.9f),
            )
        }
        Text(
            text = stringResource(R.string.invite_banner_action),
            fontSize = 13.5.sp,
            fontWeight = FontWeight.SemiBold,
            // Белый текст на полупрозрачной подложке поверх градиента давал
            // 2.6:1 — на ярком фоне надпись просто пропадала. Плашка
            // непрозрачная, текст — тёмный акцент
            color = colors.accentText,
            modifier = Modifier
                .clip(RoundedCornerShape(50))
                .background(Color.White)
                .padding(horizontal = 12.dp, vertical = 8.dp),
        )
    }
}

// MARK: - Контент

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GroupDetailContent(
    room: RoomDetail,
    sections: List<MonthSection>,
    localOperations: List<OutboxEntry>,
    meId: Long,
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    onSettleUp: () -> Unit,
    onAddExpense: () -> Unit,
    onInvite: () -> Unit,
    onOpenOperation: (String) -> Unit,
    onEditLocalOperation: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    // Фильтр «Со мной»: только операции, где я донор или в получателях
    // (аналог фильтра «Мои операции» в телеграм-боте).
    var isMineOnly by rememberSaveable { mutableStateOf(false) }
    val displaySections = if (isMineOnly) {
        sections.mapNotNull { section ->
            val ops = section.operations.filter { it.involves(meId) }
            if (ops.isEmpty()) null else section.copy(operations = ops)
        }
    } else {
        sections
    }
    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = modifier.fillMaxSize(),
    ) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            // Запас снизу — под FAB «+ Расход».
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            item(key = "hero") {
                DebtHeroCard(
                    room = room,
                    meId = meId,
                    pendingCount = localOperations.size,
                    onSettleUp = onSettleUp,
                )
            }
            // Пока в группе только вы — зовём добавить друзей.
            if (room.members.size <= 1 && !room.isArchived) {
                item(key = "invite-banner") {
                    InviteBanner(onClick = onInvite)
                }
            }
            item(key = "mine-segment") {
                MineSegment(isMineOnly = isMineOnly, onChange = { isMineOnly = it })
            }
            // Неотправленные (локальные) операции — всегда сверху списка.
            if (localOperations.isNotEmpty()) {
                item(key = "outbox") {
                    LocalOperationsCard(
                        entries = localOperations,
                        currency = room.currency,
                        onEdit = onEditLocalOperation,
                    )
                }
            }
            if (room.operations.isEmpty() && localOperations.isEmpty()) {
                item(key = "empty") {
                    GroupsEmptyCard(
                        icon = Icons.Outlined.Description,
                        title = stringResource(R.string.group_empty_ops_title),
                        subtitle = stringResource(R.string.group_empty_ops_subtitle),
                    ) {
                        PrimaryPillButton(
                            text = stringResource(R.string.group_add_expense),
                            onClick = onAddExpense,
                        )
                    }
                }
            } else {
                displaySections.forEach { section ->
                    item(key = "month-${section.month}") {
                        MonthSectionCard(
                            section = section,
                            meId = meId,
                            currency = room.currency,
                            onOpenOperation = onOpenOperation,
                        )
                    }
                }
                if (displaySections.isEmpty() && isMineOnly) {
                    item(key = "mine-empty") {
                        Text(
                            text = stringResource(R.string.group_mine_only_empty),
                            fontSize = 15.sp,
                            color = Splitty.colors.inkSecondary,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 24.dp),
                            textAlign = TextAlign.Center,
                        )
                    }
                }
            }
        }
    }
}

/** Hero-карточка статуса долга (+ бейдж архива, + подпись про outbox). */
@Composable
private fun DebtHeroCard(
    room: RoomDetail,
    meId: Long,
    pendingCount: Int = 0,
    onSettleUp: (() -> Unit)? = null,
) {
    val colors = Splitty.colors
    val canSettle = room.debts.any { it.debtor.id == meId || it.lender.id == meId }
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 20.dp) {
        if (room.isArchived) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = Icons.Outlined.Archive,
                    contentDescription = null,
                    tint = colors.inkSecondary,
                    modifier = Modifier.size(16.dp),
                )
                Text(
                    text = stringResource(R.string.group_archived_badge),
                    fontSize = 13.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.inkSecondary,
                )
            }
            Spacer(Modifier.height(10.dp))
        }
        when {
            // Долги неисчислимы (старые данные бота): честный бейдж вместо
            // ложного «все в расчёте» — сервер прислал debtsUnavailable.
            room.debtsUnavailable -> {
                Text(
                    text = stringResource(R.string.group_debts_unavailable_title),
                    fontSize = 22.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                )
                Spacer(Modifier.height(4.dp))
                Text(
                    text = stringResource(R.string.group_debts_unavailable_subtitle),
                    fontSize = 14.sp,
                    color = colors.inkSecondary,
                )
            }

            room.myBalance > 0 -> {
                SectionHeader(stringResource(R.string.group_you_are_owed))
                Spacer(Modifier.height(4.dp))
                MoneyText(room.myBalance, size = 40.sp, currency = room.currency)
            }

            room.myBalance < 0 -> {
                // Один кредитор — называем его по имени, как в iOS.
                val creditors = room.debts.filter { it.debtor.id == meId }
                if (creditors.size == 1) {
                    SectionHeader(
                        stringResource(R.string.group_you_owe_name, creditors.first().lender.displayName)
                    )
                    Spacer(Modifier.height(4.dp))
                    MoneyText(
                        creditors.first().sum,
                        role = MoneyRole.NEGATIVE,
                        size = 40.sp,
                        currency = room.currency,
                    )
                } else {
                    SectionHeader(stringResource(R.string.group_you_owe))
                    Spacer(Modifier.height(4.dp))
                    MoneyText(room.myBalance, size = 40.sp, currency = room.currency)
                }
            }

            else -> {
                Text(
                    text = stringResource(R.string.group_no_debts),
                    fontSize = 22.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                )
                Spacer(Modifier.height(4.dp))
                Text(
                    text = stringResource(R.string.group_members_settled),
                    fontSize = 14.sp,
                    color = colors.inkSecondary,
                )
            }
        }
        // «Погасить» живёт рядом с долгом, а не в общем ряду кнопок. Тот же
        // вид, что у «Погасить» в балансах — единый SoftChip; isSelected=true
        // делает чип акцентным (порт iOS `.softChip(isSelected: true)`).
        if (canSettle && onSettleUp != null) {
            Spacer(Modifier.height(12.dp))
            SoftChip(
                text = stringResource(R.string.group_chip_settle_short),
                onClick = onSettleUp,
                isSelected = true,
            )
        }
        // Балансы сервера не учитывают неотправленные офлайн-операции.
        if (pendingCount > 0) {
            Spacer(Modifier.height(6.dp))
            Text(
                text = pluralStringResource(
                    R.plurals.group_hero_pending_ops,
                    pendingCount,
                    pendingCount,
                ),
                fontSize = 13.sp,
                color = colors.inkSecondary,
            )
        }
    }
}

// MARK: - Неотправленные операции (outbox)

/** Карточка локальных операций комнаты: заголовок + строки с бейджами. */
@Composable
private fun LocalOperationsCard(
    entries: List<OutboxEntry>,
    currency: String,
    onEdit: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            stringResource(R.string.outbox_section_title),
            modifier = Modifier.padding(start = 4.dp),
        )
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            entries.forEachIndexed { index, entry ->
                LocalOperationRow(
                    entry = entry,
                    currency = currency,
                    onClick = { onEdit(entry.localId) },
                )
                if (index < entries.lastIndex) {
                    HairlineDivider(startIndent = 62.dp)
                }
            }
        }
    }
}

/**
 * Строка неотправленной операции: колонка даты, иконка cloud-off, описание
 * и бейдж «не отправлено» (failed — negative + текст ошибки сервера), сумма
 * справа нейтрально. Тап — правка локальной записи в форме расхода.
 */
@Composable
private fun LocalOperationRow(
    entry: OutboxEntry,
    currency: String,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    val badgeColor = if (entry.isFailed) colors.negative else colors.inkSecondary
    val badgeText = if (entry.isFailed) {
        entry.errorMessage ?: stringResource(R.string.common_error_title)
    } else {
        stringResource(R.string.outbox_badge_pending)
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(
            modifier = Modifier.width(34.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = GroupsDateFmt.monthShort(entry.createdAt),
                fontSize = 11.sp,
                color = colors.inkSecondary,
            )
            Text(
                text = GroupsDateFmt.day(entry.createdAt),
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.inkSecondary,
            )
        }
        Box(
            modifier = Modifier
                .size(36.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(colors.ink.copy(alpha = 0.06f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Outlined.CloudOff,
                contentDescription = null,
                tint = badgeColor,
                modifier = Modifier.size(18.dp),
            )
        }
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = entry.payload.description.ifEmpty { stringResource(R.string.group_op_fallback) },
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = Icons.Outlined.CloudOff,
                    contentDescription = null,
                    tint = badgeColor,
                    modifier = Modifier.size(12.dp),
                )
                Text(
                    text = badgeText,
                    fontSize = 12.sp,
                    color = badgeColor,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        MoneyText(
            entry.payload.sum,
            role = MoneyRole.NEUTRAL,
            size = 15.sp,
            weight = FontWeight.Normal,
            currency = currency,
        )
    }
}

// MARK: - Нижний бар тусы

internal const val TUSA_TAB_OPS = "ops"
internal const val TUSA_TAB_BALANCES = "balances"
internal const val TUSA_TAB_TOTALS = "totals"
internal const val TUSA_TAB_SETTINGS = "settings"

/**
 * Контекстный таб-бар тусы: [Операции][Балансы] (+) [Итоги][Настройки].
 * Нативный Material NavigationBar (та же стилистика, что главный бар
 * MainScaffold), центральная позиция — приподнятая кнопка «+».
 */
@Composable
private fun TusaBar(
    selected: String,
    enabled: Boolean,
    onSelect: (String) -> Unit,
    onAdd: () -> Unit,
) {
    val colors = Splitty.colors
    // Кнопку «+» рисуем ПОВЕРХ бара в Box (Box не клипует), а не как icon
    // у NavigationBarItem: NavigationBar клипует контент по своей высоте, из-за
    // чего приподнятая на -12dp кнопка обрезалась сверху, а переразмеренный
    // item тянул высоту бара вверх. Тот же приём, что в MainScaffold.
    Box {
        // windowInsets = 0: инсет нижней системной навигации уже применён
        // родительским MainScaffold (см. contentWindowInsets выше) — иначе бар
        // приподнимался над низом экрана и не совпадал с баром вкладок.
        NavigationBar(
            containerColor = colors.surface,
            windowInsets = WindowInsets(0, 0, 0, 0),
        ) {
            TusaTabItem(
                title = stringResource(R.string.group_tab_operations),
                icon = Icons.AutoMirrored.Outlined.ReceiptLong,
                isSelected = selected == TUSA_TAB_OPS,
            ) { onSelect(TUSA_TAB_OPS) }
            TusaTabItem(
                title = stringResource(R.string.group_balances_title),
                icon = Icons.Outlined.SwapHoriz,
                isSelected = selected == TUSA_TAB_BALANCES,
            ) { onSelect(TUSA_TAB_BALANCES) }
            // Плейсхолдер центральной позиции: держит раскладку табов
            // (сама кнопка — оверлеем ниже).
            NavigationBarItem(
                selected = false,
                onClick = { if (enabled) onAdd() },
                icon = { Spacer(Modifier.size(58.dp)) },
                colors = NavigationBarItemDefaults.colors(
                    indicatorColor = Color.Transparent,
                ),
            )
            TusaTabItem(
                title = stringResource(R.string.totals_title),
                icon = Icons.Outlined.PieChart,
                isSelected = selected == TUSA_TAB_TOTALS,
            ) { onSelect(TUSA_TAB_TOTALS) }
            TusaTabItem(
                title = stringResource(R.string.group_settings_short),
                icon = Icons.Outlined.Settings,
                isSelected = selected == TUSA_TAB_SETTINGS,
            ) { onSelect(TUSA_TAB_SETTINGS) }
        }
        TusaAddFab(
            onClick = { if (enabled) onAdd() },
            modifier = Modifier.align(Alignment.TopCenter),
        )
    }
}

@Composable
private fun RowScope.TusaTabItem(
    title: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    isSelected: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    NavigationBarItem(
        selected = isSelected,
        onClick = onClick,
        icon = { Icon(icon, contentDescription = null) },
        label = { Text(text = title, fontSize = 11.sp, maxLines = 1) },
        colors = NavigationBarItemDefaults.colors(
            selectedIconColor = colors.accent,
            selectedTextColor = colors.accent,
            unselectedIconColor = colors.inkSecondary,
            unselectedTextColor = colors.inkSecondary,
            indicatorColor = colors.accent.copy(alpha = 0.14f),
        ),
    )
}

/** Приподнятая кнопка «+» бара тусы (копия AddExpenseFab из MainScaffold). */
@Composable
private fun TusaAddFab(onClick: () -> Unit, modifier: Modifier = Modifier) {
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
            contentDescription = stringResource(R.string.group_add_expense),
            tint = Color.White,
        )
    }
}

/** Сегмент фильтра операций: «Все | Со мной». */
@Composable
private fun MineSegment(isMineOnly: Boolean, onChange: (Boolean) -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(colors.surface)
            .padding(3.dp),
    ) {
        SegmentButton(stringResource(R.string.group_filter_all), !isMineOnly) { onChange(false) }
        SegmentButton(stringResource(R.string.group_chip_mine_only), isMineOnly) { onChange(true) }
    }
}

@Composable
private fun SegmentButton(title: String, isOn: Boolean, onClick: () -> Unit) {
    val colors = Splitty.colors
    val haptics = rememberHaptics()
    Text(
        text = title,
        fontSize = 13.sp,
        fontWeight = FontWeight.SemiBold,
        color = if (isOn) Color.White else colors.inkSecondary,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (isOn) colors.accent else Color.Transparent)
            .clickable {
                // Отклик только на реальное переключение, не на повторный тап
                // по уже выбранному сегменту (порт iOS mineSegment).
                if (!isOn) haptics.tap()
                onClick()
            }
            // isSelected для TalkBack: сегмент читается как выбранный/невыбранный.
            .semantics { selected = isOn }
            .padding(horizontal = 16.dp, vertical = 6.dp),
    )
}

/** Вкладка «Балансы»: карточный список долгов (бывший bottom sheet). */
@Composable
private fun GroupBalancesTab(
    room: RoomDetail,
    meId: Long,
    onSettle: (Debt) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    if (room.debtsUnavailable) {
        // Долги неисчислимы — не рисуем ложное «все в расчёте».
        Column(
            modifier = modifier
                .fillMaxSize()
                .padding(horizontal = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Text(
                text = stringResource(R.string.group_debts_unavailable_title),
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.group_debts_unavailable_subtitle),
                fontSize = 14.sp,
                color = colors.inkSecondary,
                textAlign = TextAlign.Center,
            )
        }
    } else if (room.debts.isEmpty()) {
        Column(
            modifier = modifier.fillMaxSize(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Icon(
                imageVector = Icons.Outlined.CheckCircle,
                contentDescription = null,
                tint = colors.accent,
                modifier = Modifier.size(36.dp),
            )
            Spacer(Modifier.height(10.dp))
            Text(
                text = stringResource(R.string.group_no_debts),
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.group_members_settled),
                fontSize = 14.sp,
                color = colors.inkSecondary,
            )
        }
    } else {
        Column(
            modifier = modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
        ) {
            SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                room.debts.forEachIndexed { index, debt ->
                    DebtRow(
                        debt = debt,
                        meId = meId,
                        currency = room.currency,
                        // Погашение с предвыбором именно этого долга.
                        onSettle = { onSettle(debt) },
                    )
                    if (index < room.debts.lastIndex) {
                        HairlineDivider()
                    }
                }
            }
        }
    }
}

/** Секция месяца: тихий заголовок + карточка со строками операций. */
@Composable
private fun MonthSectionCard(
    section: MonthSection,
    meId: Long,
    currency: String,
    onOpenOperation: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(section.title, modifier = Modifier.padding(start = 4.dp))
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            section.operations.forEachIndexed { index, operation ->
                OperationRow(
                    operation = operation,
                    meId = meId,
                    currency = currency,
                    onClick = { onOpenOperation(operation.id) },
                )
                if (index < section.operations.lastIndex) {
                    HairlineDivider(startIndent = 62.dp)
                }
            }
        }
    }
}

/**
 * Строка операции: колонка даты, иконка, описание и «кто заплатил», моя
 * позиция справа. Позиция — по ХРАНИМЫМ долям recipients[].sum
 * (Operation.netPosition), не пересчёт.
 */
@Composable
private fun OperationRow(
    operation: Operation,
    meId: Long,
    currency: String,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    val net = operation.netPosition(meId)

    val title = if (operation.isDebtRepayment) {
        val recipientName = operation.recipients.firstOrNull()?.user?.displayName.orEmpty()
        when {
            operation.donor.id == meId ->
                stringResource(R.string.group_repay_you_paid, recipientName)

            operation.recipients.firstOrNull()?.user?.id == meId ->
                stringResource(R.string.group_repay_paid_you, operation.donor.displayName)

            else ->
                stringResource(R.string.group_repay_paid, operation.donor.displayName, recipientName)
        }
    } else {
        operation.description.ifEmpty { stringResource(R.string.group_op_fallback) }
    }

    val subtitle = if (operation.isDebtRepayment) {
        null
    } else if (operation.donor.id == meId) {
        stringResource(R.string.group_op_you_paid_sum, money(operation.sum, currency))
    } else {
        stringResource(
            R.string.group_op_paid_sum,
            operation.donor.displayName,
            money(operation.sum, currency),
        )
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Колонка даты: «июл» сверху, «5» снизу, вторичным цветом.
        Column(
            modifier = Modifier.width(34.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = GroupsDateFmt.monthShort(operation.createdAt),
                fontSize = 11.sp,
                color = colors.inkSecondary,
            )
            Text(
                text = GroupsDateFmt.day(operation.createdAt),
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.inkSecondary,
            )
        }
        OperationIconBox(isRepayment = operation.isDebtRepayment)
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = title,
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false),
                )
                if (operation.hasFiles) {
                    Icon(
                        imageVector = Icons.Outlined.Attachment,
                        contentDescription = null,
                        tint = colors.accent,
                        modifier = Modifier.size(13.dp),
                    )
                }
            }
            if (subtitle != null) {
                Text(
                    text = subtitle,
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        when {
            operation.isDebtRepayment -> MoneyText(
                operation.sum,
                role = MoneyRole.NEUTRAL,
                size = 15.sp,
                weight = FontWeight.Normal,
                currency = currency,
            )

            net != null && net != 0L -> Column(
                horizontalAlignment = Alignment.End,
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(
                    text = stringResource(
                        if (net > 0) R.string.group_op_lent else R.string.group_op_owe
                    ),
                    fontSize = 11.sp,
                    color = colors.inkSecondary,
                )
                MoneyText(net, size = 15.sp, currency = currency)
            }

            net != null -> Text(
                text = stringResource(R.string.group_op_settled),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )

            else -> Text(
                text = stringResource(R.string.group_op_not_involved),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
    }
}

/** Квадрат-иконка операции: банкнота для погашений, документ для расходов. */
@Composable
private fun OperationIconBox(isRepayment: Boolean, size: androidx.compose.ui.unit.Dp = 36.dp) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .size(size)
            .clip(RoundedCornerShape(10.dp))
            .background(
                if (isRepayment) colors.accent.copy(alpha = 0.14f)
                else colors.ink.copy(alpha = 0.06f)
            ),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = if (isRepayment) Icons.Outlined.Payments else Icons.Outlined.Description,
            contentDescription = null,
            tint = if (isRepayment) colors.accent else colors.inkSecondary,
            modifier = Modifier.size(18.dp),
        )
    }
}

// MARK: - Строка долга

@Composable
private fun DebtRow(
    debt: Debt,
    meId: Long,
    currency: String,
    onSettle: () -> Unit,
) {
    val colors = Splitty.colors
    val involvesMe = debt.debtor.id == meId || debt.lender.id == meId
    val title = when {
        debt.debtor.id == meId -> stringResource(R.string.group_debt_you_owe, debt.lender.displayName)
        debt.lender.id == meId -> stringResource(R.string.group_debt_owes_you, debt.debtor.displayName)
        else -> stringResource(R.string.group_debt_pair, debt.debtor.displayName, debt.lender.displayName)
    }
    // Цвет суммы: мой долг — negative, долг мне — accent, чужие — нейтрально.
    val role = when {
        debt.debtor.id == meId -> MoneyRole.NEGATIVE
        debt.lender.id == meId -> MoneyRole.POSITIVE
        else -> MoneyRole.NEUTRAL
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Аватары+стрелка+подпись+сумма — один элемент для TalkBack (читается
        // «Петя должен(на) Вася, 500 ₽»); «Погасить» остаётся отдельной кнопкой.
        Row(
            modifier = Modifier
                .weight(1f)
                .semantics(mergeDescendants = true) {},
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            GradientAvatar(user = debt.debtor, size = 36.dp)
            Icon(
                imageVector = Icons.AutoMirrored.Filled.ArrowForward,
                contentDescription = null, // декоративная стрелка
                tint = colors.inkSecondary,
                modifier = Modifier.size(14.dp),
            )
            GradientAvatar(user = debt.lender, size = 36.dp)
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(
                    text = title,
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.ink,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                MoneyText(debt.sum, role = role, size = 15.sp, currency = currency)
            }
        }
        if (involvesMe) {
            SoftChip(text = stringResource(R.string.group_debt_settle), onClick = onSettle)
        }
    }
}

// MARK: - Настройки (bottom sheet)

/**
 * Настройки группы: участники, валюта (GET /currencies + PUT currency),
 * приглашение (share + код), архив/разархив. Вкладка бара тусы
 * (полноэкранная, бывший bottom sheet). Порт iOS GroupSettingsView.
 */
@Composable
private fun GroupSettingsTab(
    room: RoomDetail,
    meId: Long?,
    viewModel: GroupDetailViewModel,
    onInvite: () -> Unit,
    onInviteFriends: () -> Unit,
    onLeft: () -> Unit,
    modifier: Modifier = Modifier,
) {
    LaunchedEffect(Unit) { viewModel.loadCurrencies() }

    // Кого убираем / подтверждение выхода.
    var memberToRemove by remember { mutableStateOf<User?>(null) }
    var isLeaveConfirmVisible by remember { mutableStateOf(false) }

    memberToRemove?.let { member ->
        AlertDialog(
            onDismissRequest = { memberToRemove = null },
            title = { Text(stringResource(R.string.group_remove_member_title, member.displayName)) },
            text = { Text(stringResource(R.string.group_remove_member_message)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.removeMember(member.id)
                    memberToRemove = null
                }) { Text(stringResource(R.string.group_remove_member_confirm)) }
            },
            dismissButton = {
                TextButton(onClick = { memberToRemove = null }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }
    if (isLeaveConfirmVisible) {
        AlertDialog(
            onDismissRequest = { isLeaveConfirmVisible = false },
            title = { Text(stringResource(R.string.group_leave_title, room.name)) },
            text = { Text(stringResource(R.string.group_leave_message)) },
            confirmButton = {
                TextButton(onClick = {
                    isLeaveConfirmVisible = false
                    viewModel.leaveRoom(onLeft)
                }) { Text(stringResource(R.string.group_leave_confirm)) }
            },
            dismissButton = {
                TextButton(onClick = { isLeaveConfirmVisible = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    val currencies by viewModel.currencies.collectAsStateWithLifecycle()
    val savingCurrency by viewModel.savingCurrency.collectAsStateWithLifecycle()
    val selectedOverride by viewModel.selectedCurrencyOverride.collectAsStateWithLifecycle()
    val isArchiving by viewModel.isArchiving.collectAsStateWithLifecycle()

    val colors = Splitty.colors
    val haptics = rememberHaptics()
    val selectedCurrency = selectedOverride ?: room.currency
    // Смена валюты — с подтверждением: суммы НЕ пересчитываются, меняется
    // только обозначение у всех участников (порт iOS confirmationDialog).
    var pendingCurrency by remember { mutableStateOf<CurrencyInfo?>(null) }

    pendingCurrency?.let { currency ->
        AlertDialog(
            onDismissRequest = { pendingCurrency = null },
            title = { Text(stringResource(R.string.group_currency_change_title, currency.code)) },
            text = { Text(stringResource(R.string.group_currency_change_message)) },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    viewModel.setCurrency(currency.code)
                    pendingCurrency = null
                }) {
                    Text(stringResource(R.string.group_currency_change_confirm, currency.code))
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingCurrency = null }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp)
            .padding(top = 4.dp, bottom = 16.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {

            // Участники
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    SectionHeader(stringResource(R.string.group_settings_members))
                    Spacer(Modifier.weight(1f))
                    Row(
                        modifier = Modifier
                            .clip(RoundedCornerShape(50))
                            .clickable(onClick = onInviteFriends)
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                        horizontalArrangement = Arrangement.spacedBy(5.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(
                            imageVector = Icons.Outlined.PersonAddAlt,
                            contentDescription = null,
                            tint = colors.accent,
                            modifier = Modifier.size(18.dp),
                        )
                        Text(
                            text = stringResource(R.string.group_settings_members_invite),
                            fontSize = 14.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = colors.accentText,
                        )
                    }
                }
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    room.members.forEachIndexed { index, member ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 16.dp, vertical = 10.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            GradientAvatar(user = member, size = 36.dp)
                            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                Text(
                                    text = if (member.id == meId) {
                                        stringResource(R.string.group_settings_member_you, member.displayName)
                                    } else {
                                        member.displayName
                                    },
                                    fontSize = 15.sp,
                                    fontWeight = FontWeight.Medium,
                                    color = colors.ink,
                                )
                                member.username?.takeIf { it.isNotEmpty() }?.let { username ->
                                    Text(
                                        text = "@$username",
                                        fontSize = 12.sp,
                                        color = colors.inkSecondary,
                                    )
                                }
                            }
                            // Лекарство от «позвал не того»: убрать участника
                            // может любой в комнате, как и править расходы.
                            if (member.id != meId) {
                                Spacer(Modifier.weight(1f))
                                TextButton(onClick = { memberToRemove = member }) {
                                    Text(
                                        text = stringResource(R.string.group_remove_member_confirm),
                                        fontSize = 13.sp,
                                        color = colors.negativeText,
                                    )
                                }
                            }
                        }
                        if (index < room.members.lastIndex) {
                            HairlineDivider(startIndent = 64.dp)
                        }
                    }
                }
            }

            // Валюта
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                SectionHeader(
                    stringResource(R.string.group_settings_currency),
                    modifier = Modifier.padding(start = 4.dp),
                )
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    when (val list = currencies) {
                        UiState.Loading -> Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 20.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            CircularProgressIndicator(
                                color = colors.accent,
                                modifier = Modifier.size(24.dp),
                                strokeWidth = 2.dp,
                            )
                        }

                        is UiState.Error -> Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(16.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            Text(
                                text = list.message,
                                fontSize = 13.sp,
                                color = colors.inkSecondary,
                            )
                            SoftChip(
                                text = stringResource(R.string.common_retry),
                                onClick = viewModel::loadCurrencies,
                            )
                        }

                        is UiState.Content -> list.value.forEachIndexed { index, currency ->
                            CurrencyRow(
                                currency = currency,
                                isSelected = selectedCurrency == currency.code,
                                isSaving = savingCurrency == currency.code,
                                enabled = savingCurrency == null,
                                // Подтверждаем только реальную смену (не текущую валюту).
                                onClick = {
                                    if (currency.code != selectedCurrency) pendingCurrency = currency
                                },
                            )
                            if (index < list.value.lastIndex) {
                                HairlineDivider(startIndent = 52.dp)
                            }
                        }
                    }
                }
                Text(
                    text = stringResource(R.string.group_settings_currency_footer),
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                    modifier = Modifier.padding(horizontal = 4.dp),
                )
            }

            // Архив
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable(enabled = !isArchiving) {
                                // Вкладка (не шторка): после архива экран
                                // обновится сам через dataVersion — закрывать нечего.
                                viewModel.toggleArchive(onDone = {})
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        // «Архивировать» — нейтральный ink, не «опасный» красный:
                        // действие обратимо и скрывает группу только у самого юзера.
                        val actionColor = if (room.isArchived) colors.accent else colors.ink
                        Icon(
                            imageVector = if (room.isArchived) Icons.Outlined.Unarchive else Icons.Outlined.Archive,
                            contentDescription = null,
                            tint = actionColor,
                            modifier = Modifier.size(18.dp),
                        )
                        Text(
                            text = stringResource(
                                if (room.isArchived) R.string.group_settings_unarchive
                                else R.string.group_settings_archive
                            ),
                            fontSize = 15.sp,
                            fontWeight = FontWeight.Medium,
                            color = actionColor,
                        )
                        Spacer(Modifier.weight(1f))
                        if (isArchiving) {
                            CircularProgressIndicator(
                                color = colors.inkSecondary,
                                modifier = Modifier.size(18.dp),
                                strokeWidth = 2.dp,
                            )
                        }
                    }
                }
                Text(
                    text = stringResource(R.string.group_settings_archive_footer),
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                    modifier = Modifier.padding(horizontal = 4.dp),
                )
            }

            // Выход из группы
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                // Кнопка гаснет ЗАРАНЕЕ: расходы видно в самой комнате, и ждать
                // отказа сервера, чтобы сообщить об этом, незачем
                val blocking = meId?.let { room.operationsBlockingLeave(it) }.orEmpty()
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable(enabled = blocking.isEmpty()) { isLeaveConfirmVisible = true }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowForward,
                            contentDescription = null,
                            tint = colors.negative,
                            modifier = Modifier.size(18.dp),
                        )
                        Text(
                            text = stringResource(R.string.group_leave_action),
                            fontSize = 15.sp,
                            fontWeight = FontWeight.Medium,
                            color = colors.negativeText,
                        )
                    }
                }
                Text(
                    text = if (blocking.isEmpty()) {
                        stringResource(R.string.group_leave_message)
                    } else {
                        stringResource(R.string.group_leave_blocked, blocking.size)
                    },
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                    modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
        }
}

/**
 * Кого можно позвать в комнату из списка друзей.
 *
 * Участников комнаты не показываем — приглашение им ничего бы не сделало.
 * Удалённые аккаунты — тоже: человека за ними нет, приглашение вернётся 404,
 * а в списке друзей они остаются, потому что общие расходы никуда не делись.
 */
internal fun inviteCandidates(
    friends: List<FriendBalance>,
    memberIds: Set<Long>,
): List<FriendBalance> = friends.filterNot { it.user.id in memberIds || it.user.deleted }

/**
 * Шит выбора друзей для приглашения.
 *
 * Друг — человек, с которым уже была общая группа, значит его id известен и
 * вводить код никому не нужно.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun InviteFriendsSheet(
    room: RoomDetail,
    viewModel: GroupDetailViewModel,
    onLink: () -> Unit,
    onDismiss: () -> Unit,
) {
    LaunchedEffect(Unit) { viewModel.loadFriends() }

    val colors = Splitty.colors
    val friends by viewModel.friends.collectAsStateWithLifecycle()
    val memberIds = remember(room) { room.members.map { it.id }.toSet() }
    val candidates = inviteCandidates(friends, memberIds)
    var selected by remember { mutableStateOf(emptySet<Long>()) }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .padding(bottom = 36.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text(
                text = stringResource(R.string.invite_friends_title),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )

            if (candidates.isEmpty()) {
                Text(
                    text = stringResource(R.string.invite_friends_empty),
                    fontSize = 14.sp,
                    color = colors.inkSecondary,
                )
            } else {
                SectionHeader(stringResource(R.string.invite_friends_section))
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    candidates.forEachIndexed { index, friend ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    selected = if (selected.contains(friend.user.id)) {
                                        selected - friend.user.id
                                    } else {
                                        selected + friend.user.id
                                    }
                                }
                                .padding(horizontal = 16.dp, vertical = 10.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            GradientAvatar(user = friend.user, size = 36.dp)
                            Text(
                                text = friend.user.displayName,
                                fontSize = 15.sp,
                                fontWeight = FontWeight.Medium,
                                color = colors.ink,
                            )
                            Spacer(Modifier.weight(1f))
                            if (selected.contains(friend.user.id)) {
                                Icon(
                                    imageVector = Icons.Filled.Check,
                                    contentDescription = null,
                                    tint = colors.accent,
                                    modifier = Modifier.size(20.dp),
                                )
                            }
                        }
                        if (index < candidates.lastIndex) {
                            HairlineDivider(startIndent = 64.dp)
                        }
                    }
                }
                PrimaryPillButton(
                    text = stringResource(R.string.invite_friends_send),
                    onClick = { viewModel.inviteFriends(selected) { onDismiss() } },
                    enabled = selected.isNotEmpty(),
                )
            }

            SoftChip(
                text = stringResource(R.string.invite_friends_link),
                onClick = onLink,
            )
            Text(
                text = stringResource(R.string.invite_friends_link_footer),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
    }
}

/** Строка пикера валют: флаг, код, символ; чекмарк у текущей, спиннер у PUT. */
@Composable
private fun CurrencyRow(
    currency: CurrencyInfo,
    isSelected: Boolean,
    isSaving: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = currency.flag, fontSize = 22.sp)
        Text(
            text = currency.code,
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = colors.ink,
        )
        Text(
            text = currency.symbol,
            fontSize = 15.sp,
            color = colors.inkSecondary,
        )
        Spacer(Modifier.weight(1f))
        if (isSaving) {
            CircularProgressIndicator(
                color = colors.accent,
                modifier = Modifier.size(18.dp),
                strokeWidth = 2.dp,
            )
        } else if (isSelected) {
            Icon(
                imageVector = Icons.Filled.Check,
                contentDescription = null,
                tint = colors.accent,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}
