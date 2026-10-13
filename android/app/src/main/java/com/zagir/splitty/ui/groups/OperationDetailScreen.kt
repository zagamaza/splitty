package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.ui.resolve
import com.zagir.splitty.core.ui.UiText
import android.content.Context
import android.content.Intent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Movie
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.FileProvider
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import androidx.compose.runtime.setValue
import androidx.compose.runtime.getValue
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.heading
import com.zagir.splitty.ui.components.OutlineActionButton
import com.zagir.splitty.ui.components.isLargeFontScale
import com.zagir.splitty.ui.components.ReceiptDivider
import com.zagir.splitty.ui.components.ReceiptLabel
import com.zagir.splitty.ui.components.ReceiptPaper
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationFile
import com.zagir.splitty.core.model.OperationRecipient
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
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
                message = current.message.resolve(),
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
                OperationDocument(
                    operation = current.value.operation,
                    members = current.value.members,
                    currency = current.value.currency,
                    meId = meId,
                    onOpenFile = { previewFile = it },
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

// MARK: - Документ

/**
 * Карточка операции — один лист: тип и дата реквизитом, название, сумма и кто
 * платил, доли, позиции чека и вложения разделами через пунктир (паритет iOS).
 */
@Composable
private fun OperationDocument(
    operation: Operation,
    members: List<User>,
    currency: String,
    meId: Long?,
    onOpenFile: (OperationFile) -> Unit,
) {
    ReceiptPaper {
        DocumentHeader(operation)
        SectionBreak()
        AmountBlock(operation, currency, meId)
        SectionBreak()
        RecipientsBlock(operation, currency, meId)
        // Позиции чека (itemized-операция AI) — только чтение, без своего листа.
        if (operation.itemList.isNotEmpty()) {
            SectionBreak()
            ReceiptCard(items = operation.itemList, members = members, currency = currency, embedded = true)
        }
        operation.files?.takeIf { it.isNotEmpty() }?.let { files ->
            SectionBreak()
            AttachmentsBlock(files = files, onOpen = onOpenFile)
        }
    }
}

@Composable
private fun SectionBreak() {
    ReceiptDivider(Modifier.padding(vertical = 16.dp))
}

/** Шапка: тип и дата реквизитом, название. Погашение — другой документ, видно с первой строки. */
@Composable
private fun DocumentHeader(operation: Operation) {
    val date = GroupsDateFmt.fullDate(operation.createdAt)
    val title = if (operation.isDebtRepayment) {
        stringResource(R.string.op_repayment_title)
    } else {
        operation.description.ifEmpty { stringResource(R.string.group_op_fallback) }
    }
    Column(
        modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) { heading() },
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        ReceiptLabel(
            stringResource(if (operation.isDebtRepayment) R.string.op_doc_repayment else R.string.op_doc_expense, date),
            textAlign = TextAlign.Center,
        )
        Text(
            text = title,
            fontSize = 24.sp,
            lineHeight = 28.sp,
            fontWeight = FontWeight.ExtraBold,
            letterSpacing = (-0.6).sp,
            color = Splitty.colors.ink,
            textAlign = TextAlign.Center,
        )
    }
}

/** Сумма крупно и строкой — кто платил: роль в подписи, имя без «заплатил(а)». */
@Composable
private fun AmountBlock(operation: Operation, currency: String, meId: Long?) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Column(Modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            ReceiptLabel(stringResource(R.string.op_amount))
            MoneyText(
                operation.sum,
                exactMinor = operation.exactMinor,
                role = MoneyRole.NEUTRAL,
                size = 40.sp,
                weight = FontWeight.ExtraBold,
                currency = currency,
                minScale = 0.5f,
            )
        }
        DocumentRow(
            title = stringResource(if (operation.isDebtRepayment) R.string.op_who_sent else R.string.op_who_paid),
            value = displayName(operation.donor, meId),
        )
    }
}

/** Строка документа: подпись слева, значение справа; на крупном шрифте — под подписью. */
@Composable
private fun DocumentRow(title: String, value: String) {
    val colors = Splitty.colors
    if (isLargeFontScale()) {
        Column(Modifier.fillMaxWidth().semantics(mergeDescendants = true) {}) {
            Text(text = title, fontSize = 15.sp, color = colors.inkSecondary)
            Text(text = value, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = colors.ink)
        }
    } else {
        Row(
            Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.Bottom,
        ) {
            Text(text = title, modifier = Modifier.weight(1f), fontSize = 15.sp, color = colors.inkSecondary)
            Text(text = value, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = colors.ink, textAlign = TextAlign.End)
        }
    }
}

@Composable
private fun displayName(user: User, meId: Long?): String =
    if (user.id == meId) stringResource(R.string.op_you) else user.displayName

/**
 * «Кто участвует»/«Кто получил» — получатели с ХРАНИМЫМИ долями
 * (recipients[].sum): при делении «по суммам» именно они, а не пересчёт поровну.
 */
