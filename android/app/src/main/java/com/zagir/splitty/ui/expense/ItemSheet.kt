package com.zagir.splitty.ui.expense

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material.icons.outlined.Circle
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.ui.res.stringResource
import com.zagir.splitty.R
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.model.derivedShares
import com.zagir.splitty.core.model.isSurcharge
import com.zagir.splitty.core.model.shareList
import com.zagir.splitty.core.money.currencySymbol
import com.zagir.splitty.core.money.money
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.components.rememberHaptics
import com.zagir.splitty.ui.theme.Splitty

// Шит правки позиции чека — порт ios/.../AddExpenseView.swift → ItemSheetView.
// «Долями» (веса-степперы) или «Суммами» (поля точных сумм); живой пересчёт
// долей (зеркало серверного derivedShares), удаление с подтверждением. Для
// надбавки (сбор) — только название и цена.

// MARK: Живой расчёт деления (чистая логика — под JVM-тест)

/** Итог деления позиции по текущему состоянию шита. */
sealed interface ItemSplitStatus {
    /** Деление сходится: userId → сумма (зеркало серверного расчёта). */
    data class Ok(val shares: Map<Long, Long>) : ItemSplitStatus
    object NoPrice : ItemSplitStatus
    object NoParticipants : ItemSplitStatus
    /** Все фиксы, до цены не хватает [rest] (некому отдать остаток). */
    data class Under(val rest: Long) : ItemSplitStatus
    /** Фиксы превышают цену на [extra]. */
    data class Over(val extra: Long) : ItemSplitStatus
}

/** Фикс участника из режима «Суммами»; null — «авто» (пустое/нулевое поле). */
private fun fixedAmountOf(byAmount: Boolean, amounts: Map<Long, String>, id: Long): Long? =
    if (byAmount) amounts[id]?.toLongOrNull()?.takeIf { it > 0 } else null

/** Доли из состояния шита — ровно та же сборка, что в commit. */
internal fun itemSheetShares(
    members: List<User>,
    participating: Set<Long>,
    byAmount: Boolean,
    weights: Map<Long, Int>,
    amounts: Map<Long, String>,
): List<ItemShare> = run {
    // Порядок — как в members, но участники, которых нет в members (состав
    // комнаты изменился с момента создания операции), НЕ выбрасываются: раньше
    // фильтр по members молча стирал их долю и перераспределял деньги между
    // остальными, а шит при этом рапортовал «сумма распределена полностью».
    val ordered = members.map { it.id }.filter { it in participating }
    ordered + participating.filterNot { it in ordered }
}.map { id ->
    val fixed = fixedAmountOf(byAmount, amounts, id)
    if (fixed != null) {
        ItemShare(userId = id, weight = 1, amount = fixed)
    } else {
        ItemShare(userId = id, weight = if (byAmount) 1 else maxOf(1, weights[id] ?: 1))
    }
}

/**
 * Статус деления цены по текущему состоянию шита (порт iOS `splitStatus`):
 * нет цены/участников, перебор/недобор фиксов или сошедшееся деление с картой сумм.
 */
internal fun computeItemSplitStatus(
    price: Long,
    members: List<User>,
    participating: Set<Long>,
    byAmount: Boolean,
    weights: Map<Long, Int>,
    amounts: Map<Long, String>,
): ItemSplitStatus {
    if (price < 1) return ItemSplitStatus.NoPrice
    if (participating.isEmpty()) return ItemSplitStatus.NoParticipants
    val fixed = participating.sumOf { fixedAmountOf(byAmount, amounts, it) ?: 0L }
    if (fixed > price) return ItemSplitStatus.Over(fixed - price)
    val hasAuto = participating.any { fixedAmountOf(byAmount, amounts, it) == null }
    if (!hasAuto && fixed < price) return ItemSplitStatus.Under(price - fixed)
    val item = OperationItem(
        name = "·",
        price = price,
        shares = itemSheetShares(members, participating, byAmount, weights, amounts),
    )
    val shares = listOf(item).derivedShares()?.shares ?: return ItemSplitStatus.Under(price - fixed)
    return ItemSplitStatus.Ok(shares)
}

