package com.zagir.splitty.ui.groups

import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.Attachment
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Payments
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Unarchive
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
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
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.money.money
import com.zagir.splitty.data.OutboxEntry
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
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

    var isBalancesPresented by rememberSaveable { mutableStateOf(false) }
    var isTotalsPresented by rememberSaveable { mutableStateOf(false) }
    var isSettingsPresented by rememberSaveable { mutableStateOf(false) }

    val colors = Splitty.colors
    val detail = (state as? UiState.Content)?.value
    val meId = meIdState

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
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
                actions = {
                    IconButton(onClick = { isSettingsPresented = true }, enabled = detail != null) {
                        Icon(
                            imageVector = Icons.Outlined.Settings,
                            contentDescription = stringResource(R.string.group_settings),
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
        floatingActionButton = {
            if (detail != null) {
                ExtendedFloatingActionButton(
                    onClick = { onAddExpense(roomId) },
                    containerColor = colors.accent,
                    contentColor = Color.White,
                    shape = CircleShape,
                    icon = {
                        Icon(
                            imageVector = Icons.Filled.Add,
                            contentDescription = stringResource(R.string.group_add_expense),
                        )
                    },
                    text = {
                        Text(
                            text = stringResource(R.string.group_fab_expense),
                            fontSize = 16.sp,
                            fontWeight = FontWeight.SemiBold,
                        )
                    },
                )
            }
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
                GroupDetailContent(
                    room = current.value,
                    sections = sections,
                    localOperations = localOperations,
                    meId = meId,
                    isRefreshing = isRefreshing,
                    onRefresh = viewModel::refresh,
                    onSettleUp = settleUp,
                    onShowBalances = { isBalancesPresented = true },
                    onShowTotals = { isTotalsPresented = true },
                    onOpenOperation = { operationId -> onOpenOperation(roomId, operationId) },
                    onEditLocalOperation = { localId -> onEditLocalOperation(roomId, localId) },
                    modifier = Modifier.padding(innerPadding),
                )
            }
        }
    }

    if (isBalancesPresented && detail != null && meId != null) {
        GroupBalancesSheet(
            room = detail,
            meId = meId,
            onDismiss = { isBalancesPresented = false },
            onSettle = {
                isBalancesPresented = false
                settleUp()
            },
        )
    }
    if (isTotalsPresented) {
        GroupDashboardSheet(roomId = roomId, onDismiss = { isTotalsPresented = false })
    }
    if (isSettingsPresented && detail != null) {
        GroupSettingsSheet(
            room = detail,
            meId = meId,
            viewModel = viewModel,
            onDismiss = { isSettingsPresented = false },
        )
    }
    GroupsAlertDialog(alertMessage, viewModel::dismissAlert)
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
    onShowBalances: () -> Unit,
    onShowTotals: () -> Unit,
    onOpenOperation: (String) -> Unit,
    onEditLocalOperation: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = modifier.fillMaxSize(),
    ) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            // Запас снизу — под FAB «+ Расход».
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 96.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            item(key = "hero") {
                DebtHeroCard(room = room, meId = meId, pendingCount = localOperations.size)
            }
            item(key = "chips") {
                ActionChips(
                    room = room,
                    meId = meId,
                    onSettleUp = onSettleUp,
                    onShowBalances = onShowBalances,
                    onShowTotals = onShowTotals,
                )
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
                    )
                }
            } else {
                sections.forEach { section ->
                    item(key = "month-${section.month}") {
                        MonthSectionCard(
                            section = section,
                            meId = meId,
                            currency = room.currency,
                            onOpenOperation = onOpenOperation,
                        )
                    }
                }
            }
        }
    }
}

