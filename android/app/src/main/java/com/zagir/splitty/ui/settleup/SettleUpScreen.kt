package com.zagir.splitty.ui.settleup

import com.zagir.splitty.core.ui.resolve
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.WifiOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CenterAlignedTopAppBar
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.money.currencySymbol
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.components.FailedState
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.MoneyRole
import com.zagir.splitty.ui.components.MoneyText
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.theme.Splitty

/**
 * Погашение долга (порт iOS SettleUpView): шаг 1 — список моих долгов
 * группы карточками «А должен(на) Б — сумма»; шаг 2 — «Записать платёж»
 * с крупным полем суммы (prefill полной суммой долга) и CTA.
 * Если долг с моим участием ровно один — сразу шаг 2.
 *
 * @param roomId группа, долги которой гасим (суммы — в её валюте).
 * @param onDone закрыть экран — зовётся после успешного платежа и по «Отмена».
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettleUpScreen(
    roomId: String,
    viewModel: SettleUpViewModel = hiltViewModel(),
    onDone: () -> Unit,
) {
    LaunchedEffect(Unit) { viewModel.trackScreen() }
    LaunchedEffect(roomId) {
        viewModel.start(roomId)
        viewModel.trackOpened()
    }
    val state by viewModel.state.collectAsStateWithLifecycle()
    val isOnline by viewModel.isOnline.collectAsStateWithLifecycle()
    val snapshot = state
    val form = if (snapshot is UiState.Content) snapshot.value else null
    val haptics = rememberHaptics()

    LaunchedEffect(form?.isSaved) {
        if (form?.isSaved == true) {
            haptics.success() // успех платежа — тактильный отклик (порт iOS Haptics.success)
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
                        text = stringResource(R.string.settleup_title),
                        fontSize = 17.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                },
                navigationIcon = {
                    if (form?.showsBackToList == true) {
                        IconButton(onClick = viewModel::backToList) {
                            Icon(
                                imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                                contentDescription = stringResource(R.string.settleup_back_to_list),
                                tint = colors.accent,
                            )
                        }
                    }
                },
                actions = {
                    TextButton(onClick = onDone) {
                        Text(
                            text = stringResource(R.string.common_cancel),
                            fontSize = 17.sp,
                            color = colors.accentText,
                        )
                    }
                },
                colors = TopAppBarDefaults.centerAlignedTopAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                ),
            )
        },
    ) { innerPadding ->
        val contentModifier = Modifier
            .fillMaxSize()
            .padding(innerPadding)
        when (val current = state) {
            is UiState.Loading -> Box(
                modifier = contentModifier,
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(color = colors.accent)
            }

            is UiState.Error -> LoadErrorPane(
                message = current.message.resolve(),
                onRetry = viewModel::retry,
                modifier = contentModifier,
            )

            is UiState.Content -> {
                val debt = current.value.selectedDebt
                if (debt != null) {
                    PaymentStep(
                        form = current.value,
                        debt = debt,
                        isOnline = isOnline,
                        viewModel = viewModel,
                        modifier = contentModifier,
                    )
                } else {
                    DebtListStep(
                        form = current.value,
                        onSelect = viewModel::selectDebt,
                        modifier = contentModifier,
                    )
                }
            }
        }
    }

    form?.alert?.let { alert ->
        val message = when (alert) {
            SettleUpAlert.DebtSettled -> stringResource(R.string.settleup_already_settled)
            is SettleUpAlert.Message -> alert.text
        }
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

// MARK: Шаг 1 — список долгов

@Composable
private fun DebtListStep(
    form: SettleUpForm,
    onSelect: (Debt) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (form.debts.isEmpty()) {
        NoDebtsPane(modifier = modifier)
        return
    }
    Column(
        modifier = modifier
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SectionHeader(
            text = stringResource(R.string.settleup_your_debts),
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        form.debts.forEach { debt ->
            DebtRow(
                debt = debt,
                currency = form.currency,
                meId = form.meId,
                onClick = { onSelect(debt) },
            )
        }
    }
}

/** Карточка долга: аватары «должник → кредитор», подпись и сумма, шеврон. */
@Composable
private fun DebtRow(
    debt: Debt,
    currency: String,
    meId: Long?,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    // Без ripple: клип карточки применяется внутри SurfaceCard, и прямоугольная
    // подсветка вылезала бы за скруглённые углы (паттерн SoftChip).
    val interactionSource = remember { MutableInteractionSource() }
    SurfaceCard(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick,
            ),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            GradientAvatar(user = debt.debtor, size = 40.dp)
            Icon(
                imageVector = Icons.AutoMirrored.Filled.ArrowForward,
                contentDescription = null,
                tint = colors.inkSecondary,
                modifier = Modifier.size(16.dp),
            )
            GradientAvatar(user = debt.lender, size = 40.dp)
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Text(
                    text = debtTitle(debt, meId),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Medium,
                    color = colors.ink,
                    maxLines = 2,
                )
                MoneyText(
                    amount = debt.sum,
                    role = if (debt.debtor.id == meId) MoneyRole.NEGATIVE else MoneyRole.POSITIVE,
                    currency = currency,
                )
            }
            Icon(
                imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = colors.inkSecondary,
            )
        }
    }
}

