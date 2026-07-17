package com.zagir.splitty.ui.groups

import androidx.activity.compose.BackHandler
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Login
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
import com.zagir.splitty.core.model.CurrencySum
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.money.aggregateByCurrency
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.MoneyTotalsText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

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
    var isCreatePresented by rememberSaveable { mutableStateOf(false) }
    var isJoinPresented by rememberSaveable { mutableStateOf(false) }
    val colors = Splitty.colors

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(R.string.groups_title),
                        fontWeight = FontWeight.Bold,
                    )
                },
                // Пункт один — прямая кнопка «Присоединиться» вместо меню-матрёшки
                // из одного пункта (порт iOS toolbar `.topBarLeading`).
                navigationIcon = {
                    IconButton(onClick = { isJoinPresented = true }) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.Login,
                            contentDescription = stringResource(R.string.groups_join_by_code),
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                    navigationIconContentColor = colors.ink,
                    actionIconContentColor = colors.ink,
                ),
                actions = {
                    IconButton(onClick = { isCreatePresented = true }) {
                        Icon(
                            imageVector = Icons.Filled.Add,
                            contentDescription = stringResource(R.string.groups_create_group),
                        )
                    }
                },
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

            is UiState.Content -> RoomsList(
                rooms = current.value,
                pendingRoomIds = pendingRoomIds,
                isRefreshing = isRefreshing,
                onRefresh = viewModel::refresh,
                onOpenRoom = onOpenRoom,
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
    pendingRoomIds: Set<String>,
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    onOpenRoom: (String) -> Unit,
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
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 24.dp),
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
                        Spacer(Modifier.height(10.dp))
                        SoftChip(
                            text = stringResource(R.string.groups_join_by_code),
                            onClick = onJoin,
                        )
                    }
                }
                // Без строки «Архив»: в пустом состоянии она отвлекает от первого шага.
            } else {
                item(key = "summary") { SummaryCard(rooms) }
                items(rooms, key = { it.id }) { room ->
                    GroupCard(
                        room = room,
                        hasPending = room.id in pendingRoomIds,
                        onClick = { onOpenRoom(room.id) },
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
@Composable
private fun SummaryCard(rooms: List<RoomSummary>) {
    val totals = remember(rooms) {
        aggregateByCurrency(rooms.map { CurrencySum(currency = it.currency, sum = it.myBalance) })
    }
    val primarySign = totals.firstOrNull()?.sum ?: 0
    val subtitle = when {
        primarySign > 0 -> stringResource(R.string.group_you_are_owed)
        primarySign < 0 -> stringResource(R.string.group_you_owe)
        else -> stringResource(R.string.groups_all_settled)
    }
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 20.dp) {
        SectionHeader(stringResource(R.string.groups_total_balance))
        Spacer(Modifier.height(6.dp))
        MoneyTotalsText(totals = totals)
        Spacer(Modifier.height(6.dp))
        Text(
            text = subtitle,
            fontSize = 15.sp,
            color = Splitty.colors.inkSecondary,
        )
    }
}

/** Карточка группы: аватар-градиент, название, участники, баланс, chevron. */
@Composable
private fun GroupCard(room: RoomSummary, hasPending: Boolean, onClick: () -> Unit) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(20.dp))
                .clickable(onClick = onClick)
                .padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            GroupAvatar(roomId = room.id, name = room.name, size = 46.dp)
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = room.name,
                        fontSize = 16.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    // Есть неотправленные офлайн-операции в этой группе.
                    if (hasPending) {
                        Icon(
                            imageVector = Icons.Outlined.CloudOff,
                            contentDescription = stringResource(R.string.group_card_pending_badge),
                            tint = colors.inkSecondary,
                            modifier = Modifier.size(15.dp),
                        )
                    }
                }
                Text(
                    text = memberCountText(room.memberCount),
                    fontSize = 13.sp,
                    color = colors.inkSecondary,
                )
            }
            if (room.myBalance == 0) {
                Text(
                    text = stringResource(R.string.groups_row_settled),
                    fontSize = 14.sp,
                    color = colors.inkSecondary,
                )
            } else {
                Column(
                    horizontalAlignment = Alignment.End,
                    verticalArrangement = Arrangement.spacedBy(2.dp),
                ) {
                    Text(
                        text = stringResource(
                            if (room.myBalance > 0) R.string.groups_row_owed else R.string.groups_row_owes
                        ),
                        fontSize = 11.sp,
                        color = colors.inkSecondary,
                    )
                    MoneyText(room.myBalance, size = 15.sp, currency = room.currency)
                }
            }
            ChevronIcon()
        }
    }
}

