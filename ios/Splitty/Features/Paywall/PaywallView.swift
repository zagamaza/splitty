import StoreKit
import SwiftUI

/// Экран оплаты Splitor Plus.
///
/// Открывается ровно там, где человек упёрся: распознавания на сегодня
/// кончились. Поэтому герой экрана — не список преимуществ, а сам момент,
/// который только что не состоялся: сказанная фраза, превращающаяся в готовый
/// расход. Показать продукт честнее, чем пообещать его словами.
///
/// Обязательное по Guideline 3.1.2 (цена, период, автопродление, восстановление
/// покупок, ссылки на условия и политику) — не формальность внизу экрана, а
/// причина, по которой подписку вообще пропустят на ревью.
struct PaywallView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    let store: SubscriptionStore
    /// Остаток на момент открытия: экран показывает, что именно закончилось.
    var quota: AiQuota?

    @State private var selectedProductId = StoreKitService.yearlyId
    @State private var isRestoring = false

    private var products: [Product] { store.storeKit.products }
    private var selectedProduct: Product? {
        products.first { $0.id == selectedProductId } ?? products.first
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 24) {
                    heroCard
                    if let quota, !quota.unlimited {
                        ReceiptStubsView(limit: quota.limit, used: quota.used)
                    }
                    plansSection
                    legalSection
                }
                .padding(.horizontal, 20)
                .padding(.top, 8)
                .padding(.bottom, 24)
            }
            .background(Color.bg)
            .safeAreaInset(edge: .bottom) { purchaseBar }
            .navigationTitle(Text("Splitor Plus"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "Закрыть")) { dismiss() }
                }
            }
            .task { await store.storeKit.loadProducts() }
            .alert(
                String(localized: "Покупка"),
                isPresented: Binding(
                    get: { store.purchaseError != nil },
                    set: { if !$0 { store.purchaseError = nil } }
                )
            ) {
                Button(String(localized: "Понятно"), role: .cancel) { store.purchaseError = nil }
            } message: {
                Text(store.purchaseError ?? "")
            }
        }
    }

    // MARK: - Герой

    /// Момент, ради которого платят: фраза превращается в готовый расход.
    private var heroCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label {
                Text("«Ужин 3200, делим на четверых»")
                    .scaledFont(size: 16, weight: .medium)
                    .foregroundStyle(Color.ink)
            } icon: {
                Image(systemName: "mic.fill")
                    .foregroundStyle(Color.accent)
            }

            Image(systemName: "arrow.down")
                .scaledFont(size: 13, weight: .semibold)
                .foregroundStyle(Color.inkSecondary)
                .frame(maxWidth: .infinity)

            recognizedExpense
        }
        .padding(18)
        .background(Color.receiptPaper, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Color.hairline, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text("Сказанная фраза превращается в готовый расход, разделённый на четверых"))
    }

    private var recognizedExpense: some View {
        VStack(spacing: 10) {
            HStack {
                Text("Ужин")
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Spacer()
                Text("3 200 ₽")
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.ink)
                    .monospacedDigit()
            }
            Divider().overlay(Color.hairline)
            HStack {
                Text("Поровну на четверых")
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .foregroundStyle(Color.inkSecondary)
                Spacer()
                Text("по 800 ₽")
                    .scaledFont(size: 14, weight: .medium, relativeTo: .subheadline)
                    .foregroundStyle(Color.accentText)
                    .monospacedDigit()
            }
        }
    }

    // MARK: - Тарифы

    private var plansSection: some View {
        VStack(spacing: 10) {
            if store.storeKit.isLoadingProducts && products.isEmpty {
                ProgressView().frame(maxWidth: .infinity, minHeight: 120)
            } else if products.isEmpty {
                Text("Не удалось загрузить тарифы. Проверьте связь и попробуйте ещё раз")
                    .scaledFont(size: 15, relativeTo: .subheadline)
                    .foregroundStyle(Color.inkSecondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity, minHeight: 120)
            } else {
                ForEach(products, id: \.id) { product in
                    PlanRow(
                        product: product,
                        isSelected: product.id == selectedProductId,
                        discount: discountBadge(for: product)
                    ) {
                        selectedProductId = product.id
                    }
                }
            }
        }
    }

    /// Скидка годового относительно месячного.
    ///
    /// Считается ТОЛЬКО когда валюты обоих продуктов совпадают: App Store
    /// отдаёт цены в валюте витрины покупателя, и вычесть рубли из долларов
    /// значит показать выдуманный процент. Не сходится — бейджа просто нет.
    private func discountBadge(for product: Product) -> String? {
        guard product.id == StoreKitService.yearlyId,
              let monthly = products.first(where: { $0.id == StoreKitService.monthlyId }),
              monthly.priceFormatStyle.currencyCode == product.priceFormatStyle.currencyCode
        else { return nil }

        // Всё в Decimal: цены магазина — Decimal, и смешивание с литералами
        // молча ушло бы в двоичную плавающую точку на деньгах.
        let yearAtMonthlyRate = monthly.price * Decimal(12)
        guard yearAtMonthlyRate > 0, product.price < yearAtMonthlyRate else { return nil }

        let saved = (yearAtMonthlyRate - product.price) / yearAtMonthlyRate
        let percent = Int(NSDecimalNumber(decimal: saved * Decimal(100)).doubleValue.rounded())
        guard percent >= 5 else { return nil }
        return "−\(percent)%"
    }

    // MARK: - Покупка

    private var purchaseBar: some View {
        VStack(spacing: 10) {
            Button {
                Task { await buy() }
            } label: {
                if store.storeKit.purchasingProductId != nil {
                    ProgressView().tint(.white)
                } else {
                    Text(purchaseTitle)
                }
            }
            .buttonStyle(.primaryPill)
            .disabled(selectedProduct == nil || store.storeKit.purchasingProductId != nil)

            // Обязательное раскрытие: без явного «продлевается автоматически»
            // подписку заворачивают на ревью.
            Text(renewalNotice)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Color.inkSecondary)
                .multilineTextAlignment(.center)

            Button(String(localized: "Добавить расход вручную")) { dismiss() }
                .scaledFont(size: 14, weight: .medium, relativeTo: .subheadline)
                .foregroundStyle(Color.inkSecondary)
        }
        .padding(.horizontal, 20)
        .padding(.top, 12)
        .padding(.bottom, 8)
        .background(.bar)
    }

    private var purchaseTitle: String {
        guard let product = selectedProduct else { return String(localized: "Оформить") }
        return String(localized: "Оформить за \(product.displayPrice)")
    }

    private var renewalNotice: String {
        guard let product = selectedProduct else {
            return String(localized: "Подписка продлевается автоматически. Отменить можно в настройках Apple ID")
        }
        let period = product.id == StoreKitService.yearlyId
            ? String(localized: "год")
            : String(localized: "месяц")
        return String(localized:
            "\(product.displayPrice) за \(period). Подписка продлевается автоматически, отменить можно в настройках Apple ID")
    }

    private func buy() async {
        guard let product = selectedProduct else { return }
        if await store.purchase(product) == .success {
            dismiss()
        }
    }

    // MARK: - Обязательные ссылки

    private var legalSection: some View {
        VStack(spacing: 12) {
            Button {
                Task {
                    isRestoring = true
                    defer { isRestoring = false }
                    if await store.restore() { dismiss() }
                }
            } label: {
                if isRestoring {
                    ProgressView()
                } else {
                    Text("Восстановить покупки")
                }
            }
            .buttonStyle(.softChip)

            HStack(spacing: 16) {
                Button(String(localized: "Условия")) { open("/terms") }
                Button(String(localized: "Конфиденциальность")) { open("/privacy") }
            }
            .scaledFont(size: 13, relativeTo: .footnote)
            .foregroundStyle(Color.inkSecondary)
        }
        .padding(.top, 4)
    }

    private func open(_ path: String) {
        guard let url = URL(string: "https://splitor.zagirnur.dev" + path) else { return }
        openURL(url)
    }
}

/// Строка тарифа.
private struct PlanRow: View {
    let product: Product
    let isSelected: Bool
    let discount: String?
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: 12) {
                Image(systemName: isSelected ? "largecircle.fill.circle" : "circle")
                    .foregroundStyle(isSelected ? Color.accent : Color.inkSecondary)
                    .scaledFont(size: 20)

                VStack(alignment: .leading, spacing: 2) {
                    Text(product.displayName)
                        .scaledFont(size: 16, weight: .semibold)
                        .foregroundStyle(Color.ink)
                    Text(product.displayPrice)
                        .scaledFont(size: 14, relativeTo: .subheadline)
                        .foregroundStyle(Color.inkSecondary)
                }

                Spacer()

                if let discount {
                    Text(discount)
                        .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                        .foregroundStyle(.white)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(Color.accent, in: Capsule())
                }
            }
            .padding(16)
            .background(Color.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(isSelected ? Color.accent : Color.hairline, lineWidth: isSelected ? 2 : 1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }
}
