package com.zagir.splitty.ui.groups

import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.heading
import com.zagir.splitty.ui.components.BalanceSummaryLines
import com.zagir.splitty.ui.components.TextLinkButton
import com.zagir.splitty.ui.components.ReceiptDivider
import com.zagir.splitty.ui.components.ReceiptStub
import com.zagir.splitty.ui.components.ReceiptLabel
import com.zagir.splitty.core.ui.resolve
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.ui.platform.testTag
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.DataFreshness
import com.zagir.splitty.core.model.CurrencySum
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.money.aggregateByCurrency
import com.zagir.splitty.ui.components.CacheNote
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.main.badgeLabel
import com.zagir.splitty.ui.theme.Splitty
import androidx.compose.material.icons.outlined.UnfoldMore
import com.zagir.splitty.core.model.CurrencyInfo
import com.zagir.splitty.core.money.suggestedRoomCurrency
import java.util.Currency
import java.util.Locale

/**
 * Вкладка «Группы»: hero-карточка общего баланса, карточки групп, тихая
 * строка «Архив», создание группы и присоединение по коду.
 * Порт iOS GroupsListView.
 *
 * [onOpenRoom] — открыть экран группы по id комнаты.
 */
@Composable
fun GroupsListScreen(
    onOpenRoom: (String) -> Unit,
    viewModel: GroupsListViewModel = hiltViewModel(),
) {
    LaunchedEffect(Unit) { viewModel.trackScreen() }
    var isArchivePresented by rememberSaveable { mutableStateOf(false) }
    if (isArchivePresented) {
        ArchivedGroupsScreen(
            onBack = { isArchivePresented = false },
            onOpenRoom = onOpenRoom,
            viewModel = viewModel,
        )
    } else {
        GroupsListContent(
            onOpenRoom = onOpenRoom,
            onOpenArchive = { isArchivePresented = true },
            viewModel = viewModel,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GroupsListContent(
    onOpenRoom: (String) -> Unit,
    onOpenArchive: () -> Unit,
    viewModel: GroupsListViewModel,
) {
    val state by viewModel.rooms.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val alertMessage by viewModel.alertMessage.collectAsStateWithLifecycle()
    val pendingRoomIds by viewModel.pendingRoomIds.collectAsStateWithLifecycle()
    val freshness by viewModel.freshness.collectAsStateWithLifecycle()
    val archivedState by viewModel.archived.collectAsStateWithLifecycle()
    val hasArchived = (archivedState as? UiState.Content)?.value?.isNotEmpty() == true
    var isCreatePresented by rememberSaveable { mutableStateOf(false) }
    var isJoinPresented by rememberSaveable { mutableStateOf(false) }
    val colors = Splitty.colors
    Scaffold(
        containerColor = colors.bg,
        topBar = {
            // Простой заголовок, как на «Друзьях»: большой сворачивающийся
            // заголовок Material давал над «Группами» пустоту, которой нет ни
            // на соседних вкладках, ни на iOS.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 16.dp, top = 16.dp, end = 8.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.groups_title),
                    modifier = Modifier.weight(1f).semantics { heading() },
                    fontSize = 32.sp,
                    fontWeight = FontWeight.Bold,
                    color = colors.ink,
                )
                GroupsAddMenu(
                    onCreate = { isCreatePresented = true },
                    onJoinByCode = { isJoinPresented = true },
                )
            }
        },
    ) { innerPadding ->
        when (val current = state) {
            UiState.Loading -> GroupsLoading(Modifier.padding(innerPadding))

            is UiState.Error -> GroupsFullScreenError(
                message = current.message.resolve(),
                onRetry = viewModel::retry,
                modifier = Modifier.padding(innerPadding),
            )

            is UiState.Content -> RoomsList(
                rooms = current.value,
                freshness = freshness,
                hasArchived = hasArchived,
                pendingRoomIds = pendingRoomIds,
                isRefreshing = isRefreshing,
                onRefresh = viewModel::refresh,
                onOpenRoom = onOpenRoom,
                onArchive = viewModel::archive,
                onOpenArchive = onOpenArchive,
                onCreate = { isCreatePresented = true },
                onJoin = { isJoinPresented = true },
                modifier = Modifier.padding(innerPadding),
            )
        }
    }

    if (isCreatePresented) {
        CreateGroupSheet(viewModel = viewModel, onDismiss = { isCreatePresented = false })
    }
    if (isJoinPresented) {
        JoinGroupSheet(viewModel = viewModel, onDismiss = { isJoinPresented = false })
    }
    GroupsAlertDialog(alertMessage, viewModel::dismissAlert)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RoomsList(
    rooms: List<RoomSummary>,
    freshness: DataFreshness,
    hasArchived: Boolean,
    pendingRoomIds: Set<String>,
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    onOpenRoom: (String) -> Unit,
    onArchive: (String) -> Unit,
    onOpenArchive: () -> Unit,
    onCreate: () -> Unit,
    onJoin: () -> Unit,
    modifier: Modifier = Modifier,
) {
    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = modifier.fillMaxSize(),
    ) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 20.dp, end = 20.dp, top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            if (rooms.isEmpty()) {
                item(key = "empty") {
                    GroupsEmptyCard(
                        icon = Icons.Outlined.Groups,
                        title = stringResource(R.string.groups_empty_title),
                        subtitle = stringResource(R.string.groups_empty_subtitle),
                    ) {
                        PrimaryPillButton(
                            text = stringResource(R.string.groups_create_group),
                            onClick = onCreate,
                        )
                        Spacer(Modifier.height(4.dp))
                        TextLinkButton(
                            text = stringResource(R.string.groups_join_by_code),
                            onClick = onJoin,
                        )
                    }
                }
                // В пустом состоянии «Архив» отвлекает от первого шага. Но если
                // архив НЕ пуст, строка обязана остаться: заархивировав
                // последнюю группу, человек терял единственный вход в архив и
                // не мог достать её обратно
                if (hasArchived) {
                    item(key = "archive") { ArchiveRow(onOpenArchive) }
                }
            } else {
                item(key = "summary") { SummaryCard(rooms, freshness) }
                items(rooms, key = { it.id }) { room ->
                    GroupCard(
                        room = room,
                        hasPending = room.id in pendingRoomIds,
                        onClick = { onOpenRoom(room.id) },
                        onArchive = { onArchive(room.id) },
                    )
                }
                item(key = "archive") { ArchiveRow(onOpenArchive) }
            }
        }
    }
}

