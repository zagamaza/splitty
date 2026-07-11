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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Description
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
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

    LaunchedEffect(form?.isSaved) {
        if (form?.isSaved == true) onDone()
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
                    PrimaryPillButton(
                        text = stringResource(R.string.common_save),
                        onClick = viewModel::save,
                        enabled = form.canSave && !form.isSaving &&
                            canSaveExpenseOffline(form.isEditingSynced, isOnline),
                    )
                }
            }
        },
    ) { innerPadding ->
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
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
            )
        }
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

// MARK: Содержимое формы

@Composable
private fun ExpenseFormContent(
    form: AddExpenseForm,
    isOnline: Boolean,
    viewModel: AddExpenseViewModel,
    modifier: Modifier = Modifier,
) {
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
        if (form.showsRoomPicker) {
            GroupPickerCard(form = form, onSelect = viewModel::selectRoom)
        }
        ExpenseCard(form = form, viewModel = viewModel)
        SplitCard(form = form, viewModel = viewModel)
        // Неотправленную запись можно удалить прямо из формы правки.
        if (form.isEditingLocal) {
            DeleteLocalCard(enabled = !form.isSaving, onDelete = viewModel::deleteLocal)
        }
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

/** Выбор группы чипами (экран открыт с центральной «+»). */
@Composable
private fun GroupPickerCard(
    form: AddExpenseForm,
    onSelect: (RoomSummary) -> Unit,
) {
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        SectionHeader(stringResource(R.string.expense_group_section))
        Spacer(Modifier.height(12.dp))
        if (form.rooms.isEmpty()) {
            Text(
                text = stringResource(R.string.expense_no_groups),
                fontSize = 15.sp,
                color = Splitty.colors.inkSecondary,
            )
        } else {
            Row(
                modifier = Modifier.horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                form.rooms.forEach { room ->
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
    val focusManager = LocalFocusManager.current

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
                modifier = Modifier.weight(1f),
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
            BasicTextField(
                value = form.sumText,
                onValueChange = viewModel::onSumChange,
                modifier = Modifier
                    .widthIn(min = 48.dp)
                    .width(IntrinsicSize.Min)
                    .focusRequester(sumFocusRequester),
                textStyle = TextStyle(
                    color = colors.ink,
                    fontSize = 40.sp,
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
        Spacer(Modifier.height(8.dp))
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

/** Ошибка первичной загрузки с кнопкой «Повторить». */
@Composable
private fun LoadErrorPane(
    message: String,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = Splitty.colors
    Column(
        modifier = modifier.padding(horizontal = 40.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            imageVector = Icons.Filled.WifiOff,
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = stringResource(R.string.common_load_failed),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            text = message,
            fontSize = 15.sp,
            color = colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(16.dp))
        SoftChip(
            text = stringResource(R.string.common_retry),
            onClick = onRetry,
        )
    }
}
