package com.zagir.splitty.ui.friends

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.WifiOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.FriendRoomBalance
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.Glossary
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.MoneyTotalsText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Экран друга: шапка с аватаром 88dp и hero-суммой нетто по валютам,
 * карточка «По группам» (тап по строке — переход в группу).
 * Порт ios/Splitty/Features/Friends/FriendDetailView.swift.
 * userId/name приходят nav-аргументами (читает [FriendDetailViewModel]).
 */
@Composable
fun FriendDetailScreen(
    onBack: () -> Unit,
    onOpenRoom: (String) -> Unit,
    onSettleUp: (String) -> Unit,
    viewModel: FriendDetailViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val errorMessage by viewModel.errorMessage.collectAsStateWithLifecycle()
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()

    val friend = (state as? UiState.Content)?.value
    // Пикер группы для погашения (несколько общих групп → выбор, как iOS).
    var showRoomPicker by remember { mutableStateOf(false) }

    // Тап «Погасить»: офлайн — алерт; одна группа — сразу; несколько — пикер.
    val onSettleTap: () -> Unit = {
        val rooms = friend?.rooms.orEmpty()
        when {
            !isOnline -> viewModel.showOfflineSettleError()
            rooms.size == 1 -> onSettleUp(rooms.first().roomId)
            rooms.isNotEmpty() -> showRoomPicker = true
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg),
    ) {
        TopBar(
            title = friend?.user?.displayName.orEmpty(),
            onBack = onBack,
        )
        when (val current = state) {
            is UiState.Loading -> LoadingView()
            is UiState.Error -> ErrorView(current.message, onRetry = viewModel::retry)
            is UiState.Content -> FriendDetailContent(
                friend = current.value,
                isRefreshing = isRefreshing,
                onRefresh = viewModel::refresh,
                onOpenRoom = onOpenRoom,
                onSettleUp = onSettleTap,
            )
        }
    }

    if (showRoomPicker && friend != null) {
        SettleRoomPicker(
            rooms = friend.rooms,
            onDismiss = { showRoomPicker = false },
            onPick = { roomId ->
                showRoomPicker = false
                onSettleUp(roomId)
            },
        )
    }

    val message = errorMessage
    if (message != null) {
        AlertDialog(
            onDismissRequest = viewModel::dismissError,
            title = { Text(stringResource(R.string.common_error_title)) },
            text = { Text(message) },
            confirmButton = {
                TextButton(onClick = viewModel::dismissError) {
                    Text(stringResource(R.string.common_ok))
                }
            },
        )
    }
}

/** Выбор группы для погашения при нескольких общих группах (порт iOS action sheet). */
@Composable
private fun SettleRoomPicker(
    rooms: List<FriendRoomBalance>,
    onDismiss: () -> Unit,
    onPick: (String) -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.friend_settle_room_picker_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                rooms.forEach { room ->
                    TextButton(
                        onClick = { onPick(room.roomId) },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(
                            text = room.roomName,
                            modifier = Modifier.fillMaxWidth(),
                            color = Splitty.colors.ink,
                        )
                    }
                }
            }
        },
        confirmButton = {},
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.common_cancel))
            }
        },
    )
}

/** Инлайн-шапка: кнопка «Назад» и имя друга по центру. */
@Composable
private fun TopBar(title: String, onBack: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(52.dp),
    ) {
        IconButton(onClick = onBack, modifier = Modifier.align(Alignment.CenterStart)) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                contentDescription = stringResource(R.string.common_back),
                tint = Splitty.colors.ink,
            )
        }
        Text(
            text = title,
            modifier = Modifier
                .align(Alignment.Center)
                .padding(horizontal = 56.dp),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = Splitty.colors.ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FriendDetailContent(
    friend: FriendBalance,
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    onOpenRoom: (String) -> Unit,
    onSettleUp: () -> Unit,
) {
    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = Modifier.fillMaxSize(),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Header(friend)
            // CTA «Погасить» — только когда есть непогашенные долги по группам.
            if (friend.totals.isNotEmpty() && friend.rooms.isNotEmpty()) {
                PrimaryPillButton(
                    text = stringResource(R.string.friend_settle_up),
                    onClick = onSettleUp,
                )
            }
            GroupsSection(rooms = friend.rooms, onOpenRoom = onOpenRoom)
            Spacer(Modifier.height(32.dp))
        }
    }
}