/**
 * Hero-карточка: суммарный баланс по всем группам крупной суммой. Разные
 * валюты не складываются: основная крупно, остальные — вторичной строкой.
 */
/**
 * Что сказать при нулевом общем итоге. Ноль — не всегда «всё погашено»: +500 в
 * одной группе и −500 в другой дают ноль при двух открытых долгах, а группы со
 * старыми данными в итог не входят вовсе (паритет iOS `ZeroBalanceSummary`).
 */
internal enum class ZeroBalanceSummary { CANCEL_OUT, UNAVAILABLE, PARTIAL, SETTLED }

internal fun zeroBalanceSummary(rooms: List<RoomSummary>): ZeroBalanceSummary = when {
    rooms.any { !it.debtsUnavailable && it.exactMyBalanceMinor != 0L } -> ZeroBalanceSummary.CANCEL_OUT
    rooms.any { it.debtsUnavailable } ->
        if (rooms.all { it.debtsUnavailable }) ZeroBalanceSummary.UNAVAILABLE else ZeroBalanceSummary.PARTIAL
    else -> ZeroBalanceSummary.SETTLED
}

@Composable
private fun SummaryCard(rooms: List<RoomSummary>, freshness: DataFreshness) {
    val totals = remember(rooms) {
        // Комнаты с неисчислимыми долгами не участвуют: их myBalance=0 —
        // заглушка, а не «в расчёте», и складывать её в итог нельзя.
        aggregateByCurrency(
            // ⚠️ Точные величины: из округлённых баланс 0,25 пришёл бы нулём,
            // и валюта пропала бы из сводки целиком.
            rooms.filterNot { it.debtsUnavailable }
                .map {
                    CurrencySum(
                        currency = it.currency,
                        sum = it.myBalance,
                        sumMinor = it.exactMyBalanceMinor,
                    )
                }
        )
    }
    val hasUnavailable = rooms.any { it.debtsUnavailable }
    val hasCountable = rooms.any { !it.debtsUnavailable }
    val zeroSummary = stringResource(
        when (zeroBalanceSummary(rooms)) {
            ZeroBalanceSummary.CANCEL_OUT -> R.string.groups_summary_cancel_out
            ZeroBalanceSummary.UNAVAILABLE -> R.string.group_debts_unavailable_title
            ZeroBalanceSummary.PARTIAL -> R.string.groups_summary_partial
            ZeroBalanceSummary.SETTLED -> R.string.groups_all_settled
        }
    )
    Column(Modifier.fillMaxWidth()) {
        ReceiptLabel(stringResource(R.string.groups_total_balance))
        Spacer(Modifier.height(6.dp))
        if (totals.isEmpty()) {
            Text(
                text = zeroSummary,
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = Splitty.colors.ink,
            )
        } else {
            BalanceSummaryLines(totals)
        }
        // Группы со старыми данными в итог не входят — и это сказано.
        if (hasUnavailable && hasCountable && !(totals.isEmpty() && zeroBalanceSummary(rooms) == ZeroBalanceSummary.PARTIAL)) {
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.groups_summary_partial),
                fontSize = 13.sp,
                color = Splitty.colors.inkSecondary,
            )
        }
        // Подпись о свежести: без неё человек смотрел на старые суммы, ничего
        // об этом не зная, — и «неправильный» баланс выглядел как ошибка расчёта
        if (freshness.fromCache) {
            Spacer(Modifier.height(6.dp))
            CacheNote(freshness = freshness, tag = "groups_cache_note")
        }
        Spacer(Modifier.height(14.dp))
        ReceiptDivider()
    }
}