// MARK: Шит

/**
 * Модальный шит правки позиции чека [item] (индекс [index] в черновике). «Готово»
 * пересобирает позицию и зовёт [onCommit]; «Удалить» — [onDelete] после подтверждения.
 *
 * @param onCommit сохранить изменённую позицию (write-back в черновик).
 * @param onDelete удалить позицию из чека.
 * @param onDismiss закрыть шит без изменений.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ItemSheet(
    item: OperationItem,
    members: List<User>,
    currency: String,
    meId: Long?,
    onCommit: (OperationItem) -> Unit,
    onDelete: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = Splitty.colors.bg,
    ) {
        ItemSheetBody(
            item = item,
            members = members,
            currency = currency,
            meId = meId,
            onCommit = onCommit,
            onDelete = onDelete,
            onDismiss = onDismiss,
        )
    }
}

/**
 * Тело шита позиции без [ModalBottomSheet]-обёртки — так его можно рендерить в
 * Roborazzi-снапшотах (модальный шит в Robolectric не встаёт). Держит всё
 * состояние правки и живой пересчёт.
 */
@Composable
fun ItemSheetBody(
    item: OperationItem,
    members: List<User>,
    currency: String,
    meId: Long?,
    onCommit: (OperationItem) -> Unit,
    onDelete: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = Splitty.colors
    val haptics = rememberHaptics()
    val isSurcharge = item.isSurcharge

    // Ключ по item обязателен: лист живёт на стабильной позиции композиции, и
    // смена редактируемой позиции (добавление строки, переприход /parse) не
    // выводила его из композиции. Без ключа commit() писал имя, цену и деление
    // предыдущей позиции поверх другой строки чека.
    var name by remember(item) { mutableStateOf(item.name) }
    var priceText by remember(item) { mutableStateOf(if (item.price > 0) item.price.toString() else "") }
    // false — «Долями» (веса-степперы), true — «Суммами» (поля сумм).
    var byAmount by remember(item) { mutableStateOf(item.shareList.any { it.amount != null }) }
    val participating = remember(item) {
        mutableStateMapOf<Long, Boolean>().apply {
            item.shareList.forEach { put(it.userId, true) }
        }
    }
    val weights = remember(item) {
        mutableStateMapOf<Long, Int>().apply { item.shareList.forEach { put(it.userId, it.weight) } }
    }
    val amounts = remember(item) {
        mutableStateMapOf<Long, String>().apply {
            item.shareList.forEach { s -> s.amount?.let { put(s.userId, it.toString()) } }
        }
    }
    var confirmDelete by remember(item) { mutableStateOf(false) }

    val participatingSet = participating.filterValues { it }.keys
    val price = priceText.toLongOrNull() ?: 0L
    val status = computeItemSplitStatus(
        price = price,
        members = members,
        participating = participatingSet,
        byAmount = byAmount,
        weights = weights,
        amounts = amounts,
    )
    val isCommittable = if (isSurcharge) price >= 1 else status is ItemSplitStatus.Ok

    fun commit() {
        val finalPrice = priceText.toLongOrNull() ?: item.price
        val trimmedName = name.trim()
        val newShares = if (isSurcharge) {
            null
        } else {
            itemSheetShares(members, participatingSet, byAmount, weights, amounts)
        }
        onCommit(
            item.copy(
                name = trimmedName.ifEmpty { item.name },
                price = finalPrice,
                shares = newShares,
            )
        )
    }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            // Прокрутка: список участников рос вниз без ограничения, и в группе
            // из десятка человек нижние строки вместе с кнопкой «Готово»
            // уезжали за экран
            .verticalScroll(rememberScrollState())
            .navigationBarsPadding()
            .imePadding()
            .padding(horizontal = 20.dp)
            .padding(bottom = 20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        // Заголовок + «Готово».
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = stringResource(if (isSurcharge) R.string.item_sheet_title_surcharge else R.string.item_sheet_title_item),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.ink,
                modifier = Modifier.weight(1f),
            )
            TextButton(
                onClick = { commit(); onDismiss() },
                enabled = isCommittable,
            ) {
                Text(
                    text = stringResource(R.string.item_sheet_done),
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = if (isCommittable) colors.accent else colors.inkSecondary,
                )
            }
        }

        FieldsCard(
            name = name,
            onNameChange = { name = it },
            priceText = priceText,
            onPriceChange = { priceText = digitsField(it) },
            currency = currency,
        )

        if (!isSurcharge) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SoftChip(text = stringResource(R.string.item_sheet_mode_shares), onClick = { byAmount = false }, isSelected = !byAmount)
                SoftChip(text = stringResource(R.string.item_sheet_mode_amounts), onClick = { byAmount = true }, isSelected = byAmount)
            }
            Text(
                text = if (byAmount) {
                    stringResource(R.string.item_sheet_hint_amounts)
                } else {
                    stringResource(R.string.item_sheet_hint_shares)
                },
                modifier = Modifier.fillMaxWidth(),
                fontSize = 12.sp,
                color = colors.inkSecondary,
                textAlign = TextAlign.Center,
            )
            ParticipantsCard(
                members = members,
                meId = meId,
                currency = currency,
                byAmount = byAmount,
                participating = participating,
                weights = weights,
                amounts = amounts,
                status = status,
                onToggle = { id ->
                    haptics.tap()
                    val on = participating[id] == true
                    participating[id] = !on
                    if (!on && weights[id] == null) weights[id] = 1
                },
                onWeightChange = { id, w -> weights[id] = maxOf(1, w) },
                onAmountChange = { id, v -> amounts[id] = digitsField(v) },
            )
            SplitStatusLine(status = status, currency = currency)
        }

        TextButton(
            onClick = { confirmDelete = true },
            modifier = Modifier.fillMaxWidth(),
        ) {
            Icon(
                imageVector = Icons.Filled.Delete,
                contentDescription = null,
                tint = colors.negative,
                modifier = Modifier.size(18.dp),
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = stringResource(if (isSurcharge) R.string.item_sheet_delete_surcharge else R.string.item_sheet_delete_item),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = colors.negative,
            )
        }
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text(stringResource(if (isSurcharge) R.string.item_sheet_delete_surcharge_q else R.string.item_sheet_delete_item_q)) },
            text = { Text(stringResource(R.string.item_sheet_delete_message)) },
            confirmButton = {
                TextButton(onClick = {
                    haptics.tap()
                    confirmDelete = false
                    onDelete()
                    onDismiss()
                }) {
                    Text(stringResource(R.string.item_sheet_delete_confirm), color = colors.negative)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = false }) { Text(stringResource(R.string.common_cancel)) }
            },
        )
    }
}

