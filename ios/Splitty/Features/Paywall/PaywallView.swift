import StoreKit
import SwiftUI

/// Экран оплаты Splitor Plus.
///
/// Открывается там, где человек упёрся: распознавания на сегодня кончились.
/// Поэтому первое, что он читает, — что именно произошло и что это снимает.
/// Экран без заголовка заставлял догадываться по картинке, ради чего платить.
///
/// Годовой тариф продаёт не сама цена, а цена ЗА МЕСЯЦ при годовой оплате:
/// «$19.99» рядом с «$2.99» выглядит дороже, хотя вдвое дешевле.
///
/// Обязательное по Guideline 3.1.2 (цена, период, автопродление,
/// восстановление покупок, ссылки на условия и политику) — не формальность
/// внизу, а причина, по которой подписку вообще пропустят на ревью.
struct PaywallView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    let store: SubscriptionStore
    var quota: AiQuota?
    /// Откуда открыт: `quota` — упёрся в лимит распознаваний, `account` — зашёл
    /// сам из профиля. Два разных вопроса к воронке, и складывать их нельзя.
    var from: String = "quota"

    @State private var selectedProductId = StoreKitService.yearlyId
    @State private var isRestoring = false

    private var products: [Product] { store.storeKit.products }
    private var selectedProduct: Product? {
        products.first { $0.id == selectedProductId } ?? products.first
    }

    /// Человек пришёл сюда, упёршись в лимит, — или сам, из профиля.
    private var isOutOfQuota: Bool {
        guard let quota, !quota.unlimited else { return false }
        return quota.remaining <= 0
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 22) {
                    headline
                    heroCard
                    benefits
                    plansSection
                }
                .padding(.horizontal, 20)
                .padding(.top, 4)
                .padding(.bottom, 16)
            }
            .background(Color.bg)
            .safeAreaInset(edge: .bottom) { purchaseBar }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                            .scaledFont(size: 15, weight: .semibold)
                            .foregroundStyle(Color.inkSecondary)
                    }
                    .accessibilityLabel(Text("Закрыть"))
                }
            }
            .task { await store.storeKit.loadProducts() }
            .onAppear { Analytics.shared.track(.paywallShown(from: from)) }
            .onDisappear {
                // Закрыли, не купив. Вывести это из «показали, но не начали
                // покупку» нельзя: «закрыл экран» и «ушёл из приложения» —
                // разные ответы на вопрос, почему не купили.
                if !store.isPlus {
                    Analytics.shared.track(.paywallDismissed(from: from))
                }
            }
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

    // MARK: - Заголовок

    /// Первое, что читает человек. Текст зависит от того, как он сюда попал:
    /// упёрся в лимит — объясняем, что кончилось и когда вернётся; пришёл сам —
    /// говорим, что даёт тариф.
    private var headline: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Splitor Plus")
                .scaledFont(size: 13, weight: .bold, relativeTo: .footnote)
                .tracking(1.2)
                .foregroundStyle(Color.accentText)

            Text(isOutOfQuota
                 ? String(localized: "Распознавания на сегодня кончились")
                 : String(localized: "Распознавайте без суточного лимита"))
                .scaledFont(size: 28, weight: .bold, relativeTo: .title)
                .foregroundStyle(Color.ink)
                .fixedSize(horizontal: false, vertical: true)

            Text(subtitleText)
                .scaledFont(size: 15, relativeTo: .subheadline)
                .foregroundStyle(Color.inkSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var subtitleText: String {
        guard isOutOfQuota, let quota else {
            return String(localized: "Диктуйте расход или снимайте чек столько раз, сколько нужно")
        }
        let hours = Int((quota.resetsIn / 3600).rounded(.up))
        if hours >= 1 {
            return String(localized: "Бесплатные вернутся через \(hours) ч. Plus снимает суточный лимит совсем")
        }
        return String(localized: "Бесплатные скоро вернутся. Plus снимает суточный лимит совсем")
    }

    // MARK: - Герой

    /// Момент, ради которого платят: сказанная фраза становится готовым расходом.
    /// Компактнее прежнего — заголовок выше уже всё назвал словами.
    private var heroCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "mic.fill")
                    .scaledFont(size: 13)
                    .foregroundStyle(Color.accent)
                Text("«Ужин 3200, делим на четверых»")
                    .scaledFont(size: 15, weight: .medium, relativeTo: .subheadline)
                    .foregroundStyle(Color.ink)
            }

            Divider().overlay(Color.hairline)

            HStack {
                Text("Ужин")
                    .scaledFont(size: 16, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Spacer()
                Text("3 200 ₽")
                    .scaledFont(size: 16, weight: .semibold)
                    .foregroundStyle(Color.ink)
                    .monospacedDigit()
            }
            HStack {
                Text("Поровну на четверых")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                Spacer()
                Text("по 800 ₽")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.accentText)
                    .monospacedDigit()
            }
        }
        .padding(14)
        .background(Color.receiptPaper, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Color.hairline, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text("Сказанная фраза превращается в готовый расход, разделённый на четверых"))
    }

    // MARK: - Что даёт Plus

    /// Три строки вместо пустоты между чеком и тарифами.
    ///
    /// Последняя — про бесплатное — не рекламная: на ревью подписку заворачивают,
    /// когда непонятно, что именно платное, а что и так работает.
    private var benefits: some View {
        VStack(alignment: .leading, spacing: 10) {
            benefitRow("infinity", "Распознаваний в день — сколько нужно")
            benefitRow("mic.fill", "Голосом или фото чека — как удобно")
            benefitRow("checkmark.circle.fill", "Группы, расходы и долги остаются бесплатными")
        }
    }

    private func benefitRow(_ icon: String, _ text: LocalizedStringKey) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: icon)
                .scaledFont(size: 14)
                .foregroundStyle(Color.accent)
                .frame(width: 20)
            Text(text)
                .scaledFont(size: 15, relativeTo: .subheadline)
                .foregroundStyle(Color.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // MARK: - Тарифы

    private var plansSection: some View {
        VStack(spacing: 10) {
            if store.storeKit.isLoadingProducts && products.isEmpty {
                ProgressView().frame(maxWidth: .infinity, minHeight: 132)
            } else if products.isEmpty {
                Text("Не удалось загрузить тарифы. Проверьте связь и попробуйте ещё раз")
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .foregroundStyle(Color.inkSecondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity, minHeight: 132)
            } else {
                ForEach(products, id: \.id) { product in
                    PlanRow(
                        product: product,
                        title: planTitle(for: product),
                        isSelected: product.id == selectedProductId,
                        perMonth: perMonthText(for: product),
                        discount: discountBadge(for: product)
                    ) {
                        selectedProductId = product.id
                    }
                }
            }
        }
    }

    /// Название периода своими словами: `displayName` приходит из App Store
    /// Connect и там не локализовано — на русском экране торчало «Yearly».
    private func planTitle(for product: Product) -> String {
        product.id == StoreKitService.yearlyId
            ? String(localized: "На год")
            : String(localized: "На месяц")
    }

    /// Цена за месяц при годовой оплате — то, что делает годовой тариф понятным.
    /// Без неё «$19.99» рядом с «$2.99» читается как «дороже».
    private func perMonthText(for product: Product) -> String? {
        guard product.id == StoreKitService.yearlyId else { return nil }
        let monthly = product.price / Decimal(12)
        let formatted = monthly.formatted(product.priceFormatStyle)
        return String(localized: "\(formatted) в месяц")
    }

    /// Скидка годового относительно месячного.
    ///
    /// Считается ТОЛЬКО когда валюты обоих продуктов совпадают: App Store
    /// отдаёт цены в валюте витрины покупателя, и вычесть рубли из долларов
    /// значит показать выдуманный процент.
    private func discountBadge(for product: Product) -> String? {
        guard product.id == StoreKitService.yearlyId,
              let monthly = products.first(where: { $0.id == StoreKitService.monthlyId }),
              monthly.priceFormatStyle.currencyCode == product.priceFormatStyle.currencyCode
        else { return nil }

        let yearAtMonthlyRate = monthly.price * Decimal(12)
        guard yearAtMonthlyRate > 0, product.price < yearAtMonthlyRate else { return nil }

        let saved = (yearAtMonthlyRate - product.price) / yearAtMonthlyRate
        let percent = Int(NSDecimalNumber(decimal: saved * Decimal(100)).doubleValue.rounded())
        guard percent >= 5 else { return nil }
        return "−\(percent)%"
    }

    // MARK: - Покупка

    private var purchaseBar: some View {
        VStack(spacing: 8) {
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
                .scaledFont(size: 11, relativeTo: .caption2)
                .foregroundStyle(Color.inkSecondary)
                .multilineTextAlignment(.center)

            legalRow
                .padding(.top, 2)
        }
        .padding(.horizontal, 20)
        .padding(.top, 10)
        .padding(.bottom, 6)
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

    private var legalRow: some View {
        HStack(spacing: 14) {
            Button {
                Task {
                    isRestoring = true
                    defer { isRestoring = false }
                    if await store.restore() { dismiss() }
                }
            } label: {
                if isRestoring {
                    ProgressView().scaleEffect(0.8)
                } else {
                    Text("Восстановить покупки")
                }
            }
            Spacer(minLength: 8)
            Button(String(localized: "Условия")) { open("/terms") }
            Button(String(localized: "Конфиденциальность")) { open("/privacy") }
        }
        .scaledFont(size: 12, relativeTo: .caption)
        .foregroundStyle(Color.inkSecondary)
        .frame(maxWidth: .infinity)
    }

    private func open(_ path: String) {
        guard let url = URL(string: "https://splitor.zagirnur.dev" + path) else { return }
        openURL(url)
    }
}

/// Строка тарифа: период крупно, цена рядом, под ней — цена за месяц.
private struct PlanRow: View {
    let product: Product
    let title: String
    let isSelected: Bool
    let perMonth: String?
    let discount: String?
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: 12) {
                Image(systemName: isSelected ? "largecircle.fill.circle" : "circle")
                    .foregroundStyle(isSelected ? Color.accent : Color.hairline)
                    .scaledFont(size: 22)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(title)
                            .scaledFont(size: 16, weight: .semibold)
                            .foregroundStyle(Color.ink)
                        if let discount {
                            Text(discount)
                                .scaledFont(size: 12, weight: .bold, relativeTo: .caption)
                                .foregroundStyle(.white)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 3)
                                .background(Color.accent, in: Capsule())
                        }
                    }
                    if let perMonth {
                        Text(perMonth)
                            .scaledFont(size: 13, relativeTo: .footnote)
                            .foregroundStyle(Color.inkSecondary)
                    }
                }

                Spacer(minLength: 8)

                Text(product.displayPrice)
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.ink)
                    .monospacedDigit()
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(Color.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(isSelected ? Color.accent : Color.hairline, lineWidth: isSelected ? 2 : 1)
            }
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
    }
}
