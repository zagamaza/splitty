package com.zagir.splitty.billing

import android.app.Activity
import android.content.Context
import com.android.billingclient.api.AcknowledgePurchaseParams
import com.android.billingclient.api.BillingClient
import com.android.billingclient.api.BillingClientStateListener
import com.android.billingclient.api.BillingFlowParams
import com.android.billingclient.api.BillingResult
import com.android.billingclient.api.ProductDetails
import com.android.billingclient.api.Purchase
import com.android.billingclient.api.PurchasesUpdatedListener
import com.android.billingclient.api.QueryProductDetailsParams
import com.android.billingclient.api.QueryPurchasesParams
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.suspendCancellableCoroutine
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume

/**
 * Покупка подписки Splitor Plus через Google Play Billing.
 *
 * Главное правило: **тариф определяет сервер, а не Play**. Здесь только
 * проводится оплата и добывается `purchaseToken`; платный человек или нет —
 * решает бэкенд, проверив токен у Google. Верить локальному ответу биллинга
 * нельзя: пропатченный apk подменяет его, и Plus достаётся бесплатно.
 *
 * ⚠️ Подтверждение покупки (`acknowledge`) делает СЕРВЕР после проверки токена.
 * Не подтвердив покупку за трое суток, Google откатывает её и возвращает
 * деньги, поэтому подтверждение обязано пережить закрытие приложения — на
 * клиенте ему места нет.
 */
