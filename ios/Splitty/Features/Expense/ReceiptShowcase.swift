#if DEBUG
import SwiftUI

/// Витрина чека для съёмки вёрстки без сервера: `SPLITTY_RECEIPT_SHOWCASE=1`
/// открывает ReceiptView на заготовке вместо приложения. Нужна для случаев,
/// которых тестовая туса не даёт: дробная цена шире листа, длинное название,
/// сбор и подвал с тем же итогом.
struct ReceiptShowcase: View {
    static var isRequested: Bool {
        ProcessInfo.processInfo.environment["SPLITTY_RECEIPT_SHOWCASE"] != nil
    }

    private let members = [
        User(id: 1, username: nil, displayName: "Константин Константинопольский"),
        User(id: 2, username: nil, displayName: "Алмаз"),
    ]

    private var items: [OperationItem] {
        [
            OperationItem(
                name: "Хачапури по-аджарски с двойным сыром, яйцом и сливочным маслом",
                price: 1_234_568, priceMinor: 123_456_789,
                shares: [ItemShare(userId: 1, weight: 1, amount: nil), ItemShare(userId: 2, weight: 2, amount: nil)]
            ),
            OperationItem(
                name: "Вино", price: 2400, qty: 2,
                shares: [ItemShare(userId: 1, weight: 1, amount: nil), ItemShare(userId: 2, weight: 1, amount: nil)]
            ),
            OperationItem(
                name: "Обслуживание", price: 123_697, priceMinor: 12_369_679,
                kind: OperationItem.kindSurcharge, split: OperationItem.splitProportional, percent: 10
            ),
        ]
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                ReceiptView(items: items, members: members, currency: "RUB")
                ReceiptView(items: items, members: members, currency: "RUB", embedded: true)
                    .padding(22)
                    .receiptPaper(padding: 0)
            }
            .padding(16)
        }
        .background(Color.bg)
    }
}
#endif