/**
 * Корешок счёта группы: реквизит (дата · участники), название, остаток словами
 * и суммой. Пунктир и вырезы снизу — отрывная часть ленты (паритет iOS).
 */
@Composable
internal fun GroupStub(
    room: RoomSummary,
    hasPending: Boolean = false,
    showsBalance: Boolean = true,
    closed: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    ReceiptStub(modifier = modifier, closed = closed) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ReceiptLabel(GroupsDateFmt.dayMonth(room.createdAt), Modifier.weight(1f))
            // Есть неотправленные офлайн-операции в этой группе.
            if (hasPending) {
                Icon(
                    imageVector = Icons.Outlined.CloudOff,
                    contentDescription = stringResource(R.string.group_card_pending_badge),
                    tint = colors.inkSecondary,
                    modifier = Modifier.size(14.dp),
                )
            }
            // Непрочитанные события этой группы. Текст готовит общий с
            // бейджем вкладки badgeLabel: одно и то же число не должно
            // выглядеть на двух экранах по-разному.
            val unread = badgeLabel(room.unreadCount, stringResource(R.string.notifications_badge_overflow))
            if (unread != null) UnreadBadge(unread)
            ReceiptLabel(memberCountText(room.memberCount))
        }
        Spacer(Modifier.height(10.dp))
        Text(
            text = room.name,
            fontSize = 19.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (showsBalance) {
            Spacer(Modifier.height(12.dp))
            ReceiptDivider()
            Spacer(Modifier.height(10.dp))
            Row(
                modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
                verticalAlignment = Alignment.Bottom,
            ) {
                when {
                    // Долги неисчислимы (легаси-данные): честная метка вместо
                    // «в расчёте» — myBalance у таких комнат всегда 0.
                    room.debtsUnavailable -> Text(
                        text = stringResource(R.string.groups_row_debts_unavailable),
                        fontSize = 14.sp,
                        color = colors.inkSecondary,
                    )
                    room.exactMyBalanceMinor == 0L -> Text(
                        text = stringResource(R.string.groups_row_settled),
                        fontSize = 14.sp,
                        color = colors.inkSecondary,
                    )
                    else -> {
                        Text(
                            text = stringResource(
                                if (room.exactMyBalanceMinor > 0) R.string.groups_row_owed else R.string.groups_row_owes
                            ),
                            modifier = Modifier.weight(1f),
                            fontSize = 14.sp,
                            color = colors.ink,
                        )
                        MoneyText(
                            room.myBalance,
                            exactMinor = room.exactMyBalanceMinor,
                            size = 15.sp,
                            weight = FontWeight.Bold,
                            currency = room.currency,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun GroupCard(
    room: RoomSummary,
    hasPending: Boolean,
    onClick: () -> Unit,
    onArchive: () -> Unit,
) {
    // Долгое зажатие вместо свайпа: корешки лежат в LazyColumn как обычные
    // элементы, и свайп пришлось бы городить руками поверх горизонтальных
    // жестов. Меню — паритет с iOS, где стоит .contextMenu.
    var isMenuOpen by remember { mutableStateOf(false) }
    Box {
        GroupStub(
            room = room,
            hasPending = hasPending,
            modifier = Modifier.combinedClickable(onClick = onClick, onLongClick = { isMenuOpen = true }),
        )
        // Меню якорится к корешку: всплывает там, где человек держал палец.
        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.groups_archive_action)) },
                onClick = {
                    isMenuOpen = false
                    onArchive()
                },
            )
        }
    }
}

/**
 * Счётчик непрочитанного на карточке группы: акцентная капсула рядом с
 * названием (порт iOS GroupsListView.UnreadBadge — вид должен совпадать).
 */
@Composable
private fun UnreadBadge(text: String) {
    val colors = Splitty.colors
    val description = stringResource(R.string.group_card_unread_badge, text)
    Text(
        text = text,
        fontSize = 11.sp,
        fontWeight = FontWeight.Bold,
        fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
        color = colors.onAccent,
        modifier = Modifier
            .clip(RoundedCornerShape(2.dp))
            .background(colors.accent)
            .padding(horizontal = 6.dp, vertical = 2.dp)
            .semantics { contentDescription = description },
    )
}

/** «Архив» — закрытые счета: тихая подчёркнутая ссылка под корешками. */
@Composable
private fun ArchiveRow(onClick: () -> Unit) {
    TextLinkButton(text = stringResource(R.string.groups_archive) + " ↗", onClick = onClick)
}

// MARK: - Архив

/**
 * Архивные группы: карточки с чипом «Разархивировать»; тап по карточке
 * открывает группу (внутри — бейдж «Группа в архиве»).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArchivedGroupsScreen(
    onBack: () -> Unit,
    onOpenRoom: (String) -> Unit,
    viewModel: GroupsListViewModel = hiltViewModel(),
) {
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { viewModel.onArchiveOpened() }

    val state by viewModel.archived.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val isMutating by viewModel.isMutating.collectAsStateWithLifecycle()
    val alertMessage by viewModel.alertMessage.collectAsStateWithLifecycle()
    val colors = Splitty.colors

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(R.string.groups_archive),
                        fontWeight = FontWeight.Bold,
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
                ),
            )
        },
    ) { innerPadding ->
        when (val current = state) {
            UiState.Loading -> GroupsLoading(Modifier.padding(innerPadding))

            is UiState.Error -> GroupsFullScreenError(
                message = current.message.resolve(),
                onRetry = viewModel::onArchiveOpened,
                modifier = Modifier.padding(innerPadding),
            )

            is UiState.Content -> PullToRefreshBox(
                isRefreshing = isRefreshing,
                onRefresh = viewModel::refresh,
                modifier = Modifier
                    .padding(innerPadding)
                    .fillMaxSize(),
            ) {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(20.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    if (current.value.isEmpty()) {
                        item(key = "empty") {
                            GroupsEmptyCard(
                                icon = Icons.Outlined.Archive,
                                title = stringResource(R.string.groups_archive_empty_title),
                                subtitle = stringResource(R.string.groups_archive_empty_subtitle),
                            )
                        }
                    } else {
                        items(current.value, key = { it.id }) { room ->
                            ArchivedGroupCard(
                                room = room,
                                isMutating = isMutating,
                                onOpen = { onOpenRoom(room.id) },
                                onUnarchive = { viewModel.unarchive(room.id) },
                            )
                        }
                    }
                }
            }
        }
    }

    GroupsAlertDialog(alertMessage, viewModel::dismissAlert)
}

/**
 * Закрытый счёт: корешок на бумаге закрытого счёта. «Разархивировать» —
 * СОСЕД, а не часть clickable-зоны (иначе хит-зона конфликтует с переходом
 * в группу; порт iOS ArchivedGroupsView).
 */
@Composable
private fun ArchivedGroupCard(
    room: RoomSummary,
    isMutating: Boolean,
    onOpen: () -> Unit,
    onUnarchive: () -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        GroupStub(
            room = room,
            showsBalance = false,
            closed = true,
            modifier = Modifier.clickable(onClick = onOpen),
        )
        TextLinkButton(
            text = stringResource(R.string.groups_unarchive),
            onClick = { if (!isMutating) onUnarchive() },
        )
    }
}