@Singleton
class BillingService @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    companion object {
        const val MONTHLY_ID = "com.zagir.splitty.plus.monthly"
        const val YEARLY_ID = "com.zagir.splitty.plus.yearly"
        val PRODUCT_IDS = listOf(YEARLY_ID, MONTHLY_ID)
    }

    /** Вызывается с токеном покупки: должна вернуть true, если сервер её принял. */
    var submitToServer: (suspend (token: String, productId: String) -> Boolean)? = null

    private var pendingPurchase: CompletableDeferred<PurchaseOutcome>? = null

    private val purchasesListener = PurchasesUpdatedListener { result, purchases ->
        when (result.responseCode) {
            BillingClient.BillingResponseCode.OK -> {
                val list = purchases.orEmpty()
                if (list.isEmpty()) {
                    pendingPurchase?.complete(PurchaseOutcome.Failed(null))
                } else {
                    // Обработка асинхронная, а ожидающему ответ нужен сейчас:
                    // подтверждение придёт из handlePurchases при следующем
                    // обращении, а покупка уже не потеряется — Play отдаст её
                    // в queryPurchases при следующем запуске.
                    pendingPurchase?.complete(PurchaseOutcome.PendingServer(list))
                }
            }
            BillingClient.BillingResponseCode.USER_CANCELED ->
                pendingPurchase?.complete(PurchaseOutcome.Cancelled)
            else ->
                pendingPurchase?.complete(PurchaseOutcome.Failed(result.debugMessage))
        }
        pendingPurchase = null
    }

    private val client: BillingClient = BillingClient.newBuilder(context)
        .setListener(purchasesListener)
        .enablePendingPurchases()
        .build()

    private suspend fun ensureConnected(): Boolean {
        if (client.isReady) return true
        return suspendCancellableCoroutine { cont ->
            client.startConnection(object : BillingClientStateListener {
                override fun onBillingSetupFinished(result: BillingResult) {
                    if (cont.isActive) {
                        cont.resume(result.responseCode == BillingClient.BillingResponseCode.OK)
                    }
                }

                override fun onBillingServiceDisconnected() {
                    if (cont.isActive) cont.resume(false)
                }
            })
        }
    }

    /** Загружает описания продуктов. Пустой список — биллинг недоступен. */
    suspend fun loadProducts(): List<ProductDetails> {
        if (!ensureConnected()) return emptyList()

        val params = QueryProductDetailsParams.newBuilder()
            .setProductList(
                PRODUCT_IDS.map {
                    QueryProductDetailsParams.Product.newBuilder()
                        .setProductId(it)
                        .setProductType(BillingClient.ProductType.SUBS)
                        .build()
                }
            )
            .build()

        return suspendCancellableCoroutine { cont ->
            client.queryProductDetailsAsync(params) { result, details ->
                if (!cont.isActive) return@queryProductDetailsAsync
                if (result.responseCode == BillingClient.BillingResponseCode.OK) {
                    // Порядок задаём сами: год первым, он выбран по умолчанию.
                    cont.resume(details.sortedBy { PRODUCT_IDS.indexOf(it.productId) })
                } else {
                    cont.resume(emptyList())
                }
            }
        }
    }

    /**
     * Запускает покупку.
     *
     * [bindingToken] — токен привязки к аккаунту Splitor: он уезжает в покупку
     * (`obfuscatedAccountId`) и позволяет серверу убедиться, что она именно
     * этого человека. Без него чек достаётся тому, кто первый его пришлёт.
     */
    suspend fun purchase(
        activity: Activity,
        product: ProductDetails,
        bindingToken: String?,
    ): PurchaseOutcome {
        if (!ensureConnected()) return PurchaseOutcome.Failed("биллинг недоступен")

        val offerToken = product.subscriptionOfferDetails?.firstOrNull()?.offerToken
            ?: return PurchaseOutcome.Failed("у продукта нет предложения")

        val params = BillingFlowParams.newBuilder()
            .setProductDetailsParamsList(
                listOf(
                    BillingFlowParams.ProductDetailsParams.newBuilder()
                        .setProductDetails(product)
                        .setOfferToken(offerToken)
                        .build()
                )
            )
            .apply { if (!bindingToken.isNullOrEmpty()) setObfuscatedAccountId(bindingToken) }
            .build()

        val deferred = CompletableDeferred<PurchaseOutcome>()
        pendingPurchase = deferred

        val launch = client.launchBillingFlow(activity, params)
        if (launch.responseCode != BillingClient.BillingResponseCode.OK) {
            pendingPurchase = null
            return PurchaseOutcome.Failed(launch.debugMessage)
        }

        return when (val outcome = deferred.await()) {
            is PurchaseOutcome.PendingServer -> {
                if (handlePurchases(outcome.purchases)) PurchaseOutcome.Success else outcome
            }
            else -> outcome
        }
    }

    /**
     * Досылает на сервер покупки, о которых он мог не узнать.
     *
     * Зовётся на старте: оплата могла пройти, а приложение — закрыться до того,
     * как токен доехал. Без этого человек остался бы со списанными деньгами и
     * без Plus.
     */
    suspend fun restore(): Boolean {
        if (!ensureConnected()) return false

        val params = QueryPurchasesParams.newBuilder()
            .setProductType(BillingClient.ProductType.SUBS)
            .build()

        val purchases = suspendCancellableCoroutine<List<Purchase>> { cont ->
            client.queryPurchasesAsync(params) { result, list ->
                if (!cont.isActive) return@queryPurchasesAsync
                cont.resume(
                    if (result.responseCode == BillingClient.BillingResponseCode.OK) list
                    else emptyList()
                )
            }
        }
        return handlePurchases(purchases)
    }

    /** Отдаёт серверу токены купленных подписок. */
    private suspend fun handlePurchases(purchases: List<Purchase>): Boolean {
        val submit = submitToServer ?: return false
        var acceptedAny = false

        for (purchase in purchases) {
            if (purchase.purchaseState != Purchase.PurchaseState.PURCHASED) continue
            val productId = purchase.products.firstOrNull() ?: continue
            if (submit(purchase.purchaseToken, productId)) {
                acceptedAny = true
            }
        }
        return acceptedAny
    }

    /**
     * Резервное подтверждение покупки на клиенте.
     *
     * Обычно подтверждает сервер — он один переживает закрытие приложения. Это
     * страховка на случай, когда сервер принял покупку, но подтвердить её у
     * Google не смог: три дня без подтверждения — и деньги возвращаются.
     */
    suspend fun acknowledgeLocally(purchaseToken: String) {
        if (!ensureConnected()) return
        val params = AcknowledgePurchaseParams.newBuilder()
            .setPurchaseToken(purchaseToken)
            .build()
        suspendCancellableCoroutine<Unit> { cont ->
            client.acknowledgePurchase(params) { if (cont.isActive) cont.resume(Unit) }
        }
    }
}

/** Чем закончилась попытка покупки. */
sealed interface PurchaseOutcome {
    /** Куплено и записано на сервере — Plus уже действует. */
    data object Success : PurchaseOutcome

    /**
     * Оплата прошла, но сервер её ещё не принял. Деньги списаны; доступ
     * появится, как только токен доедет (Play отдаст покупку в `restore`).
     */
    data class PendingServer(val purchases: List<Purchase>) : PurchaseOutcome

    data object Cancelled : PurchaseOutcome

    data class Failed(val message: String?) : PurchaseOutcome
}
