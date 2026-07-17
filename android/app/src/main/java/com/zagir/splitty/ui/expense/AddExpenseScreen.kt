package com.zagir.splitty.ui.expense

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AddCircle
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Description
import androidx.compose.material.icons.filled.GraphicEq
import androidx.compose.material.icons.filled.PhotoCamera
import androidx.compose.material.icons.filled.PhotoLibrary
import androidx.compose.material.icons.filled.WarningAmber
import androidx.compose.material.icons.filled.WifiOff
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.money.currencySymbol
import com.zagir.splitty.core.money.money
import com.zagir.splitty.core.money.moneyRange
import com.zagir.splitty.core.money.shares
import com.zagir.splitty.ui.components.AppToast
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.components.nudgeHighlight
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.theme.Splitty

/**
 * Полноэкранная форма добавления/редактирования расхода (порт iOS
 * AddExpenseView): выбор группы чипами (когда [roomId] == null — открыто
 * с центральной «+»), карточка «описание + крупная сумма», карточка деления
 * «Поровну | По суммам», CTA «Сохранить» внизу.
 *
 * Офлайн: создание уходит в outbox; правка локальной записи ([localId])
 * доступна всегда, правка серверной операции без сети блокируется плашкой.
 *
 * @param roomId фиксированная группа; null — сверху появляется выбор группы.
 * @param operationId id редактируемой операции (prefill формы, PUT вместо
 *   POST); учитывается только вместе с [roomId].
 * @param localId localId неотправленной записи outbox — правка локальной
 *   операции (учитывается только вместе с [roomId], приоритетнее [operationId]).
 * @param onDone закрыть экран — зовётся после успешного сохранения и по «Отмена».
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddExpenseScreen(
    roomId: String?,
    operationId: String?,
    localId: String? = null,
    viewModel: AddExpenseViewModel = hiltViewModel(),
    onDone: () -> Unit,
) {
    LaunchedEffect(roomId, operationId, localId) { viewModel.start(roomId, operationId, localId) }
    val state by viewModel.state.collectAsStateWithLifecycle()
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()
    val snapshot = state
    val form = if (snapshot is UiState.Content) snapshot.value else null
    val haptics = rememberHaptics()

    // Фото чека → путь к готовому JPEG в cacheDir → распознавание (Task 7).
    val receiptCapture = rememberReceiptCapture { path -> viewModel.parseReceiptImage(path) }

    // Встряска поля группы при нудже (тап по «Сохранить» без выбранной группы).
    var groupNudge by remember { mutableIntStateOf(0) }
    // Открытый шит правки позиции чека (индекс в draftItems) / сопоставления имени.
    var itemEditIndex by remember { mutableStateOf<Int?>(null) }
    var unknownTarget by remember { mutableStateOf<UnknownTarget?>(null) }

    LaunchedEffect(form?.isSaved) {
        if (form?.isSaved == true) {
            haptics.success()
            onDone()
        }
    }

    val colors = Splitty.colors
    Scaffold(
        containerColor = colors.bg,
        topBar = {
            CenterAlignedTopAppBar(
                title = {
                    Text(
                        text = stringResource(
                            if ((operationId != null || localId != null) && roomId != null) {
                                R.string.expense_title_edit
                            } else {
                                R.string.expense_title_add
                            }
                        ),
                        fontSize = 17.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                },
                navigationIcon = {
                    TextButton(onClick = onDone) {
                        Text(
                            text = stringResource(R.string.common_cancel),
                            fontSize = 17.sp,
                            color = colors.accent,
                        )
                    }
                },
                colors = TopAppBarDefaults.centerAlignedTopAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                ),
            )
        },
        bottomBar = {
            if (form != null) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(colors.bg)
                        .navigationBarsPadding()
                        .imePadding()
                        .padding(horizontal = 20.dp, vertical = 8.dp),
                ) {
                    // Кнопка живая: тап при блокировке объясняет причину тостом
                    // (нет группы → встряска поля + тост), а не молчит. Жёсткий
                    // disabled — только сохранение в полёте и офлайн-правка синка.
                    PrimaryPillButton(
                        text = stringResource(R.string.common_save),
                        onClick = onSave@{
                            when {
                                form.selectedRoomId == null -> {
                                    haptics.warning()
                                    groupNudge++
                                    viewModel.nudgeSelectGroup()
                                }
                                form.saveBlockedReason != null -> {
                                    haptics.warning()
                                    viewModel.showToast(form.saveBlockedReason!!)
                                }
                                else -> viewModel.save()
                            }
                        },
                        enabled = !form.isSaving &&
                            canSaveExpenseOffline(form.isEditingSynced, isOnline),
                    )
                }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.fillMaxSize()) {
            when (val current = state) {
                is UiState.Loading -> Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(innerPadding),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(color = colors.accent)
                }

                is UiState.Error -> LoadErrorPane(
                    message = current.message,
                    onRetry = viewModel::retry,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(innerPadding),
                )

                is UiState.Content -> ExpenseFormContent(
                    form = current.value,
                    isOnline = isOnline,
                    viewModel = viewModel,
                    groupNudge = groupNudge,
                    onTakePhoto = receiptCapture::captureFromCamera,
                    onPickPhoto = receiptCapture::pickFromGallery,
                    onEditItem = { index -> itemEditIndex = index },
                    onResolveUnknown = { index, name -> unknownTarget = UnknownTarget(index, name) },
                    onAddItem = { itemEditIndex = viewModel.addBlankItem() },
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(innerPadding),
                )
            }

            // Оверлей распознавания: спиннер, «Отмена» — через 2.5с (см. iOS).
            if (form?.isParsing == true) {
                ParsingOverlay(
                    onCancel = viewModel::cancelParse,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(innerPadding),
                )
            }

            // Тост-подтверждение/причина блокировки — поверх содержимого, снизу.
            AppToast(
                message = form?.toastMessage,
                onDismiss = viewModel::dismissToast,
                modifier = Modifier.padding(innerPadding),
            )
        }
    }

    // Шит правки позиции чека (индекс мог устареть после удаления — сверяем с формой).
    val editIndex = itemEditIndex
    val editItem = editIndex?.let { form?.draftItems?.getOrNull(it) }
    if (form != null && editIndex != null && editItem != null) {
        ItemSheet(
            item = editItem,
            members = form.members,
            currency = form.currency,
            meId = form.meId,
            onCommit = { viewModel.replaceItem(editIndex, it) },
            onDelete = { viewModel.deleteItem(editIndex) },
            onDismiss = { itemEditIndex = null },
        )
    }

    val unknown = unknownTarget
    if (form != null && unknown != null) {
        UnknownPickerSheet(
            name = unknown.name,
            members = form.members,
            onPick = { userId -> viewModel.resolveUnknown(unknown.itemIndex, unknown.name, userId) },
            onDismiss = { unknownTarget = null },
        )
    }

    form?.alertMessage?.let { message ->
        AlertDialog(
            onDismissRequest = viewModel::dismissAlert,
            confirmButton = {
                TextButton(onClick = viewModel::dismissAlert) {
                    Text(stringResource(R.string.common_ok))
                }
            },
            title = { Text(stringResource(R.string.common_error_title)) },
            text = { Text(message) },
        )
    }
}

/** Цель сопоставления нераспознанного имени: позиция и само имя. */
private data class UnknownTarget(val itemIndex: Int, val name: String)