@Composable
private fun RecipientsBlock(operation: Operation, currency: String, meId: Long?) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        val label: @Composable () -> Unit = {
            ReceiptLabel(
                stringResource(
                    if (operation.isDebtRepayment) R.string.op_who_received else R.string.op_who_participates
                )
            )
        }
        val split: @Composable () -> Unit = {
            if (!operation.isDebtRepayment) {
                Text(
                    text = stringResource(
                        if (operation.splitType == SplitType.BY_EXACT_AMOUNT) R.string.op_split_exact
                        else R.string.op_split_equally
                    ),
                    fontSize = 12.sp,
                    color = colors.inkSecondary,
                )
            }
        }
        val headerModifier = Modifier.padding(bottom = 4.dp).semantics(mergeDescendants = true) { heading() }
        if (isLargeFontScale()) {
            Column(headerModifier) { label(); split() }
        } else {
            Row(headerModifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.Bottom) {
                label(); split()
            }
        }
        operation.recipients.forEach { recipient ->
            RecipientRow(operation = operation, recipient = recipient, currency = currency, meId = meId)
        }
    }
}

/** «Вложения»: список типов (Фото/Видео/Документ) с открытием на просмотр. */
@Composable
private fun AttachmentsBlock(files: List<OperationFile>, onOpen: (OperationFile) -> Unit) {
    val colors = Splitty.colors
    Column {
        ReceiptLabel(stringResource(R.string.op_attachments), Modifier.semantics { heading() })
        files.forEach { file ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .defaultMinSize(minHeight = 48.dp)
                    .clickable { onOpen(file) },
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
                    fontSize = 16.sp,
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

/** Строковый ресурс имени типа вложения. */
private fun attachmentLabel(type: String): Int = when (attachmentKind(type)) {
    AttachmentKind.PHOTO -> R.string.attach_photo
    AttachmentKind.VIDEO -> R.string.attach_video
    AttachmentKind.DOCUMENT -> R.string.attach_document
    AttachmentKind.OTHER -> R.string.attach_other
}

/** Имя слева, доля справа; на крупном шрифте доля уходит под имя, имя не рвётся. */
@Composable
private fun RecipientRow(
    operation: Operation,
    recipient: OperationRecipient,
    currency: String,
    meId: Long?,
) {
    val colors = Splitty.colors
    val user = recipient.user
    // Роль словами, без «должен(на)»; у погашения подпись не нужна — раздел
    // и так называется «Кто получил».
    val caption = when {
        operation.isDebtRepayment -> null
        user.id == operation.donor.id ->
            stringResource(if (user.id == meId) R.string.op_share_yours else R.string.op_share)
        user.id == meId -> stringResource(R.string.op_your_debt)
        else -> stringResource(R.string.op_debt)
    }
    // Цвет доли: негатив — только для СВОЕГО долга; остальное нейтрально.
    val role = if (!operation.isDebtRepayment && user.id == meId && user.id != operation.donor.id) {
        MoneyRole.NEGATIVE
    } else {
        MoneyRole.NEUTRAL
    }
    val who: @Composable (Modifier) -> Unit = { m ->
        Column(m, verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(text = displayName(user, meId), fontSize = 16.sp, color = colors.ink)
            if (caption != null) Text(text = caption, fontSize = 12.sp, color = colors.inkSecondary)
        }
    }
    val amount: @Composable () -> Unit = {
        MoneyText(recipient.sum, exactMinor = recipient.exactMinor, role = role, size = 16.sp,
            weight = FontWeight.SemiBold, currency = currency)
    }
    val rowModifier = Modifier.fillMaxWidth().padding(vertical = 6.dp).semantics(mergeDescendants = true) {}
    if (isLargeFontScale()) {
        Column(rowModifier) { who(Modifier); amount() }
    } else {
        Row(rowModifier, horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
            who(Modifier.weight(1f))
            amount()
        }
    }
}

/** Под листом: «Изменить» рамкой (только расходы) и «Удалить» красной ссылкой — тише суммы. */
@Composable
private fun OperationActionsCard(
    canEdit: Boolean,
    isDeleting: Boolean,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
) {
    val colors = Splitty.colors
    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (canEdit) {
            OutlineActionButton(text = stringResource(R.string.op_edit), onClick = onEdit)
        }
        Row(
            modifier = Modifier
                .defaultMinSize(minHeight = 48.dp)
                .clickable(enabled = !isDeleting, role = Role.Button, onClick = onDelete)
                .padding(horizontal = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (isDeleting) {
                CircularProgressIndicator(
                    color = colors.negative,
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                )
            }
            Text(
                text = stringResource(if (isDeleting) R.string.op_deleting else R.string.op_delete),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.negative,
                textDecoration = TextDecoration.Underline,
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
                            Text(stringResource(R.string.common_done), color = colors.ink)
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
                    AttachmentPreview.Loading -> CircularProgressIndicator(color = colors.ink)

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