/** Оставляет только цифры (макс. 9) — целые суммы полей шита. */
private fun digitsField(raw: String): String = raw.filter { it.isDigit() }.take(9)

@Composable
private fun FieldsCard(
    name: String,
    onNameChange: (String) -> Unit,
    priceText: String,
    onPriceChange: (String) -> Unit,
    currency: String,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth()) {
        BasicTextField(
            value = name,
            onValueChange = onNameChange,
            modifier = Modifier.fillMaxWidth(),
            textStyle = TextStyle(color = colors.ink, fontSize = 17.sp, fontWeight = FontWeight.Medium),
            singleLine = true,
            cursorBrush = SolidColor(colors.accent),
            decorationBox = { inner ->
                Box {
                    if (name.isEmpty()) {
                        Text(stringResource(R.string.item_sheet_name_label), fontSize = 17.sp, color = colors.inkSecondary)
                    }
                    inner()
                }
            },
        )
        Spacer(Modifier.height(12.dp))
        HorizontalDivider(color = colors.hairline, thickness = 1.dp)
        Spacer(Modifier.height(12.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.item_sheet_price_label), fontSize = 15.sp, color = colors.inkSecondary, modifier = Modifier.weight(1f))
            BasicTextField(
                value = priceText,
                onValueChange = onPriceChange,
                modifier = Modifier.width(100.dp),
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
                decorationBox = { inner ->
                    Box(contentAlignment = Alignment.CenterEnd) {
                        if (priceText.isEmpty()) {
                            Text(
                                "0",
                                modifier = Modifier.fillMaxWidth(),
                                fontSize = 17.sp,
                                fontWeight = FontWeight.SemiBold,
                                color = colors.inkSecondary,
                                textAlign = TextAlign.End,
                                style = TextStyle(fontFeatureSettings = "tnum"),
                            )
                        }
                        inner()
                    }
                },
            )
            Spacer(Modifier.width(6.dp))
            Text(currencySymbol(currency), fontSize = 15.sp, color = colors.inkSecondary)
        }
    }
}

