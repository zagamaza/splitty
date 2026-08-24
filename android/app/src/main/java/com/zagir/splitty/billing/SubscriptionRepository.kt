package com.zagir.splitty.billing

import android.app.Activity
import com.android.billingclient.api.ProductDetails
import com.zagir.splitty.core.model.AiQuota
import com.zagir.splitty.core.model.GooglePurchaseBody
import com.zagir.splitty.core.model.SubscriptionState
import com.zagir.splitty.core.model.Tier
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.SplittyApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Provider
import javax.inject.Singleton

/**
 * Тариф, остаток распознаваний и покупка подписки.
 *
 * Единственный источник правды здесь — **ответы сервера**. Play проводит
 * оплату, но платный человек или нет, решает бэкенд: локальный ответ биллинга
 * на устройстве подменяется, и Plus достался бы бесплатно.
 */
@Singleton
class SubscriptionRepository @Inject constructor(
    private val api: Provider<SplittyApi>,
    private val billing: BillingService,
) {
    private val _tier = MutableStateFlow(Tier.FREE)
    val tier: StateFlow<Tier> = _tier.asStateFlow()

    private val _quota = MutableStateFlow<AiQuota?>(null)
    val quota: StateFlow<AiQuota?> = _quota.asStateFlow()

    private val _subscription = MutableStateFlow<SubscriptionState?>(null)
    val subscription: StateFlow<SubscriptionState?> = _subscription.asStateFlow()

    private val _products = MutableStateFlow<List<ProductDetails>>(emptyList())
    val products: StateFlow<List<ProductDetails>> = _products.asStateFlow()

    val isPlus: Boolean get() = _tier.value == Tier.PLUS

    /** Остаток распознаваний; null — потолка нет или он ещё не известен. */
    val remaining: Int?
        get() = _quota.value?.takeIf { !it.unlimited }?.remaining

    /**
     * Показывать ли подпись у микрофона.
     *
     * Только когда осталось мало: мозолить глаза счётчиком, пока распознаваний
     * вдоволь, — значит превратить рабочий экран в витрину подписки.
     */
    val shouldShowRemaining: Boolean
        get() = remaining?.let { it <= 2 } == true

    init {
        billing.submitToServer = { token, productId -> submitPurchase(token, productId) }
    }

    /** Стартовая загрузка: продукты, тариф с остатком и недоставленные покупки. */
    suspend fun bootstrap() {
        refreshQuota()
        _products.value = billing.loadProducts()
        // Оплата могла пройти, а приложение — закрыться до того, как токен
        // доехал: без этого человек остался бы со списанными деньгами без Plus.
        runCatching { billing.restore() }
    }

    suspend fun refreshQuota() {
        runCatching { api.get().aiQuota() }.onSuccess {
            _quota.value = it
            _tier.value = it.tier
        }
    }

    suspend fun refreshSubscription() {
        runCatching { api.get().subscription() }.onSuccess {
            _subscription.value = it
            _tier.value = it.tier
        }
    }

    /**
     * Применяет квоту, приехавшую в ответе на распознавание.
     *
     * Так счётчик у микрофона обновляется без единого лишнего запроса:
     * значение меняется ровно в момент распознавания.
     */
    fun applyQuota(fresh: AiQuota?) {
        if (fresh == null) return
        _quota.value = fresh
        _tier.value = fresh.tier
    }

    suspend fun loadProducts() {
        if (_products.value.isEmpty()) {
            _products.value = billing.loadProducts()
        }
    }

    /** Покупка выбранного продукта. */
    suspend fun purchase(activity: Activity, product: ProductDetails): PurchaseOutcome {
        val bindingToken = runCatching { api.get().me().purchaseBindingToken }.getOrNull()
        val outcome = billing.purchase(activity, product, bindingToken)
        if (outcome is PurchaseOutcome.Success) {
            refreshQuota()
            refreshSubscription()
        }
        return outcome
    }

    /** Восстановление покупок. */
    suspend fun restore(): Boolean {
        val restored = runCatching { billing.restore() }.getOrDefault(false)
        refreshQuota()
        refreshSubscription()
        return restored && isPlus
    }

    /**
     * Отдаёт токен покупки серверу.
     *
     * Возвращает true и при 409 «чек другого аккаунта»: переотправлять
     * бессмысленно, ответ не изменится, а бесконечный ретрай только скрыл бы
     * проблему от человека, у которого уже списали деньги.
     */
    private suspend fun submitPurchase(token: String, productId: String): Boolean {
        return try {
            val state = api.get().submitGooglePurchase(GooglePurchaseBody(token, productId))
            _subscription.value = state
            _tier.value = state.tier
            refreshQuota()
            true
        } catch (e: ApiException) {
            e.isReceiptBoundToOtherAccount
        } catch (_: Exception) {
            false
        }
    }
}