@Composable
private fun debtTitle(debt: Debt, meId: Long?): String = when (meId) {
    debt.debtor.id -> stringResource(R.string.settleup_you_owe, debt.lender.displayName)
    debt.lender.id -> stringResource(R.string.settleup_owes_you, debt.debtor.displayName)
    else -> stringResource(
        R.string.settleup_owes,
        debt.debtor.displayName,
        debt.lender.displayName,
    )
}

/** Пустое состояние: в группе нет долгов с моим участием. */
@Composable
private fun NoDebtsPane(modifier: Modifier = Modifier) {
    val colors = Splitty.colors
    Column(
        modifier = modifier.padding(horizontal = 40.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            imageVector = Icons.Filled.CheckCircle,
            contentDescription = null,
            tint = colors.accent,
            modifier = Modifier.size(44.dp),
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = stringResource(R.string.settleup_no_debts_title),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            text = stringResource(R.string.settleup_no_debts_message),
            fontSize = 15.sp,
            color = colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
    }
}

// MARK: Шаг 2 — форма платежа

@Composable
private fun PaymentStep(
    form: SettleUpForm,
    debt: Debt,
    isOnline: Boolean,
    viewModel: SettleUpViewModel,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        PaymentHeaderCard(debt = debt, meId = form.meId)
        PaymentSumCard(form = form, debt = debt, onSumChange = viewModel::onSumChange)
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            // Офлайн — CTA заблокирован с подписью-причиной (а не алерт по тапу).
            PrimaryPillButton(
                text = stringResource(R.string.settleup_submit),
                onClick = viewModel::repay,
                enabled = form.isSumValid && !form.isSaving && isOnline,
            )
            if (!isOnline) {
                Text(
                    text = stringResource(R.string.settleup_offline_caption),
                    fontSize = 13.sp,
                    fontWeight = FontWeight.Medium,
                    color = Splitty.colors.negativeText,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            // Приложение только ведёт счёт: деньги оно не переводит. Без этой
            // строки «Записать платёж» читается как перевод, и человек ждёт,
            // что деньги уйдут сами
            Text(
                text = stringResource(R.string.settleup_records_only),
                fontSize = 13.sp,
                color = Splitty.colors.inkSecondary,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Карточка-шапка: аватар должника → аватар кредитора, «Алмаз платит вам». */
@Composable
private fun PaymentHeaderCard(debt: Debt, meId: Long?) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(8.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(20.dp),
            ) {
                GradientAvatar(user = debt.debtor, size = 64.dp)
                Icon(
                    imageVector = Icons.AutoMirrored.Filled.ArrowForward,
                    contentDescription = null,
                    tint = colors.accent,
                    modifier = Modifier.size(24.dp),
                )
                GradientAvatar(user = debt.lender, size = 64.dp)
            }
            Text(
                text = paymentTitle(debt, meId),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                textAlign = TextAlign.Center,
            )
        }
    }
}

@Composable
private fun paymentTitle(debt: Debt, meId: Long?): String = when (meId) {
    debt.debtor.id -> stringResource(R.string.settleup_you_pay, debt.lender.displayName)
    debt.lender.id -> stringResource(R.string.settleup_pays_you, debt.debtor.displayName)
    else -> stringResource(
        R.string.settleup_pays,
        debt.debtor.displayName,
        debt.lender.displayName,
    )
}

/**
 * Карточка суммы платежа: крупное tnum-поле по центру (prefill полной суммой
 * долга), hairline и подпись «Долг: N» / «Не больше долга: N» (negative).
 */
@Composable
private fun PaymentSumCard(
    form: SettleUpForm,
    debt: Debt,
    onSumChange: (String) -> Unit,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.Center,
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
                    onValueChange = onSumChange,
                    modifier = Modifier
                        .widthIn(min = 48.dp)
                        .width(IntrinsicSize.Min),
                    textStyle = TextStyle(
                        color = colors.ink,
                        fontSize = 42.sp,
                        fontWeight = FontWeight.SemiBold,
                        fontFeatureSettings = "tnum",
                        textAlign = TextAlign.Center,
                    ),
                    singleLine = true,
                    cursorBrush = SolidColor(colors.accent),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    decorationBox = { innerTextField ->
                        Box(contentAlignment = Alignment.Center) {
                            if (form.sumText.isEmpty()) {
                                Text(
                                    text = "0",
                                    fontSize = 42.sp,
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
            Box(
                modifier = Modifier
                    .width(160.dp)
                    .height(1.dp)
                    .background(colors.hairline, RoundedCornerShape(1.dp)),
            )
            val isOverDebt = (form.sum ?: 0) > debt.sum
            Text(
                text = stringResource(
                    if (isOverDebt) R.string.settleup_debt_max else R.string.settleup_debt_hint,
                    money(debt.sum, form.currency),
                ),
                fontSize = 13.sp,
                fontWeight = FontWeight.Medium,
                color = if (isOverDebt) colors.negative else colors.inkSecondary,
                style = TextStyle(fontFeatureSettings = "tnum"),
            )
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