// MARK: - Создание и присоединение

/** Создание группы: поле «Название» + CTA «Создать» (POST /rooms). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CreateGroupSheet(viewModel: GroupsListViewModel, onDismiss: () -> Unit) {
    val isMutating by viewModel.isMutating.collectAsStateWithLifecycle()
    val currencies by viewModel.currencies.collectAsStateWithLifecycle()
    val rooms by viewModel.rooms.collectAsStateWithLifecycle()
    var name by rememberSaveable { mutableStateOf("") }
    // null — справочник ещё не пришёл: валюту, которой человек не видел, не
    // отправляем, и туса заводится прежним умолчанием сервера.
    var selected by rememberSaveable { mutableStateOf<String?>(null) }
    var picked by rememberSaveable { mutableStateOf(false) }

    LaunchedEffect(Unit) { viewModel.loadCurrencies() }
    LaunchedEffect(currencies) {
        if (selected == null && currencies.isNotEmpty()) {
            selected = suggestedRoomCurrency(
                recent = (rooms as? UiState.Content)?.value?.firstOrNull()?.currency,
                region = runCatching { Currency.getInstance(Locale.getDefault()).currencyCode }.getOrNull(),
                available = currencies.map { it.code },
            )
        }
    }

    val canSubmit = name.trim().isNotEmpty() && !isMutating
    val submit = {
        if (canSubmit) {
            viewModel.createGroup(
                name = name,
                currency = selected.takeIf { currencies.isNotEmpty() },
                picked = picked,
                onSuccess = onDismiss,
            )
        }
    }

    GroupFormSheet(
        title = stringResource(R.string.groups_create_title),
        onDismiss = onDismiss,
    ) {
        GroupsTextField(
            value = name,
            onValueChange = { name = it },
            placeholder = stringResource(R.string.groups_create_placeholder),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { submit() }),
        )
        Spacer(Modifier.height(10.dp))
        Text(
            text = stringResource(R.string.groups_create_hint),
            fontSize = 13.sp,
            color = Splitty.colors.inkSecondary,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        val current = selected
        if (current != null && currencies.isNotEmpty()) {
            Spacer(Modifier.height(12.dp))
            CreateCurrencyRow(
                currencies = currencies,
                selected = current,
                enabled = !isMutating,
                onSelect = { code ->
                    if (code != selected) picked = true
                    selected = code
                },
            )
            Spacer(Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.groups_create_currency_hint),
                fontSize = 13.sp,
                color = Splitty.colors.inkSecondary,
                modifier = Modifier.padding(horizontal = 4.dp),
            )
        }
        Spacer(Modifier.height(16.dp))
        PrimaryPillButton(
            text = stringResource(R.string.groups_create_button),
            onClick = submit,
            enabled = canSubmit,
        )
    }
}

/**
 * Строка «Валюта» с выпадающим списком: один экран, а не мастер — создание тусы
 * воронка, и лишний шаг тут только теряет людей.
 */
