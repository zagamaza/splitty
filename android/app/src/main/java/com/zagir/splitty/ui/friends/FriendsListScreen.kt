package com.zagir.splitty.ui.friends

import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.WifiOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
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
import com.zagir.splitty.core.model.CurrencySum
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.money.aggregateByCurrency
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyTotalsText
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Вкладка «Друзья»: hero-карточка «Общий баланс» и карточные строки друзей.
 * Порт ios/Splitty/Features/Friends/FriendsListView.swift.
 */
@Composable
fun FriendsListScreen(
    onOpenFriend: (FriendBalance) -> Unit,
    viewModel: FriendsViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val errorMessage by viewModel.errorMessage.collectAsStateWithLifecycle()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg),
    ) {
        Text(
            text = stringResource(R.string.tab_friends),
            modifier = Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp),
            fontSize = 32.sp,
            fontWeight = FontWeight.Bold,
            color = Splitty.colors.ink,
        )
        when (val current = state) {
            is UiState.Loading -> FriendsLoadingView()
            is UiState.Error -> FriendsErrorView(current.message, onRetry = viewModel::retry)
            is UiState.Content -> FriendsList(
                friends = current.value,
                isRefreshing = isRefreshing,
                onRefresh = viewModel::refresh,
                onOpenFriend = onOpenFriend,
            )
        }
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

/** Лента: hero-карточка общего баланса + карточные строки друзей. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FriendsList(
    friends: List<FriendBalance>,
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    onOpenFriend: (FriendBalance) -> Unit,
) {
    // Нетто по всем друзьям ПОВАЛЮТНО (разные валюты не складываются).
    val totals = remember(friends) {
        aggregateByCurrency(friends.flatMap { it.totalsByCurrency })
    }

    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = Modifier.fillMaxSize(),
    ) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            // Запас снизу — под центральную кнопку «+» таб-бара.
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 48.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (friends.isEmpty()) {
                item {
                    FriendsEmptyView(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 120.dp),
                    )
                }
            } else {
                item { TotalHeader(totals) }
                items(friends, key = { it.user.id }) { friend ->
                    FriendRow(friend = friend, onClick = { onOpenFriend(friend) })
                }
            }
        }
    }
}

/**
 * Hero-карточка «Общий баланс»: основная валюта крупно, остальные —
 * вторичной строкой; подпись — по знаку основной валюты.
 */
@Composable
private fun TotalHeader(totals: List<CurrencySum>) {
    SurfaceCard(
        modifier = Modifier.fillMaxWidth(),
        padding = 20.dp,
    ) {
        SectionHeader(stringResource(R.string.friends_total_balance))
        Spacer(Modifier.height(8.dp))
        MoneyTotalsText(totals = totals)
        Spacer(Modifier.height(8.dp))
        val primary = totals.firstOrNull()?.sum ?: 0
        Text(
            text = when {
                primary > 0 -> stringResource(R.string.friends_you_are_owed)
                primary < 0 -> stringResource(R.string.friends_you_owe)
                else -> stringResource(R.string.friends_all_settled)
            },
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.inkSecondary,
        )
    }
}

/** Карточная строка друга: градиентный аватар 48dp, имя, справа нетто-баланс. */
@Composable
private fun FriendRow(friend: FriendBalance, onClick: () -> Unit) {
    SurfaceCard(
        modifier = Modifier.fillMaxWidth(),
        padding = 0.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            GradientAvatar(user = friend.user, size = 48.dp)
            Text(
                text = friend.user.displayName,
                modifier = Modifier.weight(1f),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = Splitty.colors.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            FriendRowTrailing(totals = friend.totals)
        }
    }
}

/**
 * Нетто с другом: подпись по знаку основной валюты и суммы по валютам
 * (основная — обычным размером, остальные — мельче вторичной строкой);
 * полный расчёт — серое «расчёт».
 */
@Composable
private fun FriendRowTrailing(totals: List<CurrencySum>) {
    val primary = totals.firstOrNull()
    if (primary != null) {
        Column(horizontalAlignment = Alignment.End) {
            Text(
                text = if (primary.sum > 0) {
                    stringResource(R.string.friends_owes_you_short)
                } else {
                    stringResource(R.string.friends_you_owe_short)
                },
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                color = Splitty.colors.inkSecondary,
            )
            Spacer(Modifier.height(2.dp))
            MoneyTotalsText(
                totals = totals,
                primarySize = 17.sp,
                secondarySize = 13.sp,
                horizontalAlignment = Alignment.End,
            )
        }
    } else {
        Text(
            text = stringResource(R.string.friends_settlement),
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.inkSecondary,
        )
    }
}

@Composable
private fun FriendsLoadingView() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Splitty.colors.accent)
    }
}

/** Ошибка первичной загрузки: иконка, текст и кнопка «Повторить». */
@Composable
private fun FriendsErrorView(message: String, onRetry: () -> Unit) {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        FailedState(message = message, onRetry = onRetry)
    }
}

@Composable
private fun FriendsEmptyView(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.Group,
            contentDescription = null,
            modifier = Modifier.size(44.dp),
            tint = Splitty.colors.inkSecondary,
        )
        Text(
            text = stringResource(R.string.friends_empty_title),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = Splitty.colors.ink,
        )
        Text(
            text = stringResource(R.string.friends_empty_description),
            modifier = Modifier.padding(horizontal = 24.dp),
            fontSize = 15.sp,
            color = Splitty.colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
    }
}
