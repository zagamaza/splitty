import Foundation
import StoreKit

/// Покупка подписки Splitor Plus через StoreKit 2.
///
/// Главное правило: **тариф определяет сервер, а не StoreKit**. Здесь только
/// проводится покупка и добывается подписанная транзакция; платный человек или
/// нет — решает бэкенд, проверив чек у Apple. Верить локальному
/// `Transaction.currentEntitlements` нельзя: на устройстве с джейлбрейком его
/// подменяют, и Plus достаётся бесплатно.
@MainActor
@Observable
final class StoreKitService {
    /// Идентификаторы продуктов. Совпадают с App Store Connect и с белым
    /// списком на сервере (`PLUS_PRODUCT_IDS`) — расходиться им нельзя.
    static let monthlyId = "com.zagir.splitty.plus.monthly"
    static let yearlyId = "com.zagir.splitty.plus.yearly"
    static let productIds = [monthlyId, yearlyId]

    private(set) var products: [Product] = []
    private(set) var isLoadingProducts = false
    /// Идёт покупка: экран блокирует повторное нажатие.
    private(set) var purchasingProductId: String?

    /// Транзакции, которые сервер ещё НЕ подтвердил.
    ///
    /// Копятся здесь, а не финишируются сразу: `transaction.finish()` говорит
    /// Apple «доставлено», и после него транзакция больше не придёт в
    /// `Transaction.updates`. Отфинишить её до записи на сервере — значит
    /// потерять оплаченный доступ, если запрос не дошёл.
    private var unconfirmed: [UInt64: PendingTransaction] = [:]

    private var updatesTask: Task<Void, Never>?

    /// Вызывается, когда появилась подписанная транзакция, которую надо отдать
    /// серверу. Возвращает true, если сервер её принял, — только тогда
    /// транзакция финишируется.
    var submitToServer: ((String) async -> Bool)?

    /// Останавливает слушатель. Отдельный метод, а не deinit: deinit не
    /// изолирован в MainActor и трогать оттуда изолированное свойство нельзя.
    func stopObservingTransactions() {
        updatesTask?.cancel()
        updatesTask = nil
    }

    /// Запускает слушатель транзакций.
    ///
    /// Звать один раз при старте: сюда приходят покупки, совершённые вне
    /// приложения (сменил тариф в настройках iOS), продления и всё, что не
    /// удалось доставить в прошлый запуск. Без него оплаченная подписка может
    /// не доехать до сервера никогда.
    func startObservingTransactions() {
        guard updatesTask == nil else { return }
        updatesTask = Task { [weak self] in
            for await result in StoreKit.Transaction.updates {
                await self?.handle(result)
            }
        }
    }

    func loadProducts() async {
        guard products.isEmpty, !isLoadingProducts else { return }
        isLoadingProducts = true
        defer { isLoadingProducts = false }
        do {
            let loaded = try await Product.products(for: Self.productIds)
            // Порядок фиксируем сами: App Store отдаёт продукты в произвольном,
            // а на экране год обязан стоять первым (он выбран по умолчанию).
            products = loaded.sorted { lhs, _ in lhs.id == Self.yearlyId }
        } catch {
            products = []
        }
    }

    /// Покупка. `bindingToken` — токен привязки к аккаунту Splitor: он уезжает
    /// в чек и позволяет серверу убедиться, что покупка именно этого человека.
    ///
    /// Возвращает `true`, только если сервер подтвердил покупку. Локального
    /// «успеха» StoreKit недостаточно: платным делает запись на сервере.
    func purchase(_ product: Product, bindingToken: String?) async -> PurchaseOutcome {
        purchasingProductId = product.id
        defer { purchasingProductId = nil }

        var options: Set<Product.PurchaseOption> = []
        if let bindingToken, let uuid = UUID(uuidString: bindingToken) {
            options.insert(.appAccountToken(uuid))
        }

        do {
            let result = try await product.purchase(options: options)
            switch result {
            case .success(let verification):
                let accepted = await handle(verification)
                return accepted ? .success : .pendingServer
            case .userCancelled:
                return .cancelled
            case .pending:
                // Ждёт одобрения родителя (Ask to Buy) — деньги ещё не списаны.
                return .pendingApproval
            @unknown default:
                return .failed(nil)
            }
        } catch {
            return .failed(error)
        }
    }

    /// Восстановление покупок. Требование Guideline 3.1.1: без этой кнопки
    /// подписку не пропустят на ревью.
    func restore() async -> Bool {
        var restoredAny = false
        for await result in StoreKit.Transaction.currentEntitlements {
            if await handle(result) { restoredAny = true }
        }
        return restoredAny
    }

    /// Повторная отправка того, что сервер ещё не принял.
    ///
    /// Зовётся при возврате в приложение: сеть могла отвалиться ровно между
    /// оплатой и записью на сервере, и человек остался бы со списанными деньгами
    /// без Plus.
    func retryUnconfirmed() async {
        for (_, pending) in unconfirmed {
            await submit(pending)
        }
    }

    // MARK: - Внутреннее

    @discardableResult
    private func handle(_ result: VerificationResult<StoreKit.Transaction>) async -> Bool {
        guard case .verified(let transaction) = result else {
            // Подпись не сошлась — на сервер такое не отправляем: он всё равно
            // отвергнет, а «отправлено» скрыло бы проблему.
            return false
        }
        // jwsRepresentation берётся у VerificationResult, а не у транзакции:
        // серверу нужен именно ПОДПИСАННЫЙ Apple чек, по которому он проверит
        // цепочку сертификатов. jsonRepresentation — разобранные поля без
        // подписи, доверять им нельзя ни на грамм.
        let pending = PendingTransaction(transaction: transaction, jws: result.jwsRepresentation)
        unconfirmed[transaction.id] = pending
        return await submit(pending)
    }

    @discardableResult
    private func submit(_ pending: PendingTransaction) async -> Bool {
        guard let submitToServer else { return false }

        let accepted = await submitToServer(pending.jws)
        guard accepted else { return false }

        // Только теперь: сервер записал подписку, потерять её больше нельзя.
        await pending.transaction.finish()
        unconfirmed[pending.transaction.id] = nil
        return true
    }
}

/// Транзакция вместе с её подписанным представлением: до подтверждения сервером
/// нужны оба — чек, чтобы отправить, и сама транзакция, чтобы отфинишить.
private struct PendingTransaction {
    let transaction: StoreKit.Transaction
    let jws: String
}

/// Чем закончилась попытка покупки.
enum PurchaseOutcome: Equatable {
    /// Куплено и записано на сервере — Plus уже действует.
    case success
    /// Оплата прошла, но сервер её ещё не принял. Деньги списаны, доступ
    /// появится, как только транзакция доедет (её переотправит `retryUnconfirmed`).
    case pendingServer
    /// Ждёт одобрения (Ask to Buy): деньги не списаны.
    case pendingApproval
    case cancelled
    case failed(Error?)

    static func == (lhs: PurchaseOutcome, rhs: PurchaseOutcome) -> Bool {
        switch (lhs, rhs) {
        case (.success, .success), (.pendingServer, .pendingServer),
             (.pendingApproval, .pendingApproval), (.cancelled, .cancelled):
            return true
        case (.failed, .failed):
            return true
        default:
            return false
        }
    }
}
