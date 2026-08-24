package com.zagir.splitty.ui.paywall

import android.app.Activity
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AllInclusive
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.android.billingclient.api.ProductDetails
import com.zagir.splitty.R
import com.zagir.splitty.billing.BillingService
import com.zagir.splitty.core.model.AiQuota
import com.zagir.splitty.ui.theme.Splitty
import java.text.NumberFormat
import java.time.Duration
import java.time.Instant
import java.util.Currency
import kotlin.math.ceil

/**
 * Экран оплаты Splitor Plus.
 *
 * Открывается там, где человек упёрся: распознавания на сегодня кончились.
 * Поэтому первое, что он читает, — что именно произошло и что снимает подписка;
 * дальше идёт сам момент, ради которого платят, и только потом тарифы.
 *
 * Годовой тариф продаёт не его цена, а цена ЗА МЕСЯЦ при годовой оплате: «$19.99»
 * рядом с «$2.99» выглядит дороже, хотя вдвое дешевле.
 *
 * Обязательное по политике подписок (цена, период, автопродление, восстановление
 * покупок, ссылки на условия и политику) — не формальность внизу, а причина, по
 * которой подписку вообще пропустят на ревью.
 *
 * Порт iOS `PaywallView` — оформление обеих платформ обязано совпадать.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PaywallSheet(
    quota: AiQuota?,
    products: List<ProductDetails>,
    isPurchasing: Boolean,
    isLoadingProducts: Boolean,
    errorMessage: String?,
    onPurchase: (Activity, ProductDetails) -> Unit,
    onRestore: () -> Unit,
    onOpenUrl: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = Splitty.colors
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val context = LocalContext.current
    var selectedId by remember { mutableStateOf(BillingService.YEARLY_ID) }

    val selected = products.firstOrNull { it.productId == selectedId } ?: products.firstOrNull()
    val outOfQuota = quota != null && !quota.unlimited && quota.remaining <= 0

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = colors.bg,
    ) {
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp)
                .padding(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            Headline(outOfQuota, quota)

            HeroCard()

            Benefits()

            when {
                isLoadingProducts && products.isEmpty() ->
                    Box(Modifier.fillMaxWidth().padding(24.dp), Alignment.Center) {
                        CircularProgressIndicator(color = colors.accent)
                    }
                products.isEmpty() ->
                    Text(
                        text = stringResource(R.string.plus_products_failed),
                        color = colors.inkSecondary,
                        fontSize = 15.sp,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.fillMaxWidth().padding(24.dp),
                    )
                else -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    products.forEach { product ->
                        PlanRow(
                            product = product,
                            isSelected = product.productId == selectedId,
                            perMonth = perMonthText(product),
                            discount = discountBadge(product, products),
                            onClick = { selectedId = product.productId },
                        )
                    }
                }
            }

            if (errorMessage != null) {
                Text(
                    text = errorMessage,
                    color = colors.negativeText,
                    fontSize = 14.sp,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            Button(
                onClick = {
                    val activity = context as? Activity ?: return@Button
                    selected?.let { onPurchase(activity, it) }
                },
                enabled = selected != null && !isPurchasing,
                colors = ButtonDefaults.buttonColors(containerColor = colors.accent),
                shape = RoundedCornerShape(27.dp),
                modifier = Modifier.fillMaxWidth().height(54.dp),
            ) {
                if (isPurchasing) {
                    CircularProgressIndicator(
                        color = colors.surface,
                        modifier = Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                    )
                } else {
                    Text(
                        text = stringResource(R.string.plus_buy, selected?.priceText().orEmpty()),
                        fontSize = 17.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            }

            // Обязательное раскрытие: без явного «продлевается автоматически»
            // подписку заворачивают на ревью.
            Text(
                text = renewalNotice(selected),
                color = colors.inkSecondary,
                fontSize = 12.sp,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = stringResource(R.string.plus_restore),
                    color = colors.inkSecondary,
                    fontSize = 13.sp,
                    modifier = Modifier.clickable(onClick = onRestore),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    Text(
                        text = stringResource(R.string.plus_terms),
                        color = colors.inkSecondary,
                        fontSize = 13.sp,
                        modifier = Modifier.clickable {
                            onOpenUrl("https://splitor.zagirnur.dev/terms")
                        },
                    )
                    Text(
                        text = stringResource(R.string.plus_privacy),
                        color = colors.inkSecondary,
                        fontSize = 13.sp,
                        modifier = Modifier.clickable {
                            onOpenUrl("https://splitor.zagirnur.dev/privacy")
                        },
                    )
                }
            }
        }
    }
}

/**
 * Первое, что читает человек. Текст зависит от того, как он сюда попал: упёрся в
 * лимит — говорим, что кончилось и когда вернётся; пришёл сам — что даёт тариф.
 */