// MARK: Содержимое формы

@Composable
private fun ExpenseFormContent(
    form: AddExpenseForm,
    isOnline: Boolean,
    viewModel: AddExpenseViewModel,
    groupNudge: Int,
    onTakePhoto: () -> Unit,
    onPickPhoto: () -> Unit,
    onEditItem: (Int) -> Unit,
    onResolveUnknown: (Int, String) -> Unit,
    onAddItem: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var confirmDeleteLocal by remember { mutableStateOf(false) }
    Column(
        modifier = modifier
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(horizontal = 20.dp)
            .padding(top = 16.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        // Правка серверной операции офлайн недоступна (очереди update в v1 нет).
        if (form.isEditingSynced && !isOnline) {
            OfflineEditBlockedPlate()
        }
        // Ошибка распознавания: черновик не потерян, предлагаем «Повторить».
        form.parseRetryMessage?.let { message ->
            ParseRetryBanner(
                message = message,
                onRetry = viewModel::retryParse,
                onDismiss = viewModel::dismissParseRetry,
            )
        }
        if (form.showsRoomPicker) {
            GroupPickerCard(form = form, groupNudge = groupNudge, onSelect = viewModel::selectRoom)
        }
        // Распознавание по фото чека — только при создании плоского расхода
        // (при уже распознанном чеке фото добавляется из баннера «Распознано»).
        if (!form.isEditing && form.selectedRoomId != null && !form.hasDraftItems && !form.didRecognize) {
            ReceiptScanCard(onTakePhoto = onTakePhoto, onPickPhoto = onPickPhoto)
        }
        // Баннер результата голосовой/фото-правки с «Отменить»; либо плашка
        // «Распознано голосом» для плоского AI-результата (без позиций).
        if (form.canUndoParse) {
            CorrectionBanner(hasItems = form.hasDraftItems, onUndo = viewModel::undoParse)
        } else if (form.didRecognize && !form.hasDraftItems) {
            RecognizedBanner(onAddPhoto = onTakePhoto)
        }
        ExpenseCard(form = form, viewModel = viewModel)
        if (form.hasDraftItems) {
            ReceiptSection(
                form = form,
                viewModel = viewModel,
                onEditItem = onEditItem,
                onResolveUnknown = onResolveUnknown,
                onAddItem = onAddItem,
            )
        } else {
            ParseQuestionLabels(form.parseQuestions)
            SplitCard(form = form, viewModel = viewModel)
        }
        // Неотправленную запись можно удалить прямо из формы правки.
        if (form.isEditingLocal) {
            DeleteLocalCard(enabled = !form.isSaving, onDelete = { confirmDeleteLocal = true })
        }
    }

    if (confirmDeleteLocal) {
        val colors = Splitty.colors
        AlertDialog(
            onDismissRequest = { confirmDeleteLocal = false },
            title = { Text(stringResource(R.string.expense_delete_local_title)) },
            text = { Text(stringResource(R.string.expense_delete_local_message)) },
            confirmButton = {
                TextButton(onClick = {
                    confirmDeleteLocal = false
                    viewModel.deleteLocal()
                }) {
                    Text(stringResource(R.string.op_delete), color = colors.negative)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDeleteLocal = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }
}

/**
 * Секция распознанного чека: интерактивная карточка-чек (тап по строке → шит),
 * подсказки по нераспознанным именам/ценам, разбивка «С кого сколько»,
 * «+ Добавить позицию» и карточка переопределения деления «Поровну на всех».
 */
@Composable
private fun ReceiptSection(
    form: AddExpenseForm,
    viewModel: AddExpenseViewModel,
    onEditItem: (Int) -> Unit,
    onResolveUnknown: (Int, String) -> Unit,
    onAddItem: () -> Unit,
) {
    val colors = Splitty.colors
    // Подсветка правки — вспышка: гаснет сама через 2.5с.
    LaunchedEffect(form.changedItemIndices) {
        if (form.changedItemIndices.isNotEmpty()) {
            delay(2500)
            viewModel.clearChangeHighlights()
        }
    }
    Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
        ReceiptCard(
            items = form.draftItems,
            members = form.members,
            currency = form.currency,
            onEditItem = onEditItem,
            onResolveUnknown = onResolveUnknown,
            onToggleSurchargeRule = viewModel::toggleSurchargeRule,
            highlightedIndices = form.changedItemIndices,
        )
        if (form.hasUnknownItems) {
            form.firstUnknownName?.let { name ->
                ReceiptHintRow(stringResource(R.string.expense_unknown_hint, name), warn = true)
            }
        }
        if (form.hasPricelessItems) {
            ReceiptHintRow(stringResource(R.string.expense_priceless_hint), warn = true)
        }
        ParseQuestionLabels(form.parseQuestions)
        // При неопределённых ценах разбивку по людям не показываем (частичные
        // суммы вводят в заблуждение).
        if (!form.hasPricelessItems) {
            form.personShares?.takeIf { it.isNotEmpty() }?.let { shares ->
                PersonBreakdownCard(
                    shares = shares,
                    members = form.members,
                    currency = form.currency,
                    meId = form.meId,
                )
            }
        }
        // AI мог пропустить блюдо — путь добавить руками, не передиктовывая.
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onAddItem)
                .padding(vertical = 6.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Filled.AddCircle,
                contentDescription = null,
                tint = colors.accent,
                modifier = Modifier.size(20.dp),
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = stringResource(R.string.expense_add_item),
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.accent,
            )
        }
        SplitOverrideCard(onCollapse = viewModel::collapseToEqualSplit)
    }
}

/** Строка-подсказка под чеком (нераспознанное имя / отсутствие цены). */
@Composable
private fun ReceiptHintRow(text: String, warn: Boolean) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Filled.WarningAmber,
            contentDescription = null,
            tint = if (warn) colors.negative else colors.inkSecondary,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = text,
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = if (warn) colors.negative else colors.inkSecondary,
        )
    }
}