@Composable
private fun ParticipantsCard(
    members: List<User>,
    meId: Long?,
    currency: String,
    byAmount: Boolean,
    participating: Map<Long, Boolean>,
    weights: Map<Long, Int>,
    amounts: Map<Long, String>,
    status: ItemSplitStatus,
    onToggle: (Long) -> Unit,
    onWeightChange: (Long, Int) -> Unit,
    onAmountChange: (Long, String) -> Unit,
) {
    val colors = Splitty.colors
    val liveShares = (status as? ItemSplitStatus.Ok)?.shares.orEmpty()
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        members.forEachIndexed { index, member ->
            val isOn = participating[member.id] == true
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                GradientAvatar(
                    user = member,
                    size = 32.dp,
                    modifier = Modifier.alpha(if (isOn) 1f else 0.4f),
                )
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .clickable { onToggle(member.id) },
                ) {
                    Text(
                        text = if (meId != null && member.id == meId) {
                            stringResource(R.string.member_you_suffix, member.displayName)
                        } else {
                            member.displayName
                        },
                        fontSize = 15.sp,
                        color = if (isOn) colors.ink else colors.inkSecondary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    rowCaption(
                        userId = member.id,
                        isOn = isOn,
                        byAmount = byAmount,
                        weight = maxOf(1, weights[member.id] ?: 1),
                        fixedEntered = byAmount && (amounts[member.id]?.toIntOrNull() ?: 0) > 0,
                        liveAmount = liveShares[member.id],
                        currency = currency,
                    )?.let { caption ->
                        Text(
                            text = caption,
                            fontSize = 12.sp,
                            fontWeight = FontWeight.Medium,
                            color = colors.inkSecondary,
                            style = TextStyle(fontFeatureSettings = "tnum"),
                        )
                    }
                }
                if (isOn) {
                    if (byAmount) {
                        AmountField(
                            text = amounts[member.id].orEmpty(),
                            currency = currency,
                            onChange = { onAmountChange(member.id, it) },
                        )
                    } else {
                        WeightStepper(
                            value = maxOf(1, weights[member.id] ?: 1),
                            onChange = { onWeightChange(member.id, it) },
                        )
                    }
                } else {
                    Icon(
                        imageVector = Icons.Outlined.Circle,
                        contentDescription = null,
                        tint = colors.inkSecondary.copy(alpha = 0.4f),
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
            if (index != members.lastIndex) {
                HorizontalDivider(
                    modifier = Modifier.padding(start = 44.dp),
                    color = colors.hairline,
                    thickness = 1.dp,
                )
            }
        }
    }
}

/**
 * Живая подпись под именем: «×3 · 150 ₽» («Долями») или «авто · 1 250 ₽»
 * у не-зафиксированных («Суммами»). null — не участвует или фикс введён.
 */
@Composable
private fun rowCaption(
    userId: Long,
    isOn: Boolean,
    byAmount: Boolean,
    weight: Int,
    fixedEntered: Boolean,
    liveAmount: Long?,
    currency: String,
): String? {
    if (!isOn) return null
    if (byAmount) {
        if (fixedEntered) return null
        return if (liveAmount == null) {
            stringResource(R.string.item_sheet_auto)
        } else {
            stringResource(R.string.item_sheet_auto_with_sum, money(liveAmount, currency))
        }
    }
    return if (liveAmount == null) {
        stringResource(R.string.item_sheet_weight_caption, weight)
    } else {
        stringResource(R.string.item_sheet_weight_with_sum, weight, money(liveAmount, currency))
    }
}

@Composable
private fun AmountField(text: String, currency: String, onChange: (String) -> Unit) {
    val colors = Splitty.colors
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        BasicTextField(
            value = text,
            onValueChange = onChange,
            modifier = Modifier.width(72.dp),
            textStyle = TextStyle(
                color = colors.ink,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                fontFeatureSettings = "tnum",
                textAlign = TextAlign.End,
            ),
            singleLine = true,
            cursorBrush = SolidColor(colors.accent),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            decorationBox = { inner ->
                Box(contentAlignment = Alignment.CenterEnd) {
                    if (text.isEmpty()) {
                        Text(
                            stringResource(R.string.item_sheet_auto),
                            modifier = Modifier.fillMaxWidth(),
                            fontSize = 15.sp,
                            color = colors.inkSecondary,
                            textAlign = TextAlign.End,
                        )
                    }
                    inner()
                }
            },
        )
        Text(currencySymbol(currency), fontSize = 13.sp, color = colors.inkSecondary)
    }
}