/** Шапка: аватар 88dp, имя, @username и hero-нетто по валютам. */
@Composable
private fun Header(friend: FriendBalance) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        GradientAvatar(user = friend.user, size = 88.dp)
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = friend.user.displayName,
                fontSize = 24.sp,
                fontWeight = FontWeight.SemiBold,
                color = Splitty.colors.ink,
            )
            val username = friend.user.username
            if (!username.isNullOrEmpty()) {
                Text(
                    text = "@$username",
                    fontSize = 15.sp,
                    color = Splitty.colors.inkSecondary,
                )
            }
        }
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            val totals = friend.totals
            val primary = totals.firstOrNull()?.sum ?: 0
            SectionHeader(
                // Единый settled-текст на всех экранах: «Все долги погашены».
                text = when {
                    primary > 0 -> stringResource(R.string.friend_owed_caption)
                    primary < 0 -> stringResource(R.string.friends_you_owe)
                    else -> stringResource(R.string.friends_all_settled)
                },
            )
            // Нетто по валютам: основная крупно, остальные — вторичной строкой.
            MoneyTotalsText(
                totals = totals,
                primarySize = 38.sp,
                horizontalAlignment = Alignment.CenterHorizontally,
            )
        }
    }
}

/** Карточка «По группам»: строки с балансом в валюте группы, тап — в группу. */
@Composable
private fun GroupsSection(rooms: List<FriendRoomBalance>, onOpenRoom: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            text = stringResource(R.string.friend_groups_section),
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        SurfaceCard(
            modifier = Modifier.fillMaxWidth(),
            padding = 0.dp,
        ) {
            if (rooms.isEmpty()) {
                Text(
                    text = stringResource(R.string.friend_no_group_debts),
                    modifier = Modifier.padding(16.dp),
                    fontSize = 15.sp,
                    color = Splitty.colors.inkSecondary,
                )
            } else {
                rooms.forEachIndexed { index, room ->
                    RoomRow(room = room, onClick = { onOpenRoom(room.roomId) })
                    if (index != rooms.lastIndex) {
                        HairlineDivider()
                    }
                }
            }
        }
    }
}

/** Строка группы: название и баланс с другом в валюте этой группы. */
@Composable
private fun RoomRow(room: FriendRoomBalance, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = room.roomName,
            modifier = Modifier.weight(1f),
            fontSize = 16.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Column(horizontalAlignment = Alignment.End) {
            Text(
                // Через Glossary: тернарник «>0 ? вам : вы» на нулевом балансе
                // показывал «вы должны» в полностью рассчитанной группе.
                text = Glossary.balanceCaption(room.balance),
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                color = Splitty.colors.inkSecondary,
            )
            Spacer(Modifier.height(2.dp))
            // Баланс комнаты — в валюте самой комнаты.
            MoneyText(room.balance, size = 16.sp, currency = room.currency)
        }
        Icon(
            imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
        )
    }
}

/** Hairline-разделитель между строками внутри карточки. */
@Composable
private fun HairlineDivider() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp)
            .height(1.dp)
            .background(Splitty.colors.hairline),
    )
}

@Composable
private fun LoadingView() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Splitty.colors.accent)
    }
}

/** Ошибка первичной загрузки: иконка, текст и кнопка «Повторить». */
@Composable
private fun ErrorView(message: String, onRetry: () -> Unit) {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        FailedState(message = message, onRetry = onRetry)
    }
}