/** Уточняющие вопросы модели («Сколько стоила пицца?») — видимы под формой. */
@Composable
private fun ParseQuestionLabels(questions: List<String>) {
    if (questions.isEmpty()) return
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        questions.forEach { question ->
            Text(
                text = "• $question",
                modifier = Modifier.fillMaxWidth(),
                fontSize = 13.sp,
                fontWeight = FontWeight.Medium,
                color = colors.inkSecondary,
            )
        }
    }
}

/** Инлайн-баннер результата правки: статус + «Отменить» (гаснет сам через 6с). */
@Composable
private fun CorrectionBanner(hasItems: Boolean, onUndo: () -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(colors.accent.copy(alpha = 0.1f))
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.AutoAwesome,
            contentDescription = null,
            tint = colors.accent,
            modifier = Modifier.size(18.dp),
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.expense_correction_title),
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
            )
            Text(
                text = stringResource(
                    if (hasItems) R.string.expense_correction_hint_items else R.string.expense_correction_hint_flat
                ),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
        SoftChip(text = stringResource(R.string.expense_undo), onClick = onUndo)
    }
}

/** Плашка «Распознано голосом» для плоского AI-результата: справа — «+ фото чека». */
@Composable
private fun RecognizedBanner(onAddPhoto: () -> Unit) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(colors.accent.copy(alpha = 0.1f))
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = Icons.Filled.GraphicEq,
            contentDescription = null,
            tint = colors.accent,
            modifier = Modifier.size(18.dp),
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.expense_recognized_title),
                fontSize = 14.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
            )
            Text(
                text = stringResource(R.string.expense_recognized_hint),
                fontSize = 12.sp,
                color = colors.inkSecondary,
            )
        }
        Box(
            modifier = Modifier
                .size(38.dp)
                .clip(CircleShape)
                .background(colors.accent.copy(alpha = 0.12f))
                .clickable(onClick = onAddPhoto),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = Icons.Filled.PhotoCamera,
                contentDescription = stringResource(R.string.expense_recognized_add_photo),
                tint = colors.accent,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

/** Карточка переопределения деления: «Поровну на всех» сбрасывает позиции. */
@Composable
private fun SplitOverrideCard(onCollapse: () -> Unit) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SectionHeader(
                text = stringResource(R.string.expense_split_override_title),
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            SoftChip(text = stringResource(R.string.expense_split_override_action), onClick = onCollapse)
        }
        Spacer(Modifier.height(8.dp))
        Text(
            text = stringResource(R.string.expense_split_override_hint),
            fontSize = 12.sp,
            color = colors.inkSecondary,
        )
    }
}

