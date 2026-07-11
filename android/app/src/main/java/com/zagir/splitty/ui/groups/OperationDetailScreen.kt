package com.zagir.splitty.ui.groups

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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Payments
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationRecipient
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Карточка операции: hero (описание, дата, крупная сумма), «Детали» — донор
 * и получатели с ХРАНИМЫМИ долями, «Изменить»/«Удалить» для донора.
 * Порт iOS OperationDetailView (без просмотра вложений).
 *
 * [onEdit] — открыть форму расхода в режиме редактирования (roomId, operationId).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OperationDetailScreen(
    roomId: String,
    operationId: String,
    onBack: () -> Unit,
    onEdit: (String, String) -> Unit,
    viewModel: OperationDetailViewModel = hiltViewModel(),
) {
    LaunchedEffect(roomId, operationId) { viewModel.start(roomId, operationId) }

    val state by viewModel.state.collectAsStateWithLifecycle()
    val meIdState by viewModel.meId.collectAsStateWithLifecycle()
    val isDeleting by viewModel.isDeleting.collectAsStateWithLifecycle()
    val alertMessage by viewModel.alertMessage.collectAsStateWithLifecycle()
    var isDeleteConfirmPresented by remember { mutableStateOf(false) }

    val colors = Splitty.colors
    val card = (state as? UiState.Content)?.value
    val meId = meIdState

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(
                            if (card?.operation?.isDebtRepayment == true) R.string.op_title_repayment
                            else R.string.op_title_expense
                        ),
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
                onRetry = viewModel::retry,
                modifier = Modifier.padding(innerPadding),
            )

            is UiState.Content -> Column(
                modifier = Modifier
                    .padding(innerPadding)
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                OperationHeroCard(
                    operation = current.value.operation,
                    currency = current.value.currency,
                )
                OperationDetailsSection(
                    operation = current.value.operation,
                    currency = current.value.currency,
                    meId = meId,
                )
                // Редактировать/удалять может любой участник комнаты
                // (Splitwise-семантика, сервер разрешает участникам).
                if (meId != null) {
                    OperationActionsCard(
                        canEdit = !current.value.operation.isDebtRepayment,
                        isDeleting = isDeleting,
                        onEdit = { onEdit(roomId, operationId) },
                        onDelete = { isDeleteConfirmPresented = true },
                    )
                }
            }
        }
    }

    if (isDeleteConfirmPresented && card != null) {
        AlertDialog(
            onDismissRequest = { isDeleteConfirmPresented = false },
            title = {
                Text(
                    stringResource(
                        if (card.operation.isDebtRepayment) R.string.op_delete_repayment_title
                        else R.string.op_delete_expense_title
                    )
                )
            },
            text = { Text(stringResource(R.string.op_delete_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        isDeleteConfirmPresented = false
                        viewModel.delete(onDeleted = onBack)
                    },
                ) {
                    Text(stringResource(R.string.op_delete), color = colors.negative)
                }
            },
            dismissButton = {
                TextButton(onClick = { isDeleteConfirmPresented = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }
    GroupsAlertDialog(alertMessage, viewModel::dismissAlert)
}

// MARK: - Секции

/** Hero-карточка: иконка, описание, дата добавления, крупная сумма. */
@Composable
private fun OperationHeroCard(operation: Operation, currency: String) {
    val colors = Splitty.colors
    val title = if (operation.isDebtRepayment) {
        stringResource(R.string.op_repayment_title)
    } else {
        operation.description.ifEmpty { stringResource(R.string.group_op_fallback) }
    }
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 20.dp) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(
                        if (operation.isDebtRepayment) colors.accent.copy(alpha = 0.14f)
                        else colors.ink.copy(alpha = 0.06f)
                    ),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = if (operation.isDebtRepayment) Icons.Outlined.Payments
                    else Icons.Outlined.Description,
                    contentDescription = null,
                    tint = if (operation.isDebtRepayment) colors.accent else colors.inkSecondary,
                    modifier = Modifier.size(22.dp),
                )
            }
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    text = title,
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = colors.ink,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = stringResource(
                        R.string.op_added_date,
                        GroupsDateFmt.fullDate(operation.createdAt),
                    ),
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                )
            }
        }
        Spacer(Modifier.height(14.dp))
        MoneyText(operation.sum, role = MoneyRole.NEUTRAL, size = 40.sp, currency = currency)
    }
}

