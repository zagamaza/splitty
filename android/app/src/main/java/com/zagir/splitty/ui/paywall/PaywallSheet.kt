package com.zagir.splitty.ui.paywall

import android.app.Activity
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDownward
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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

/**
 * Экран оплаты Splitor Plus.
 *
 * Открывается ровно там, где человек упёрся: распознавания на сегодня
 * кончились. Поэтому герой экрана — не список преимуществ, а сам момент,
 * который только что не состоялся: сказанная фраза, превращающаяся в готовый
 * расход. Показать продукт честнее, чем пообещать его словами.
 *
 * Обязательное по политике подписок (цена, период, автопродление,
 * восстановление покупок, ссылки на условия и политику) — не формальность
 * внизу экрана, а причина, по которой подписку вообще пропустят на ревью.
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

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = colors.bg,
    ) {
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            Text(
                text = stringResource(R.string.plus_title),
                color = colors.ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
            )

            HeroCard()

            if (quota != null && !quota.unlimited) {
                ReceiptStubs(
                    limit = quota.limit,
                    used = quota.used,
                    modifier = Modifier.fillMaxWidth(),
                )
            }

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

            TextButton(onClick = onDismiss, modifier = Modifier.fillMaxWidth()) {
                Text(
                    text = stringResource(R.string.plus_add_manually),
                    color = colors.inkSecondary,
                    fontSize = 14.sp,
                )
            }

            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                TextButton(onClick = onRestore) {
                    Text(stringResource(R.string.plus_restore), color = colors.accentText)
                }
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

/** Момент, ради которого платят: фраза превращается в готовый расход. */
@Composable
private fun HeroCard() {
    val colors = Splitty.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(20.dp))
            .background(colors.receiptPaper)
            .padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Filled.Mic, contentDescription = null, tint = colors.accent)
            Text(
                text = stringResource(R.string.plus_hero_phrase),
                color = colors.ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.Medium,
            )
        }

        Icon(
            Icons.Filled.ArrowDownward,
            contentDescription = null,
            tint = colors.inkSecondary,
            modifier = Modifier.align(Alignment.CenterHorizontally).size(16.dp),
        )

        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(Modifier.fillMaxWidth(), Arrangement.SpaceBetween) {
                Text(
                    stringResource(R.string.plus_hero_expense),
                    color = colors.ink,
                    fontSize = 17.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                Text("3 200 ₽", color = colors.ink, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
            }
            HorizontalDivider(color = colors.hairline)
            Row(Modifier.fillMaxWidth(), Arrangement.SpaceBetween) {
                Text(
                    stringResource(R.string.plus_hero_split),
                    color = colors.inkSecondary,
                    fontSize = 14.sp,
                )
                Text("по 800 ₽", color = colors.accentText, fontSize = 14.sp, fontWeight = FontWeight.Medium)
            }
        }
    }
}

@Composable
private fun PlanRow(
    product: ProductDetails,
    isSelected: Boolean,
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
            .clip(RoundedCornerShape(16.dp))
            .background(colors.surface)
            .clickable(onClick = onClick)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier
                .size(20.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(if (isSelected) colors.accent else colors.hairline)
        )
        Column(Modifier.weight(1f)) {
            Text(
                text = product.title.substringBefore(" ("),
                color = colors.ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(product.priceText(), color = colors.inkSecondary, fontSize = 14.sp)
        }
        if (discount != null) {
            Box(
                Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(colors.accent)
                    .padding(horizontal = 10.dp, vertical = 5.dp)
            ) {
                Text(discount, color = colors.surface, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

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