/** Степпер веса доли (1…20) — ровно один контрол на строку в режиме «Долями». */
@Composable
private fun WeightStepper(value: Int, onChange: (Int) -> Unit) {
    val colors = Splitty.colors
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        StepperButton(icon = Icons.Filled.Remove, enabled = value > 1) { onChange(value - 1) }
        Text(
            text = "×$value",
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            color = colors.ink,
            style = TextStyle(fontFeatureSettings = "tnum"),
        )
        StepperButton(icon = Icons.Filled.Add, enabled = value < 20) { onChange(value + 1) }
    }
}

@Composable
private fun StepperButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    Box(
        modifier = Modifier
            .size(28.dp)
            .clip(CircleShape)
            .background(colors.ink.copy(alpha = 0.06f))
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = if (enabled) colors.accent else colors.inkSecondary.copy(alpha = 0.4f),
            modifier = Modifier.size(16.dp),
        )
    }
}

/** Подпись остатка/перерасхода под участниками — формулировки как в «По суммам». */
@Composable
private fun SplitStatusLine(status: ItemSplitStatus, currency: String) {
    val colors = Splitty.colors
    val (text, color) = when (status) {
        is ItemSplitStatus.Ok -> stringResource(R.string.item_sheet_split_ok) to colors.accent
        ItemSplitStatus.NoPrice -> stringResource(R.string.item_sheet_split_no_price) to colors.inkSecondary
        ItemSplitStatus.NoParticipants -> stringResource(R.string.item_sheet_split_no_participants) to colors.negative
        is ItemSplitStatus.Under -> stringResource(R.string.item_sheet_split_under, money(status.rest, currency)) to colors.negative
        is ItemSplitStatus.Over -> stringResource(R.string.item_sheet_split_over, money(status.extra, currency)) to colors.negative
    }
    Text(
        text = text,
        modifier = Modifier.fillMaxWidth(),
        fontSize = 13.sp,
        fontWeight = FontWeight.Medium,
        color = color,
        textAlign = TextAlign.Center,
        style = TextStyle(fontFeatureSettings = "tnum"),
    )
}

/**
 * Пикер участника для нераспознанного имени [name]: выбор пишет alias на сервер
 * и применяет доли локально (через [onPick]). Порт iOS `UnknownPickerView`.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UnknownPickerSheet(
    name: String,
    members: List<User>,
    onPick: (Long) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = Splitty.colors
    val haptics = rememberHaptics()
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.bg,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                // Прокрутка: пока нераспознанное имя не сопоставлено, сохранение
                // заблокировано — а в большой группе нужный человек оказывался
                // за нижней кромкой экрана, и выйти из этого было нельзя
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = 20.dp)
                .padding(bottom = 20.dp),
        ) {
            SectionHeader(stringResource(R.string.item_sheet_unknown_title, name))
            Spacer(Modifier.height(12.dp))
            members.forEach { member ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable {
                            haptics.tap()
                            onPick(member.id)
                            onDismiss()
                        }
                        .padding(vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    GradientAvatar(user = member, size = 36.dp)
                    Text(
                        text = member.displayName,
                        fontSize = 15.sp,
                        color = colors.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}