@Composable
private fun Headline(outOfQuota: Boolean, quota: AiQuota?) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            text = stringResource(R.string.plus_title),
            color = colors.accentText,
            fontSize = 13.sp,
            fontWeight = FontWeight.Bold,
        )
        Text(
            text = stringResource(
                if (outOfQuota) R.string.plus_headline_out else R.string.plus_headline
            ),
            color = colors.ink,
            fontSize = 28.sp,
            fontWeight = FontWeight.Bold,
        )
        Text(text = subtitle(outOfQuota, quota), color = colors.inkSecondary, fontSize = 15.sp)
    }
}

@Composable
private fun subtitle(outOfQuota: Boolean, quota: AiQuota?): String {
    if (!outOfQuota || quota == null) return stringResource(R.string.plus_subtitle)
    val hours = quota.hoursUntilReset()
    return if (hours != null && hours >= 1) {
        stringResource(R.string.plus_subtitle_reset, hours)
    } else {
        stringResource(R.string.plus_subtitle_reset_soon)
    }
}

/** Часов до возврата бесплатных распознаваний; null — если сервер не прислал срок. */
private fun AiQuota.hoursUntilReset(): Int? {
    val at = resetsAt ?: return null
    val instant = runCatching { Instant.parse(at) }.getOrNull() ?: return null
    val seconds = Duration.between(Instant.now(), instant).seconds
    return if (seconds <= 0) null else ceil(seconds / 3600.0).toInt()
}

/**
 * Три строки вместо пустоты между чеком и тарифами.
 *
 * Последняя — про бесплатное — не рекламная: на ревью подписку заворачивают,
 * когда непонятно, что именно платное, а что и так работает.
 */
@Composable
private fun Benefits() {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        BenefitRow(Icons.Filled.AllInclusive, R.string.plus_benefit_unlimited)
        BenefitRow(Icons.Filled.Mic, R.string.plus_benefit_input)
        BenefitRow(Icons.Filled.CheckCircle, R.string.plus_benefit_free)
    }
}

@Composable
private fun BenefitRow(icon: ImageVector, text: Int) {
    val colors = Splitty.colors
    Row(
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Icon(icon, contentDescription = null, tint = colors.accent, modifier = Modifier.size(20.dp))
        Text(stringResource(text), color = colors.ink, fontSize = 15.sp)
    }
}

/** Момент, ради которого платят: фраза превращается в готовый расход. */
@Composable
private fun HeroCard() {
    val colors = Splitty.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(colors.receiptPaper)
            .border(1.dp, colors.hairline, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                Icons.Filled.Mic,
                contentDescription = null,
                tint = colors.accent,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = stringResource(R.string.plus_hero_phrase),
                color = colors.ink,
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
            )
        }

        HorizontalDivider(color = colors.hairline)

        Row(Modifier.fillMaxWidth(), Arrangement.SpaceBetween) {
            Text(
                stringResource(R.string.plus_hero_expense),
                color = colors.ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text("3 200 ₽", color = colors.ink, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
        }
        Row(Modifier.fillMaxWidth(), Arrangement.SpaceBetween) {
            Text(
                stringResource(R.string.plus_hero_split),
                color = colors.inkSecondary,
                fontSize = 13.sp,
            )
            Text("по 800 ₽", color = colors.accentText, fontSize = 13.sp, fontWeight = FontWeight.Medium)
        }
    }
}