/** Плашка «Нет соединения…» — правка синхронизированной операции офлайн. */
@Composable
private fun OfflineEditBlockedPlate() {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 12.dp) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(
                imageVector = Icons.Filled.WifiOff,
                contentDescription = null,
                tint = colors.inkSecondary,
                modifier = Modifier.size(18.dp),
            )
            Text(
                text = stringResource(R.string.expense_offline_edit_blocked),
                fontSize = 13.sp,
                color = colors.inkSecondary,
            )
        }
    }
}

/** Карточка-кнопка «Удалить» неотправленной записи outbox. */
@Composable
private fun DeleteLocalCard(enabled: Boolean, onDelete: () -> Unit) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        Text(
            text = stringResource(R.string.op_delete),
            modifier = Modifier
                .fillMaxWidth()
                .clickable(enabled = enabled, onClick = onDelete)
                .padding(14.dp),
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.negative,
            textAlign = TextAlign.Center,
        )
    }
}

/** Выбор группы чипами (экран открыт с центральной «+»); встряхивается на нудже. */
@Composable
private fun GroupPickerCard(
    form: AddExpenseForm,
    groupNudge: Int,
    onSelect: (RoomSummary) -> Unit,
) {
    val listState = rememberLazyListState()
    // Автоскролл к выбранной группе: при 6+ группах активный чип мог оказаться
    // за экраном — непонятно, выбрано ли что-то.
    LaunchedEffect(form.selectedRoomId) {
        val index = form.rooms.indexOfFirst { it.id == form.selectedRoomId }
        if (index >= 0) listState.animateScrollToItem(index)
    }
    SurfaceCard(modifier = Modifier.fillMaxWidth().nudgeHighlight(groupNudge)) {
        SectionHeader(stringResource(R.string.expense_group_section))
        Spacer(Modifier.height(12.dp))
        if (form.rooms.isEmpty()) {
            Text(
                text = stringResource(R.string.expense_no_groups),
                fontSize = 15.sp,
                color = Splitty.colors.inkSecondary,
            )
        } else {
            LazyRow(
                state = listState,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(form.rooms, key = { it.id }) { room ->
                    SoftChip(
                        text = room.name,
                        onClick = { onSelect(room) },
                        isSelected = room.id == form.selectedRoomId,
                    )
                }
            }
        }
    }
}

