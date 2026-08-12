package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.ui.resolve
import com.zagir.splitty.core.ui.UiText
import android.content.Context
import android.content.Intent
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
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Movie
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.FileProvider
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationFile
import com.zagir.splitty.core.model.OperationRecipient
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.components.ZoomableImage
import com.zagir.splitty.ui.components.decodeDownsampled
import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.ui.expense.ReceiptCard
import com.zagir.splitty.ui.theme.Splitty
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Карточка операции: hero (описание, дата, крупная сумма), «Кто платил»/«Кто
 * участвует» с ХРАНИМЫМИ долями, «Позиции чека» (read-only ReceiptCard),
 * «Вложения» (просмотр фото с зумом), «Изменить»/«Удалить».
 * Порт iOS OperationDetailView.
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
    val haptics = rememberHaptics()
    val isDeleting by viewModel.isDeleting.collectAsStateWithLifecycle()
    val alertMessage by viewModel.alertMessage.collectAsStateWithLifecycle()
    var isDeleteConfirmPresented by remember { mutableStateOf(false) }
    // Вложение, открытое на просмотр (полноэкранный диалог).
    var previewFile by remember { mutableStateOf<OperationFile?>(null) }

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
                PayerSection(
                    operation = current.value.operation,
                    currency = current.value.currency,
                    meId = meId,
                )
                RecipientsSection(
                    operation = current.value.operation,
                    currency = current.value.currency,
                    meId = meId,
                )
                // Позиции чека (itemized-операция AI) — только чтение.
                if (current.value.operation.itemList.isNotEmpty()) {
                    ItemsSection(
                        operation = current.value.operation,
                        members = current.value.members,
                        currency = current.value.currency,
                    )
                }
                current.value.operation.files
                    ?.takeIf { it.isNotEmpty() }
                    ?.let { files ->
                        AttachmentsSection(files = files, onOpen = { previewFile = it })
                    }
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
            text = {
                Text(
                    stringResource(
                        if (card.operation.isDebtRepayment) R.string.op_delete_repayment_message
                        else R.string.op_delete_message
                    )
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        isDeleteConfirmPresented = false
                        // Успех удаления — тактильное подтверждение до закрытия
                        // экрана (порт iOS OperationDetailView Haptics.success).
                        viewModel.delete(
                            onDeleted = {
                                haptics.success()
                                onBack()
                            },
                        )
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
    previewFile?.let { file ->
        AttachmentPreviewDialog(
            file = file,
            loadBytes = viewModel::fileData,
            onDismiss = { previewFile = null },
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
 * «Кто платил»/«Кто отправил» — донор с полной суммой. Отдельная секция от
 * получателей (как iOS): плательщик визуально не смешивается с долгами.
 */
@Composable
private fun PayerSection(operation: Operation, currency: String, meId: Long?) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            stringResource(
                if (operation.isDebtRepayment) R.string.op_who_sent else R.string.op_who_paid
            ),
            modifier = Modifier.padding(start = 4.dp),
        )
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            DonorRow(operation = operation, currency = currency, meId = meId)
        }
    }
}

/**
 * «Кто участвует»/«Кто получил» — получатели с ХРАНИМЫМИ долями
 * (recipients[].sum): при делении «по суммам» именно они, а не пересчёт поровну.
 */
@Composable
private fun RecipientsSection(operation: Operation, currency: String, meId: Long?) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            modifier = Modifier.padding(start = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionHeader(
                stringResource(
                    if (operation.isDebtRepayment) R.string.op_who_received
                    else R.string.op_who_participates
                )
            )
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
            operation.recipients.forEachIndexed { index, recipient ->
                if (index != 0) HairlineDivider(startIndent = 64.dp)
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

/** «Позиции чека»: read-only ReceiptCard с участниками комнаты. */
@Composable
private fun ItemsSection(operation: Operation, members: List<User>, currency: String) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        SectionHeader(
            stringResource(R.string.op_items),
            modifier = Modifier.padding(start = 4.dp),
        )
        // Колбэки не передаём — карточка полностью read-only.
        ReceiptCard(items = operation.itemList, members = members, currency = currency)
    }
}

/** «Вложения»: список типов (Фото/Видео/Документ) с открытием на просмотр. */
@Composable
private fun AttachmentsSection(files: List<OperationFile>, onOpen: (OperationFile) -> Unit) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            stringResource(R.string.op_attachments),
            modifier = Modifier.padding(start = 4.dp),
        )
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            files.forEachIndexed { index, file ->
                if (index != 0) HairlineDivider(startIndent = 16.dp)
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { onOpen(file) }
                        .padding(horizontal = 16.dp, vertical = 14.dp),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(
                        imageVector = Icons.Outlined.AttachFile,
                        contentDescription = null,
                        tint = colors.inkSecondary,
                        modifier = Modifier.size(18.dp),
                    )
                    Text(
                        text = stringResource(attachmentLabel(file.type)),
                        fontSize = 15.sp,
                        color = colors.ink,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                        contentDescription = null,
                        tint = colors.inkSecondary.copy(alpha = 0.6f),
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
    }
}