@Composable
private fun PlanRow(
    product: ProductDetails,
    isSelected: Boolean,
    perMonth: String?,
    discount: String?,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    val border = if (isSelected) {
        BorderStroke(2.dp, colors.accent)
    } else {
        BorderStroke(1.dp, colors.hairline)
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(colors.surface)
            .border(border, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier
                .size(22.dp)
                .clip(CircleShape)
                .border(2.dp, if (isSelected) colors.accent else colors.hairline, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            if (isSelected) {
                Box(Modifier.size(12.dp).clip(CircleShape).background(colors.accent))
            }
        }

        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(planTitle(product)),
                    color = colors.ink,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                if (discount != null) {
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .background(colors.accent)
                            .padding(horizontal = 7.dp, vertical = 3.dp)
                    ) {
                        Text(discount, color = colors.surface, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    }
                }
            }
            if (perMonth != null) {
                Text(perMonth, color = colors.inkSecondary, fontSize = 13.sp)
            }
        }

        Spacer(Modifier.width(8.dp))

        Text(
            text = product.priceText(),
            color = colors.ink,
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

/**
 * Название периода своими словами: `title` продукта приходит из Play Console и
 * там не локализовано — на русском экране торчало «Yearly».
 */
private fun planTitle(product: ProductDetails): Int =
    if (product.productId == BillingService.YEARLY_ID) R.string.plus_plan_year else R.string.plus_plan_month

/** Цена продукта в валюте витрины покупателя. */
private fun ProductDetails.priceText(): String =
    subscriptionOfferDetails
        ?.firstOrNull()
        ?.pricingPhases
        ?.pricingPhaseList
        ?.lastOrNull()
        ?.formattedPrice
        .orEmpty()

private fun ProductDetails.priceMicros(): Long =
    subscriptionOfferDetails
        ?.firstOrNull()
        ?.pricingPhases
        ?.pricingPhaseList
        ?.lastOrNull()
        ?.priceAmountMicros
        ?: 0L

private fun ProductDetails.currencyCode(): String =
    subscriptionOfferDetails
        ?.firstOrNull()
        ?.pricingPhases
        ?.pricingPhaseList
        ?.lastOrNull()
        ?.priceCurrencyCode
        .orEmpty()

/**
 * Цена за месяц при годовой оплате — то, что делает годовой тариф понятным.
 * Без неё «$19.99» рядом с «$2.99» читается как «дороже».
 */
@Composable
private fun perMonthText(product: ProductDetails): String? {
    if (product.productId != BillingService.YEARLY_ID) return null
    val micros = product.priceMicros()
    if (micros <= 0) return null
    val currency = runCatching { Currency.getInstance(product.currencyCode()) }.getOrNull()
        ?: return null
    val format = NumberFormat.getCurrencyInstance().also { it.currency = currency }
    return stringResource(R.string.plus_per_month, format.format(micros / 12.0 / 1_000_000.0))
}

/**
 * Скидка годового относительно месячного.
 *
 * Считается ТОЛЬКО когда валюты обоих продуктов совпадают: Play отдаёт цены в
 * валюте витрины покупателя, и вычесть рубли из долларов значит показать
 * выдуманный процент. Не сходится — бейджа просто нет.
 */
private fun discountBadge(product: ProductDetails, all: List<ProductDetails>): String? {
    if (product.productId != BillingService.YEARLY_ID) return null
    val monthly = all.firstOrNull { it.productId == BillingService.MONTHLY_ID } ?: return null
    if (monthly.currencyCode() != product.currencyCode()) return null

    val yearAtMonthlyRate = monthly.priceMicros() * 12
    val yearly = product.priceMicros()
    if (yearAtMonthlyRate <= 0 || yearly >= yearAtMonthlyRate) return null

    val percent = ((yearAtMonthlyRate - yearly) * 100.0 / yearAtMonthlyRate).toInt()
    return if (percent >= 5) "−$percent%" else null
}

@Composable
private fun renewalNotice(product: ProductDetails?): String {
    if (product == null) return stringResource(R.string.plus_renewal_notice, "", "")
    val period = if (product.productId == BillingService.YEARLY_ID) {
        stringResource(R.string.plus_period_year)
    } else {
        stringResource(R.string.plus_period_month)
    }
    return stringResource(R.string.plus_renewal_notice, product.priceText(), period)
}