@Composable
private fun CreateCurrencyRow(
    currencies: List<CurrencyInfo>,
    selected: String,
    enabled: Boolean,
    onSelect: (String) -> Unit,
) {
    val colors = Splitty.colors
    var expanded by remember { mutableStateOf(false) }
    val label = currencies.firstOrNull { it.code == selected }
        ?.let { "${it.flag} ${it.code}" }
        ?: selected

    Box {
        SurfaceCard(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(enabled = enabled) { expanded = true },
            padding = 14.dp,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = stringResource(R.string.group_settings_currency),
                    fontSize = 17.sp,
                    color = colors.ink,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = label,
                    fontSize = 17.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.inkSecondary,
                )
                Spacer(Modifier.width(6.dp))
                Icon(
                    imageVector = Icons.Outlined.UnfoldMore,
                    contentDescription = null,
                    tint = colors.inkSecondary,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            currencies.forEach { currency ->
                DropdownMenuItem(
                    text = { Text("${currency.flag} ${currency.code} · ${currency.symbol}") },
                    onClick = {
                        onSelect(currency.code)
                        expanded = false
                    },
                )
            }
        }
    }
}

/** Присоединение по коду приглашения или ссылке (POST /rooms/{id}/join). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun JoinGroupSheet(viewModel: GroupsListViewModel, onDismiss: () -> Unit) {
    val isMutating by viewModel.isMutating.collectAsStateWithLifecycle()
    var code by rememberSaveable { mutableStateOf("") }
    val canSubmit = parseRoomCode(code) != null && !isMutating
    val submit = { if (canSubmit) viewModel.joinGroup(code, onSuccess = onDismiss) }
    val clipboard = LocalClipboardManager.current
    val colors = Splitty.colors

    GroupFormSheet(
        title = stringResource(R.string.groups_join_title),
        onDismiss = onDismiss,
    ) {
        // Код никто не набирает руками — его присылают ссылкой. Кнопка
        // «Вставить» подставляет содержимое буфера в один тап.
        Row(verticalAlignment = Alignment.CenterVertically) {
            GroupsTextField(
                value = code,
                onValueChange = { code = it },
                placeholder = stringResource(R.string.groups_join_placeholder),
                modifier = Modifier.weight(1f),
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                    imeAction = ImeAction.Done,
                ),
                keyboardActions = KeyboardActions(onDone = { submit() }),
            )
            Spacer(Modifier.width(4.dp))
            TextButton(
                onClick = { clipboard.getText()?.text?.let { code = it } },
                contentPadding = PaddingValues(horizontal = 12.dp),
            ) {
                Icon(
                    imageVector = Icons.Outlined.ContentPaste,
                    contentDescription = null,
                    tint = colors.ink,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(4.dp))
                Text(
                    text = stringResource(R.string.groups_join_paste),
                    fontSize = 15.sp,
                    color = colors.ink,
                )
            }
        }
        Spacer(Modifier.height(10.dp))
        Text(
            text = stringResource(R.string.groups_join_hint),
            fontSize = 13.sp,
            color = Splitty.colors.inkSecondary,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        Spacer(Modifier.height(16.dp))
        PrimaryPillButton(
            text = stringResource(R.string.groups_join_button),
            onClick = submit,
            enabled = canSubmit,
        )
    }
}

/** Общая обвязка форм-шитов: заголовок по центру + контент с imePadding. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GroupFormSheet(
    title: String,
    onDismiss: () -> Unit,
    content: @Composable () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = Splitty.colors.surface,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .padding(bottom = 36.dp)
                .imePadding(),
        ) {
            Text(
                text = title,
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = Splitty.colors.ink,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
            Spacer(Modifier.height(16.dp))
            content()
        }
    }
}

/**
 * Меню кнопки «+» в списке групп: создать группу или войти по коду.
 *
 * Меню, а не одна кнопка: ввод кода жил ТОЛЬКО в пустом состоянии списка, и
 * человек с одной группой попасть в него не мог никак — приглашение по коду
 * переставало работать ровно после первой группы. А приходит оно как раз к
 * тем, у кого группы уже есть.
 */
@Composable
internal fun GroupsAddMenu(onCreate: () -> Unit, onJoinByCode: () -> Unit) {
    val colors = Splitty.colors
    var isMenuOpen by rememberSaveable { mutableStateOf(false) }
    Box {
        // Текстом, а не квадратным «+»: второй «+» рядом с оранжевым
        // «Добавить расход» внизу путал, что именно добавляется.
        val description = stringResource(R.string.groups_create_group)
        Box(
            modifier = Modifier
                .padding(end = 8.dp)
                .defaultMinSize(minWidth = 48.dp, minHeight = 48.dp)
                .clickable(role = Role.Button, onClick = { isMenuOpen = true })
                .semantics { contentDescription = description }
                .padding(horizontal = 8.dp)
                .testTag("groups_add_menu"),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = stringResource(R.string.groups_new),
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.clearAndSetSemantics {},
            )
        }
        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.groups_create_group)) },
                onClick = {
                    isMenuOpen = false
                    onCreate()
                },
            )
            DropdownMenuItem(
                modifier = Modifier.testTag("groups_join_by_code"),
                text = { Text(stringResource(R.string.groups_join_by_code)) },
                onClick = {
                    isMenuOpen = false
                    onJoinByCode()
                },
            )
        }
    }
}