/** Карточка «что и сколько»: описание, hairline, крупная сумма по центру. */
@Composable
private fun ExpenseCard(form: AddExpenseForm, viewModel: AddExpenseViewModel) {
    val colors = Splitty.colors
    val sumFocusRequester = remember { FocusRequester() }
    val descriptionFocusRequester = remember { FocusRequester() }
    val focusManager = LocalFocusManager.current

    // Автофокус: без него форма открывается без курсора и клавиатуры —
    // неочевидно, что можно печатать сразу.
    LaunchedEffect(Unit) { descriptionFocusRequester.requestFocus() }

    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        // Описание.
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(44.dp)
                    .background(colors.ink.copy(alpha = 0.06f), RoundedCornerShape(12.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Filled.Description,
                    contentDescription = null,
                    tint = colors.inkSecondary,
                    modifier = Modifier.size(22.dp),
                )
            }
            BasicTextField(
                value = form.description,
                onValueChange = viewModel::onDescriptionChange,
                modifier = Modifier
                    .weight(1f)
                    .focusRequester(descriptionFocusRequester),
                textStyle = TextStyle(
                    color = colors.ink,
                    fontSize = 19.sp,
                    fontWeight = FontWeight.Medium,
                ),
                singleLine = true,
                cursorBrush = SolidColor(colors.accent),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next),
                keyboardActions = KeyboardActions(onNext = { sumFocusRequester.requestFocus() }),
                decorationBox = { innerTextField ->
                    Box {
                        if (form.description.isEmpty()) {
                            Text(
                                text = stringResource(R.string.expense_description_placeholder),
                                fontSize = 19.sp,
                                color = colors.inkSecondary,
                                maxLines = 1,
                            )
                        }
                        innerTextField()
                    }
                },
            )
        }

        Spacer(Modifier.height(16.dp))
        HorizontalDivider(color = colors.hairline, thickness = 1.dp)
        Spacer(Modifier.height(16.dp))

        // В режиме чека сумма — производная от позиций: показываем её крупно,
        // но read-only (править — через позиции чека, а не затирая их).
        if (form.hasDraftItems) {
            DerivedTotal(form = form)
        } else {
            // Крупная сумма по центру: символ валюты группы + tnum-цифры.
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = currencySymbol(form.currency),
                    fontSize = 28.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.inkSecondary,
                )
                Spacer(Modifier.width(8.dp))
                // Шрифт ступенчато уменьшается с длиной числа, чтобы сумма
                // целиком помещалась на экране (BasicTextField сам не сжимает).
                val sumFontSize = when {
                    form.sumText.length <= 6 -> 40.sp
                    form.sumText.length <= 8 -> 32.sp
                    else -> 26.sp
                }
                BasicTextField(
                    value = form.sumText,
                    onValueChange = viewModel::onSumChange,
                    modifier = Modifier
                        .widthIn(min = 48.dp)
                        .weight(1f, fill = false)
                        .width(IntrinsicSize.Min)
                        .focusRequester(sumFocusRequester),
                    textStyle = TextStyle(
                        color = colors.ink,
                        fontSize = sumFontSize,
                        fontWeight = FontWeight.SemiBold,
                        fontFeatureSettings = "tnum",
                        textAlign = TextAlign.Center,
                    ),
                    singleLine = true,
                    cursorBrush = SolidColor(colors.accent),
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Number,
                        imeAction = ImeAction.Done,
                    ),
                    keyboardActions = KeyboardActions(onDone = { focusManager.clearFocus() }),
                    decorationBox = { innerTextField ->
                        Box(contentAlignment = Alignment.Center) {
                            if (form.sumText.isEmpty()) {
                                Text(
                                    text = "0",
                                    fontSize = 40.sp,
                                    fontWeight = FontWeight.SemiBold,
                                    color = colors.inkSecondary,
                                    style = TextStyle(fontFeatureSettings = "tnum"),
                                )
                            }
                            innerTextField()
                        }
                    },
                )
            }
        }
        Spacer(Modifier.height(8.dp))
    }
}