/** «Архив» — тихая строка внизу списка, без карточки. */
@Composable
private fun ArchiveRow(onClick: () -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Archive,
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.size(20.dp),
        )
        Text(
            text = stringResource(R.string.groups_archive),
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = colors.inkSecondary,
        )
        Spacer(Modifier.weight(1f))
        ChevronIcon()
    }
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
                message = current.message,
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
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
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

@Composable
private fun ArchivedGroupCard(
    room: RoomSummary,
    isMutating: Boolean,
    onOpen: () -> Unit,
    onUnarchive: () -> Unit,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(end = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Открытие группы — только по блоку аватар+имя: «Разархивировать» —
            // СОСЕД, а не часть clickable-зоны (иначе хит-зона чипа конфликтует
            // с переходом в группу; порт iOS ArchivedGroupsView).
            Row(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(20.dp))
                    .clickable(onClick = onOpen)
                    .padding(16.dp),
                horizontalArrangement = Arrangement.spacedBy(14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                GroupAvatar(roomId = room.id, name = room.name, size = 46.dp)
                Column(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Text(
                        text = room.name,
                        fontSize = 16.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = memberCountText(room.memberCount),
                        fontSize = 13.sp,
                        color = colors.inkSecondary,
                    )
                }
            }
            SoftChip(
                text = stringResource(R.string.groups_unarchive),
                onClick = { if (!isMutating) onUnarchive() },
            )
        }
    }
}

// MARK: - Создание и присоединение

/** Создание группы: поле «Название» + CTA «Создать» (POST /rooms). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CreateGroupSheet(viewModel: GroupsListViewModel, onDismiss: () -> Unit) {
    val isMutating by viewModel.isMutating.collectAsStateWithLifecycle()
    var name by rememberSaveable { mutableStateOf("") }
    val canSubmit = name.trim().isNotEmpty() && !isMutating
    val submit = { if (canSubmit) viewModel.createGroup(name, onSuccess = onDismiss) }

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
        Spacer(Modifier.height(16.dp))
        PrimaryPillButton(
            text = stringResource(R.string.groups_create_button),
            onClick = submit,
            enabled = canSubmit,
        )
    }
}

/** Присоединение по коду приглашения или ссылке (POST /rooms/{id}/join). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun JoinGroupSheet(viewModel: GroupsListViewModel, onDismiss: () -> Unit) {
    val isMutating by viewModel.isMutating.collectAsStateWithLifecycle()
    var code by rememberSaveable { mutableStateOf("") }
    val canSubmit = parseRoomCode(code).isNotEmpty() && !isMutating
    val submit = { if (canSubmit) viewModel.joinGroup(code, onSuccess = onDismiss) }

    GroupFormSheet(
        title = stringResource(R.string.groups_join_title),
        onDismiss = onDismiss,
    ) {
        GroupsTextField(
            value = code,
            onValueChange = { code = it },
            placeholder = stringResource(R.string.groups_join_placeholder),
            keyboardOptions = KeyboardOptions(
                capitalization = KeyboardCapitalization.None,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Done,
            ),
            keyboardActions = KeyboardActions(onDone = { submit() }),
        )
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
