import Foundation
import StoreKit

/// Состояние тарифа и остатка распознаваний — то, на что смотрят экраны.
///
/// Единственный источник правды здесь — **ответы сервера**. StoreKit проводит
/// оплату, но платный человек или нет, решает бэкенд: локальное состояние
/// покупок подменяется на устройстве, и Plus доставался бы бесплатно.
@MainActor
@Observable
final class SubscriptionStore {
    private(set) var tier: Tier = .free
    private(set) var quota: AiQuota?
    private(set) var subscription: SubscriptionState?
    /// Последняя ошибка покупки, показывается на экране оплаты.
    var purchaseError: String?

    let storeKit = StoreKitService()
    private let api: APIClient

    var isPlus: Bool { tier == .plus }

    /// Остаток распознаваний. nil — потолка нет или он ещё не известен.
    var remaining: Int? {
        guard let quota, !quota.unlimited else { return nil }
        return quota.remaining
    }

    /// Показывать ли подпись у микрофона.
    ///
    /// Только когда осталось мало: мозолить глаза счётчиком, пока распознаваний
    /// вдоволь, — значит превратить рабочий экран в напоминание о лимите.
    var shouldShowRemaining: Bool {
        guard let remaining else { return false }
        return remaining <= 2
    }

    init(api: APIClient) {
        self.api = api
        storeKit.submitToServer = { [weak self] jws in
            await self?.submitAppleReceipt(jws) ?? false
        }
    }

    /// Стартовая загрузка: продукты магазина и текущие тариф с остатком.
    func bootstrap() async {
        storeKit.startObservingTransactions()
        async let products: Void = storeKit.loadProducts()
        async let quota: Void = refreshQuota()
        _ = await (products, quota)
        // Транзакции, не доехавшие до сервера в прошлый раз (оплата прошла, а
        // сеть отвалилась): без этого человек остался бы со списанными деньгами
        // и без Plus.
        await storeKit.retryUnconfirmed()
    }

    /// Перечитывает остаток и тариф с сервера.
    func refreshQuota() async {
        guard let fresh = try? await api.aiQuota() else { return }
        quota = fresh
        tier = fresh.tier
    }

    /// Применяет квоту, приехавшую в ответе на распознавание.
    ///
    /// Так счётчик у микрофона обновляется без единого лишнего запроса:
    /// значение меняется ровно в момент распознавания.
    func apply(quota fresh: AiQuota?) {
        guard let fresh else { return }
        quota = fresh
        tier = fresh.tier
    }

    func refreshSubscription() async {
        subscription = try? await api.subscription()
    }

    /// Покупка выбранного продукта.
    func purchase(_ product: Product) async -> PurchaseOutcome {
        purchaseError = nil
        let bindingToken = try? await api.me().purchaseBindingToken
        let outcome = await storeKit.purchase(product, bindingToken: bindingToken)

        switch outcome {
        case .success:
            await refreshQuota()
            await refreshSubscription()
        case .pendingServer:
            purchaseError = String(localized: "Оплата прошла, но подтвердить её пока не удалось. Проверьте связь — доступ появится сам")
        case .pendingApproval:
            purchaseError = String(localized: "Покупка ждёт подтверждения")
        case .cancelled:
            break
        case .failed(let error):
            purchaseError = error?.localizedDescription
                ?? String(localized: "Не удалось совершить покупку")
        }
        return outcome
    }

    /// Восстановление покупок (требование Guideline 3.1.1).
    func restore() async -> Bool {
        purchaseError = nil
        let restored = await storeKit.restore()
        await refreshQuota()
        await refreshSubscription()
        if !restored && tier != .plus {
            purchaseError = String(localized: "Покупок для восстановления не нашлось")
        }
        return tier == .plus
    }

    private func submitAppleReceipt(_ jws: String) async -> Bool {
        guard !jws.isEmpty else { return false }
        do {
            let state = try await api.submitAppleReceipt(jws: jws)
            subscription = state
            tier = state.tier
            await refreshQuota()
            return true
        } catch {
            if let apiError = error as? APIError, apiError.isReceiptBoundToOtherAccount {
                // Тупик, из которого сам человек не выберется: чек оформлен на
                // другой аккаунт Splitor. Молчать здесь нельзя — деньги списаны.
                purchaseError = error.localizedDescription
                // true: переотправлять бессмысленно, ответ не изменится.
                return true
            }
            return false
        }
    }
}