/** Крупный итог itemized-черновика (read-only): сумма выводится из позиций. */
@Composable
private fun DerivedTotal(form: AddExpenseForm) {
    val colors = Splitty.colors
    val total = form.itemizedTotal ?: (form.itemizedSubtotal + form.itemizedSurcharges)
    Column(
        modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = money(total, form.currency),
            fontSize = 40.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
        Spacer(Modifier.height(4.dp))
        Text(
            text = stringResource(
                if (form.hasPricelessItems) R.string.expense_derived_incomplete else R.string.expense_derived_by_positions
            ),
            fontSize = 12.sp,
            color = if (form.hasPricelessItems) colors.negative else colors.inkSecondary,
        )
    }
}

// MARK: Карточка деления

@Composable
private fun SplitCard(form: AddExpenseForm, viewModel: AddExpenseViewModel) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        if (form.members.isEmpty()) {
            Text(
                text = stringResource(R.string.expense_select_group_hint),
                modifier = Modifier.fillMaxWidth(),
                fontSize = 15.sp,
                color = colors.inkSecondary,
                textAlign = TextAlign.Center,
            )
        } else {
            PayerRow(form = form, onSelect = viewModel::selectPayer)
            Spacer(Modifier.height(14.dp))
            SplitModeChips(selected = form.splitType, onSelect = viewModel::setSplitType)
            Spacer(Modifier.height(6.dp))
            if (form.splitType == SplitType.EQUALLY) {
                EquallySection(form = form, onToggle = viewModel::toggleRecipient)
            } else {
                ByAmountsSection(form = form, onAmountChange = viewModel::onAmountChange)
            }
        }
    }
}

/** «Заплатили [вы ▾]» — выбор донора выпадающим меню из участников. */
@Composable
private fun PayerRow(form: AddExpenseForm, onSelect: (Long) -> Unit) {
    val colors = Splitty.colors
    val payerIsMe = form.meId != null && form.payerId == form.meId
    val payerLabel = if (payerIsMe) {
        stringResource(R.string.expense_payer_you)
    } else {
        form.payer?.displayName ?: "…"
    }
    var isMenuExpanded by remember { mutableStateOf(false) }

    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(
                if (payerIsMe) R.string.expense_paid_by_you else R.string.expense_paid_by_other
            ),
            fontSize = 15.sp,
            color = colors.ink,
        )
        Box {
            SoftChip(
                text = "$payerLabel ▾",
                onClick = { isMenuExpanded = true },
            )
            DropdownMenu(
                expanded = isMenuExpanded,
                onDismissRequest = { isMenuExpanded = false },
            ) {
                form.members.forEach { member ->
                    DropdownMenuItem(
                        text = { Text(memberName(member, form.meId), color = colors.ink) },
                        leadingIcon = { GradientAvatar(user = member, size = 28.dp) },
                        trailingIcon = {
                            if (member.id == form.payerId) {
                                Icon(
                                    imageVector = Icons.Filled.Check,
                                    contentDescription = null,
                                    tint = colors.accent,
                                )
                            }
                        },
                        onClick = {
                            onSelect(member.id)
                            isMenuExpanded = false
                        },
                    )
                }
            }
        }
    }
}

/** Переключатель способа деления: «Поровну» | «По суммам». */
@Composable
private fun SplitModeChips(selected: SplitType, onSelect: (SplitType) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        SoftChip(
            text = stringResource(R.string.expense_split_equally),
            onClick = { onSelect(SplitType.EQUALLY) },
            isSelected = selected == SplitType.EQUALLY,
        )
        SoftChip(
            text = stringResource(R.string.expense_split_by_amounts),
            onClick = { onSelect(SplitType.BY_EXACT_AMOUNT) },
            isSelected = selected == SplitType.BY_EXACT_AMOUNT,
        )
    }
}

