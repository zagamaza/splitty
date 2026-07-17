package com.zagir.splitty.ui.activity

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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.IconButton
import androidx.compose.material.icons.filled.Schedule
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ActivityItem
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Вкладка «Активность»: лента карточных строк операций всех групп,
 * пагинация offset/limit по скроллу, тап по строке — переход в группу.
 * Порт ios/Splitty/Features/Activity/ActivityView.swift.
 */
@Composable
fun ActivityScreen(
    onOpenRoom: (String) -> Unit,
    viewModel: ActivityViewModel = hiltViewModel(),
) {
    val state by viewModel.displayItems.collectAsStateWithLifecycle()
    val isMineOnly by viewModel.isMineOnly.collectAsStateWithLifecycle()
    val isRefreshing by viewModel.isRefreshing.collectAsStateWithLifecycle()
    val isLoadingMore by viewModel.isLoadingMore.collectAsStateWithLifecycle()
    val errorMessage by viewModel.errorMessage.collectAsStateWithLifecycle()
    val myUserId by viewModel.myUserId.collectAsStateWithLifecycle()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.tab_activity),
                modifier = Modifier.weight(1f),
                fontSize = 32.sp,
                fontWeight = FontWeight.Bold,
                color = Splitty.colors.ink,
            )
            // Фильтр «Только мои»: операции, где я донор или в получателях.
            val filterState = stringResource(
                if (isMineOnly) R.string.activity_filter_on else R.string.activity_filter_off
            )
            IconButton(
                onClick = viewModel::toggleMineOnly,
                // stateDescription: TalkBack читает «включено/выключено» у фильтра.
                modifier = Modifier.semantics { stateDescription = filterState },
            ) {
                Icon(
                    imageVector = if (isMineOnly) Icons.Filled.Person else Icons.Outlined.Person,
                    contentDescription = stringResource(R.string.activity_mine_only),
                    tint = if (isMineOnly) Splitty.colors.accent else Splitty.colors.inkSecondary,
                )
            }
        }
        when (val current = state) {
            is UiState.Loading -> ActivityLoadingView()
            is UiState.Error -> ActivityErrorView(current.message, onRetry = viewModel::retry)
            is UiState.Content -> ActivityFeed(
                items = current.value,
                isMineOnly = isMineOnly,
                myUserId = myUserId,
                isRefreshing = isRefreshing,
                isLoadingMore = isLoadingMore,
                onRefresh = viewModel::refresh,
                onItemShown = viewModel::onItemShown,
                onOpenRoom = onOpenRoom,
                onShowAll = viewModel::toggleMineOnly,
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

/** Лента карточных строк; подгрузка страниц — по последнему видимому индексу. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ActivityFeed(
    items: List<ActivityItem>,
    isMineOnly: Boolean,
    myUserId: Long?,
    isRefreshing: Boolean,
    isLoadingMore: Boolean,
    onRefresh: () -> Unit,
    onItemShown: (Int) -> Unit,
    onOpenRoom: (String) -> Unit,
    onShowAll: () -> Unit,
) {
    val listState = rememberLazyListState()

    // Пагинация: следим за последним видимым индексом (аналог .task на строке).
    LaunchedEffect(listState, items.size) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { onItemShown(it) }
    }

    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = Modifier.fillMaxSize(),
    ) {
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            // Запас снизу — под центральную кнопку «+» таб-бара.
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 48.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (items.isEmpty()) {
                item {
                    ActivityEmptyView(
                        isMineOnly = isMineOnly,
                        onShowAll = onShowAll,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 120.dp),
                    )
                }
            } else {
                itemsIndexed(items, key = { _, item -> item.operation.id }) { _, item ->
                    ActivityRow(
                        item = item,
                        myUserId = myUserId,
                        onClick = { onOpenRoom(item.roomId) },
                    )
                }
                if (isLoadingMore) {
                    item {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 12.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(28.dp),
                                color = Splitty.colors.accent,
                            )
                        }
                    }
                }
            }
        }
    }
}

/**
 * Карточная строка ленты: аватар донора, титул с жирными именами,
 * ваша позиция (MoneyText в валюте комнаты) и относительное время.
 */
@Composable
private fun ActivityRow(item: ActivityItem, myUserId: Long?, onClick: () -> Unit) {
    SurfaceCard(
        modifier = Modifier.fillMaxWidth(),
        padding = 0.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                // Карточка — один элемент для TalkBack (заголовок+позиция+время).
                .semantics(mergeDescendants = true) {}
                .padding(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            GradientAvatar(user = item.operation.donor, size = 44.dp)
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                ActivityTitle(item = item, myUserId = myUserId)
                ActivityPosition(item = item, myUserId = myUserId)
                Text(
                    text = relativeTimeText(item.operation.createdAt),
                    fontSize = 12.sp,
                    color = Splitty.colors.inkSecondary,
                )
            }
            // Chevron — аффорданс перехода в группу операции.
            Icon(
                imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

/**
 * Титул: «Загир добавил(а) «Ужин» в группе «Стамбул»» /
 * «Загир заплатил(а) вам 500 ₽ в группе «Стамбул»» — имена и сумма жирным.
 */
@Composable
private fun ActivityTitle(item: ActivityItem, myUserId: Long?) {
    val op = item.operation
    val inGroup = stringResource(R.string.activity_in_group, item.roomName)
    val paidYou = stringResource(R.string.activity_paid_you)
    val paid = stringResource(R.string.activity_paid)
    val recipientFallback = stringResource(R.string.activity_recipient_fallback)
    val added = stringResource(R.string.activity_added, op.description)

    val bold = SpanStyle(fontWeight = FontWeight.SemiBold)
    val title = buildAnnotatedString {
        withStyle(bold) { append(op.donor.displayName) }
        if (op.isDebtRepayment) {
            val lender = op.recipients.firstOrNull()?.user
            val sum = money(op.sum, item.roomCurrency)
            if (lender != null && lender.id == myUserId) {
                append(" $paidYou ")
            } else {
                append(" $paid ")
                withStyle(bold) { append(lender?.displayName ?: recipientFallback) }
                append(" ")
            }
            withStyle(bold) { append(sum) }
            append(" $inGroup")
        } else {
            append(" $added $inGroup")
        }
    }
    Text(text = title, fontSize = 15.sp, color = Splitty.colors.ink)
}

/**
 * Вторая строка: ваша позиция — подпись вторичным цветом и сумма через
 * MoneyText (семантическая окраска) в валюте комнаты операции.
 * Позиция расхода — из ХРАНИМЫХ долей (Operation.netPosition), не пересчёт.
 */
@Composable
private fun ActivityPosition(item: ActivityItem, myUserId: Long?) {
    val op = item.operation
    val (label, amount, role) = when {
        myUserId == null ->
            Triple(stringResource(R.string.activity_not_involved), null, MoneyRole.NEUTRAL)

        op.isDebtRepayment -> when {
            op.donor.id == myUserId ->
                Triple(stringResource(R.string.activity_you_paid), op.sum, MoneyRole.NEGATIVE)

            op.recipients.any { it.user.id == myUserId } ->
                Triple(stringResource(R.string.activity_you_received), op.sum, MoneyRole.POSITIVE)

            else ->
                Triple(stringResource(R.string.activity_not_involved), null, MoneyRole.NEUTRAL)
        }

        else -> when (val net = op.netPosition(myUserId)) {
            null -> Triple(stringResource(R.string.activity_not_involved), null, MoneyRole.NEUTRAL)
            0 -> Triple(stringResource(R.string.activity_settled), null, MoneyRole.NEUTRAL)
            else -> if (net > 0) {
                Triple(stringResource(R.string.activity_you_lent), net, MoneyRole.POSITIVE)
            } else {
                Triple(stringResource(R.string.activity_you_owe), -net, MoneyRole.NEGATIVE)
            }
        }
    }

    Row(
        horizontalArrangement = Arrangement.spacedBy(5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            fontSize = 14.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.inkSecondary,
        )
        if (amount != null) {
            MoneyText(amount, role = role, size = 15.sp, currency = item.roomCurrency)
        }
    }
}

@Composable
private fun ActivityLoadingView() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Splitty.colors.accent)
    }
}

/** Ошибка первичной загрузки: иконка, текст и кнопка «Повторить». */
@Composable
private fun ActivityErrorView(message: String, onRetry: () -> Unit) {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        FailedState(message = message, onRetry = onRetry)
    }
}

@Composable
private fun ActivityEmptyView(
    isMineOnly: Boolean = false,
    onShowAll: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.Schedule,
            contentDescription = null,
            modifier = Modifier.size(44.dp),
            tint = Splitty.colors.inkSecondary,
        )
        Text(
            text = stringResource(
                if (isMineOnly) R.string.activity_mine_only_empty_title
                else R.string.activity_empty_title
            ),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = Splitty.colors.ink,
        )
        Text(
            text = stringResource(
                if (isMineOnly) R.string.activity_mine_only_empty_description
                else R.string.activity_empty_description
            ),
            modifier = Modifier.padding(horizontal = 24.dp),
            fontSize = 15.sp,
            color = Splitty.colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
        // При включённом фильтре — быстрый сброс к полной ленте.
        if (isMineOnly) {
            Spacer(Modifier.height(8.dp))
            PrimaryPillButton(
                text = stringResource(R.string.activity_show_all),
                onClick = onShowAll,
                modifier = Modifier.padding(horizontal = 48.dp),
            )
        }
    }
}