/**
 * «Детали»: плательщик и получатели с аватарами и долями. Доли — ХРАНИМЫЕ
 * суммы операции (recipients[].sum): при делении «по суммам» именно они,
 * а не пересчёт поровну.
 */
@Composable
private fun OperationDetailsSection(operation: Operation, currency: String, meId: Long?) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            modifier = Modifier.padding(start = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionHeader(stringResource(R.string.op_details))
            if (!operation.isDebtRepayment) {
                Text(
                    text = stringResource(
                        if (operation.splitType == SplitType.BY_EXACT_AMOUNT) R.string.op_split_exact
                        else R.string.op_split_equally
                    ),
                    fontSize = 12.sp,
                    color = colors.inkSecondary.copy(alpha = 0.7f),
                )
            }
        }
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            DonorRow(operation = operation, currency = currency, meId = meId)
            operation.recipients.forEach { recipient ->
                HairlineDivider(startIndent = 64.dp)
                RecipientRow(
                    operation = operation,
                    recipient = recipient,
                    currency = currency,
                    meId = meId,
                )
            }
        }
    }
}

@Composable
private fun DonorRow(operation: Operation, currency: String, meId: Long?) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        GradientAvatar(user = operation.donor, size = 36.dp)
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = if (operation.donor.id == meId) stringResource(R.string.op_you)
                else operation.donor.displayName,
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = stringResource(
                    if (operation.donor.id == meId) R.string.op_paid_you else R.string.op_paid
                ),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
        MoneyText(operation.sum, role = MoneyRole.NEUTRAL, size = 15.sp, currency = currency)
    }
}

@Composable
private fun RecipientRow(
    operation: Operation,
    recipient: OperationRecipient,
    currency: String,
    meId: Long?,
) {
    val colors = Splitty.colors
    val user = recipient.user
    val caption = when {
        operation.isDebtRepayment ->
            stringResource(if (user.id == meId) R.string.op_received_you else R.string.op_received)

        user.id == operation.donor.id ->
            stringResource(if (user.id == meId) R.string.op_share_yours else R.string.op_share)

        user.id == meId -> stringResource(R.string.op_you_owe)

        else -> stringResource(R.string.op_owes)
    }
    // Цвет доли: негатив — только для СВОЕГО долга; остальное нейтрально.
    val role = if (!operation.isDebtRepayment && user.id == meId && user.id != operation.donor.id) {
        MoneyRole.NEGATIVE
    } else {
        MoneyRole.NEUTRAL
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        GradientAvatar(
            user = user,
            size = 32.dp,
            modifier = Modifier.padding(start = 4.dp),
        )
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = if (user.id == meId) stringResource(R.string.op_you) else user.displayName,
                fontSize = 15.sp,
                color = colors.ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = caption,
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
        MoneyText(recipient.sum, role = role, size = 15.sp, currency = currency)
    }
}

/** «Изменить» (только расходы) и «Удалить» — карточка действий донора. */
@Composable
private fun OperationActionsCard(
    canEdit: Boolean,
    isDeleting: Boolean,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        if (canEdit) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(onClick = onEdit)
                    .padding(horizontal = 16.dp, vertical = 14.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = Icons.Outlined.Edit,
                    contentDescription = null,
                    tint = colors.accent,
                    modifier = Modifier.size(18.dp),
                )
                Text(
                    text = stringResource(R.string.op_edit),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.accent,
                )
            }
            HairlineDivider(startIndent = 16.dp)
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(enabled = !isDeleting, onClick = onDelete)
                .padding(horizontal = 16.dp, vertical = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (isDeleting) {
                CircularProgressIndicator(
                    color = colors.negative,
                    modifier = Modifier.size(18.dp),
                    strokeWidth = 2.dp,
                )
            } else {
                Icon(
                    imageVector = Icons.Outlined.Delete,
                    contentDescription = null,
                    tint = colors.negative,
                    modifier = Modifier.size(18.dp),
                )
            }
            Text(
                text = stringResource(if (isDeleting) R.string.op_deleting else R.string.op_delete),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.negative,
            )
        }
    }
}