/** Hero-карточка статуса долга (+ бейдж архива, + подпись про outbox). */
@Composable
private fun DebtHeroCard(room: RoomDetail, meId: Long, pendingCount: Int = 0) {
    val colors = Splitty.colors
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

/** Ряд чипов: «Погасить долг» (акцентный), «Балансы», «Итоги». */
@Composable
private fun ActionChips(
    room: RoomDetail,
    meId: Long,
    onSettleUp: () -> Unit,
    onShowBalances: () -> Unit,
    onShowTotals: () -> Unit,
) {
    val canSettle = room.debts.any { it.debtor.id == meId || it.lender.id == meId }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(modifier = Modifier.alpha(if (canSettle) 1f else 0.45f)) {
            SoftChip(
                text = stringResource(R.string.group_chip_settle),
                onClick = { if (canSettle) onSettleUp() },
                isSelected = true,
            )
        }
        SoftChip(text = stringResource(R.string.group_chip_balances), onClick = onShowBalances)
        SoftChip(text = stringResource(R.string.group_chip_totals), onClick = onShowTotals)
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

            net != null && net != 0 -> Column(
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

// MARK: - Балансы (bottom sheet)

/**
 * Балансы группы: карточный список долгов «кто → кому»; у долгов с участием
 * текущего пользователя — чип «Погасить». Порт iOS GroupBalancesView.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GroupBalancesSheet(
    room: RoomDetail,
    meId: Long,
    onDismiss: () -> Unit,
    onSettle: () -> Unit,
) {
    val colors = Splitty.colors
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
        ) {
            Text(
                text = stringResource(R.string.group_balances_title),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
            Spacer(Modifier.height(16.dp))
            if (room.debts.isEmpty()) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 24.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
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
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    room.debts.forEachIndexed { index, debt ->
                        DebtRow(
                            debt = debt,
                            meId = meId,
                            currency = room.currency,
                            onSettle = onSettle,
                        )
                        if (index < room.debts.lastIndex) {
                            HairlineDivider()
                        }
                    }
                }
            }
        }
    }
}

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
        GradientAvatar(user = debt.debtor, size = 36.dp)
        Icon(
            imageVector = Icons.AutoMirrored.Filled.ArrowForward,
            contentDescription = null,
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
        if (involvesMe) {
            SoftChip(text = stringResource(R.string.group_debt_settle), onClick = onSettle)
        }
    }
}

// MARK: - Настройки (bottom sheet)

/**
 * Настройки группы: участники, валюта (GET /currencies + PUT currency),
 * приглашение (share + код), архив/разархив. Порт iOS GroupSettingsView.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GroupSettingsSheet(
    room: RoomDetail,
    meId: Long?,
    viewModel: GroupDetailViewModel,
    onDismiss: () -> Unit,
) {
    LaunchedEffect(Unit) { viewModel.loadCurrencies() }

    val currencies by viewModel.currencies.collectAsStateWithLifecycle()
    val savingCurrency by viewModel.savingCurrency.collectAsStateWithLifecycle()
    val selectedOverride by viewModel.selectedCurrencyOverride.collectAsStateWithLifecycle()
    val isArchiving by viewModel.isArchiving.collectAsStateWithLifecycle()

    val colors = Splitty.colors
    val context = LocalContext.current
    // Ссылка-приглашение, совместимая с deep-link бота.
    val inviteLink = "https://t.me/split_money_bot?start=room${room.id}"
    val inviteMessage = stringResource(R.string.group_invite_message, room.name, inviteLink, room.id)
    val selectedCurrency = selectedOverride ?: room.currency

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.bg,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp)
                .padding(bottom = 36.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            Text(
                text = stringResource(R.string.group_settings),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )

            // Участники
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                SectionHeader(
                    stringResource(R.string.group_settings_members),
                    modifier = Modifier.padding(start = 4.dp),
                )
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
                                onClick = { viewModel.setCurrency(currency.code) },
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

            // Приглашение
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                val send = Intent(Intent.ACTION_SEND).apply {
                                    type = "text/plain"
                                    putExtra(Intent.EXTRA_TEXT, inviteMessage)
                                }
                                context.startActivity(Intent.createChooser(send, null))
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(
                            imageVector = Icons.Outlined.Share,
                            contentDescription = null,
                            tint = colors.accent,
                            modifier = Modifier.size(18.dp),
                        )
                        Text(
                            text = stringResource(R.string.group_settings_invite),
                            fontSize = 15.sp,
                            fontWeight = FontWeight.Medium,
                            color = colors.accent,
                        )
                    }
                    HairlineDivider(startIndent = 16.dp)
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = stringResource(R.string.group_settings_code),
                            fontSize = 15.sp,
                            color = colors.ink,
                        )
                        Spacer(Modifier.weight(1f))
                        Text(
                            text = room.id,
                            fontSize = 12.sp,
                            fontFamily = FontFamily.Monospace,
                            color = colors.inkSecondary,
                        )
                    }
                }
                Text(
                    text = stringResource(R.string.group_settings_invite_footer),
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
                                viewModel.toggleArchive(onDone = onDismiss)
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        val actionColor = if (room.isArchived) colors.accent else colors.negative
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