/** Строковый ресурс имени типа вложения. */
private fun attachmentLabel(type: String): Int = when (attachmentKind(type)) {
    AttachmentKind.PHOTO -> R.string.attach_photo
    AttachmentKind.VIDEO -> R.string.attach_video
    AttachmentKind.DOCUMENT -> R.string.attach_document
    AttachmentKind.OTHER -> R.string.attach_other
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
                    color = colors.accentText,
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

// MARK: - Просмотр вложения

/** Состояние загрузки вложения на просмотр. */
private sealed interface AttachmentPreview {
    data object Loading : AttachmentPreview
    data class Photo(val bitmap: ImageBitmap) : AttachmentPreview
    /** Не-фото (видео/документ): временный файл под системный «Поделиться». */
    data class Shareable(val file: java.io.File) : AttachmentPreview
    data class Failed(val message: UiText) : AttachmentPreview
}

/**
 * Полноэкранный просмотр вложения — порт iOS OperationFileView. Скачивает
 * байты через VM (auth-заголовок), фото показывает зумируемым ZoomableImage,
 * прочее — иконкой с кнопкой «Открыть / поделиться» (временный файл + чузер).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AttachmentPreviewDialog(
    file: OperationFile,
    loadBytes: suspend (String) -> ByteArray,
    onDismiss: () -> Unit,
) {
    val colors = Splitty.colors
    val context = LocalContext.current
    var reloadKey by remember { mutableIntStateOf(0) }
    var preview by remember { mutableStateOf<AttachmentPreview>(AttachmentPreview.Loading) }

    LaunchedEffect(file.fileId, reloadKey) {
        preview = AttachmentPreview.Loading
        preview = try {
            val bytes = loadBytes(file.fileId)
            // Пробуем как картинку независимо от типа: сервер иногда шлёт фото
            // с type=«photo», а не «image». Не декодировалось — значит не фото.
            val bitmap = withContext(Dispatchers.Default) { decodeDownsampled(bytes) }
            if (bitmap != null) {
                AttachmentPreview.Photo(bitmap)
            } else {
                AttachmentPreview.Shareable(withContext(Dispatchers.IO) { cacheAttachment(context, file, bytes) })
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AttachmentPreview.Failed(humanErrorText(e))
        }
    }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Scaffold(
            containerColor = colors.bg,
            topBar = {
                TopAppBar(
                    title = {
                        Text(stringResource(attachmentLabel(file.type)), fontWeight = FontWeight.Bold)
                    },
                    actions = {
                        TextButton(onClick = onDismiss) {
                            Text(stringResource(R.string.common_done), color = colors.accent)
                        }
                    },
                    colors = TopAppBarDefaults.topAppBarColors(
                        containerColor = colors.bg,
                        titleContentColor = colors.ink,
                    ),
                )
            },
        ) { innerPadding ->
            Box(
                modifier = Modifier
                    .padding(innerPadding)
                    .fillMaxSize(),
                contentAlignment = Alignment.Center,
            ) {
                when (val p = preview) {
                    AttachmentPreview.Loading -> CircularProgressIndicator(color = colors.accent)

                    is AttachmentPreview.Photo -> ZoomableImage(
                        bitmap = p.bitmap,
                        contentDescription = stringResource(attachmentLabel(file.type)),
                    )

                    is AttachmentPreview.Shareable -> ShareFallback(file = file, tempFile = p.file)

                    is AttachmentPreview.Failed -> FailedState(
                        message = p.message.resolve(),
                        onRetry = { reloadKey++ },
                    )
                }
            }
        }
    }
}

/** Иконка типа + кнопка системного «Поделиться» для не-фото вложений. */
@Composable
private fun ShareFallback(file: OperationFile, tempFile: java.io.File) {
    val colors = Splitty.colors
    val context = LocalContext.current
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(24.dp),
    ) {
        Icon(
            imageVector = if (attachmentKind(file.type) == AttachmentKind.VIDEO) {
                Icons.Outlined.Movie
            } else {
                Icons.Outlined.Description
            },
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.size(44.dp),
        )
        PrimaryPillButton(
            text = stringResource(R.string.attach_open_share),
            onClick = { shareFile(context, tempFile) },
            modifier = Modifier.padding(horizontal = 40.dp),
        )
    }
}

/** Пишет байты во временный файл cacheDir/attachments для FileProvider-шаринга. */
private fun cacheAttachment(context: Context, file: OperationFile, bytes: ByteArray): java.io.File {
    val dir = java.io.File(context.cacheDir, "attachments").apply { mkdirs() }
    val ext = when (attachmentKind(file.type)) {
        AttachmentKind.VIDEO -> "mp4"
        else -> "dat"
    }
    return java.io.File(dir, "${file.fileId}.$ext").apply { writeBytes(bytes) }
}

/** Открывает системный чузер «Поделиться» по content:// URI из FileProvider. */
private fun shareFile(context: Context, file: java.io.File) {
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
    val intent = Intent(Intent.ACTION_SEND).apply {
        type = context.contentResolver.getType(uri) ?: "application/octet-stream"
        putExtra(Intent.EXTRA_STREAM, uri)
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    }
    context.startActivity(Intent.createChooser(intent, null))
}