/** Режим «Поровну»: чекбоксы участников + подсказка деления из shares(). */
@Composable
private fun EquallySection(form: AddExpenseForm, onToggle: (Long) -> Unit) {
    val colors = Splitty.colors
    Column {
        form.members.forEachIndexed { index, member ->
            val isSelected = member.id in form.recipientIds
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { onToggle(member.id) }
                    .padding(vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                GradientAvatar(user = member, size = 32.dp)
                Text(
                    text = memberName(member, form.meId),
                    modifier = Modifier.weight(1f),
                    fontSize = 15.sp,
                    color = colors.ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Icon(
                    imageVector = if (isSelected) Icons.Filled.CheckCircle else Icons.Outlined.Circle,
                    contentDescription = null,
                    tint = if (isSelected) colors.accent else colors.inkSecondary.copy(alpha = 0.4f),
                    modifier = Modifier.size(24.dp),
                )
            }
            if (index != form.members.lastIndex) {
                HorizontalDivider(
                    modifier = Modifier.padding(start = 44.dp),
                    color = colors.hairline,
                    thickness = 1.dp,
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        Text(
            text = equalSplitHint(form),
            modifier = Modifier.fillMaxWidth(),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = colors.inkSecondary,
            textAlign = TextAlign.Center,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
    }
}

/** Подсказка равного деления: «1 000 ₽ / 3 = 333–334 ₽ с человека». */
@Composable
private fun equalSplitHint(form: AddExpenseForm): String {
    val count = form.recipientIds.size
    if (count == 0) return stringResource(R.string.expense_hint_pick_member)
    val sum = form.sum ?: 0
    if (sum < 1) return stringResource(R.string.expense_hint_members_count, count)
    val parts = shares(sum, count)
    val maxShare = parts.first()
    val minShare = parts.last()
    val perPerson = if (minShare == maxShare) {
        money(minShare, form.currency)
    } else {
        // Остаток по каноническому правилу достаётся первым получателям.
        moneyRange(minShare, maxShare, form.currency)
    }
    return stringResource(
        R.string.expense_split_hint_equal,
        money(sum, form.currency),
        count,
        perPerson,
    )
}

/** Режим «По суммам»: поля точных долей выбранных участников + остаток. */
@Composable
private fun ByAmountsSection(form: AddExpenseForm, onAmountChange: (Long, String) -> Unit) {
    val colors = Splitty.colors
    Column {
        val selected = form.selectedMembers
        selected.forEachIndexed { index, member ->
            AmountRow(
                member = member,
                meId = form.meId,
                currency = form.currency,
                text = form.amountTexts[member.id].orEmpty(),
                onTextChange = { onAmountChange(member.id, it) },
            )
            if (index != selected.lastIndex) {
                HorizontalDivider(
                    modifier = Modifier.padding(start = 44.dp),
                    color = colors.hairline,
                    thickness = 1.dp,
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        val statusColor = when {
            form.recipientIds.isEmpty() -> colors.inkSecondary
            form.isDistributionBalanced -> colors.accent
            form.remainingToDistribute < 0 -> colors.negative
            else -> colors.inkSecondary
        }
        Text(
            text = distributionHint(form),
            modifier = Modifier.fillMaxWidth(),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = statusColor,
            textAlign = TextAlign.Center,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
    }
}

/** Живая подпись режима «По суммам»: остаток/перерасход/готово. */
@Composable
private fun distributionHint(form: AddExpenseForm): String = when {
    form.recipientIds.isEmpty() -> stringResource(R.string.expense_hint_pick_member)
    form.isDistributionBalanced -> stringResource(R.string.expense_distributed)
    form.remainingToDistribute < 0 -> stringResource(
        R.string.expense_overspent,
        money(-form.remainingToDistribute, form.currency),
    )

    else -> stringResource(
        R.string.expense_remaining,
        money(form.remainingToDistribute, form.currency),
    )
}

/** Строка участника с полем точной суммы (tnum, только цифры). */
@Composable
private fun AmountRow(
    member: User,
    meId: Long?,
    currency: String,
    text: String,
    onTextChange: (String) -> Unit,
) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        GradientAvatar(user = member, size = 32.dp)
        Text(
            text = memberName(member, meId),
            modifier = Modifier.weight(1f),
            fontSize = 15.sp,
            color = colors.ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        BasicTextField(
            value = text,
            onValueChange = onTextChange,
            modifier = Modifier.width(90.dp),
            textStyle = TextStyle(
                color = colors.ink,
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                fontFeatureSettings = "tnum",
                textAlign = TextAlign.End,
            ),
            singleLine = true,
            cursorBrush = SolidColor(colors.accent),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            decorationBox = { innerTextField ->
                Box(contentAlignment = Alignment.CenterEnd) {
                    if (text.isEmpty()) {
                        Text(
                            text = "0",
                            modifier = Modifier.fillMaxWidth(),
                            fontSize = 17.sp,
                            fontWeight = FontWeight.SemiBold,
                            color = colors.inkSecondary,
                            textAlign = TextAlign.End,
                            style = TextStyle(fontFeatureSettings = "tnum"),
                        )
                    }
                    innerTextField()
                }
            },
        )
        Text(
            text = currencySymbol(currency),
            fontSize = 15.sp,
            color = colors.inkSecondary,
        )
    }
}

/** Имя участника; для текущего пользователя — «Имя (вы)». */
@Composable
private fun memberName(member: User, meId: Long?): String =
    if (meId != null && member.id == meId) {
        stringResource(R.string.expense_member_you, member.displayName)
    } else {
        member.displayName
    }

/** Карточка «Распознать по чеку»: сфотографировать или выбрать фото из галереи. */
@Composable
private fun ReceiptScanCard(onTakePhoto: () -> Unit, onPickPhoto: () -> Unit) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        SectionHeader(stringResource(R.string.expense_receipt_scan_title))
        Spacer(Modifier.height(6.dp))
        Text(
            text = stringResource(R.string.expense_receipt_scan_hint),
            fontSize = 13.sp,
            color = colors.inkSecondary,
        )
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ScanActionChip(
                icon = Icons.Filled.PhotoCamera,
                text = stringResource(R.string.expense_receipt_take_photo),
                onClick = onTakePhoto,
            )
            ScanActionChip(
                icon = Icons.Filled.PhotoLibrary,
                text = stringResource(R.string.expense_receipt_pick_photo),
                onClick = onPickPhoto,
            )
        }
    }
}

/** Чип-действие с иконкой (сфотографировать / выбрать фото). */
@Composable
private fun ScanActionChip(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    text: String,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(50))
            .background(colors.ink.copy(alpha = 0.06f))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = colors.accent,
            modifier = Modifier.size(18.dp),
        )
        Text(text = text, fontSize = 14.sp, fontWeight = FontWeight.Medium, color = colors.ink)
    }
}

/** Баннер ошибки распознавания: текст + «Повторить»; черновик не потерян. */
@Composable
private fun ParseRetryBanner(message: String, onRetry: () -> Unit, onDismiss: () -> Unit) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 12.dp) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(
                text = message,
                modifier = Modifier.weight(1f),
                fontSize = 13.sp,
                color = colors.inkSecondary,
            )
            TextButton(onClick = onRetry) {
                Text(stringResource(R.string.expense_parse_retry), color = colors.accent)
            }
            Icon(
                imageVector = Icons.Filled.Close,
                contentDescription = null,
                tint = colors.inkSecondary,
                modifier = Modifier
                    .size(20.dp)
                    .clickable(onClick = onDismiss),
            )
        }
    }
}

/**
 * Оверлей распознавания: затемнение + спиннер + подпись; кнопка «Отмена»
 * появляется через 2.5с (иначе для быстрого ответа она лишь мигает). Порт iOS.
 */
@Composable
private fun ParsingOverlay(onCancel: () -> Unit, modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    var showCancel by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        delay(2500)
        showCancel = true
    }
    Box(
        modifier = modifier
            .background(colors.bg.copy(alpha = 0.88f))
            // Гасим тапы под оверлеем (форму нельзя трогать во время распознавания).
            .clickable(enabled = false, onClick = {}),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            CircularProgressIndicator(color = colors.accent)
            Text(
                text = stringResource(R.string.expense_parsing),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.ink,
            )
            if (showCancel) {
                TextButton(onClick = onCancel) {
                    Text(stringResource(R.string.expense_parsing_cancel), color = colors.accent)
                }
            }
        }
    }
}

/** Ошибка первичной загрузки с кнопкой «Повторить». */
@Composable
private fun LoadErrorPane(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    FailedState(message = message, onRetry = onRetry, modifier = modifier)
}
