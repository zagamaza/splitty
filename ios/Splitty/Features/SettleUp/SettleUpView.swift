import SwiftUI

/// Погашение долга («Записать платёж»): премиум-sheet на нейтральном фоне —
/// карточка «должник → кредитор», крупная сумма (prefilled текущим долгом,
/// не больше долга) и CTA «Записать платёж». Если долгов с участием
/// пользователя несколько — сначала список «Ваши долги» карточками.
struct SettleUpView: View {
    private let roomId: String
    /// Валюта комнаты — в ней долг и сумма платежа.
    private let currency: String
    private let preselectedDebt: Debt?
    private let onDone: (() -> Void)?

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    /// nil — ещё не загружены (нужен спиннер).
    @State private var debts: [Debt]?
    @State private var loadError: String?
    @State private var selectedDebt: Debt?
    @State private var sumText: String
    @State private var isSaving = false
    @State private var alertMessage: String?
    /// Ключи идемпотентности платежа — см. `RepayIdempotency`.
    @State private var idempotency = RepayIdempotency()
    @FocusState private var isSumFocused: Bool

    // Валюта без дефолта: «RUB по умолчанию» молча показывал рубли
    // в чужих валютных комнатах — вызывающий обязан передать валюту комнаты.
    init(
        roomId: String,
        currency: String,
        preselectedDebt: Debt? = nil,
        onDone: (() -> Void)? = nil
    ) {
        self.roomId = roomId
        self.currency = currency
        self.preselectedDebt = preselectedDebt
        self.onDone = onDone
        _selectedDebt = State(initialValue: preselectedDebt)
        _sumText = State(initialValue: preselectedDebt.map { String($0.sum) } ?? "")
    }

    /// nil, пока профиль не загружен. Фейковый `?? 0` делал `debt.debtor.id ==
    /// meId` ложным для ВСЕХ долгов: собственный долг подписывался «X должен(на)
    /// вам» и красился зелёным. Тот же паттерн, что в балансах и тусе.
    private var meId: Int? { session.me?.id }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Записать платёж")
                .navigationBarTitleDisplayMode(.inline)
                .background(Color.bg)
                .toolbar {
                    // Слева — «Назад» только когда форма открыта из списка
                    // долгов (возврат к выбору); закрытие sheet — всегда
                    // справа, чтобы leading не собирал два элемента.
                    if selectedDebt != nil && preselectedDebt == nil {
                        ToolbarItem(placement: .topBarLeading) {
                            Button {
                                selectedDebt = nil
                            } label: {
                                HStack(spacing: 3) {
                                    Image(systemName: "chevron.left")
                                    Text("Назад")
                                }
                            }
                            .accessibilityLabel("Назад, к списку долгов")
                        }
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Закрыть") { dismiss() }
                    }
                }
                .alert("Ошибка", isPresented: alertPresented) {
                    Button("Ок", role: .cancel) {}
                } message: {
                    Text(alertMessage ?? "")
                }
        }
        .tint(Color.accent)
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )
    }

    @ViewBuilder
    private var content: some View {
        if meId == nil {
            // Без профиля направление долга неизвестно: показать форму —
            // значит наврать, кто кому платит (см. `meId`).
            ContentUnavailableView {
                Label("Профиль не загружен", systemImage: "person.crop.circle.badge.exclamationmark")
            } description: {
                Text("Не удалось получить данные вашего профиля")
            } actions: {
                Button("Повторить") {
                    Task { await session.refreshMe() }
                }
                .buttonStyle(.borderedProminent)
                .tint(Color.accent)
            }
        } else if let debt = selectedDebt {
            paymentForm(debt: debt)
        } else {
            debtPicker
        }
    }

    // MARK: Список «Ваши долги»

    @ViewBuilder
    private var debtPicker: some View {
        if let loadError {
            // Единый failed-state проекта (стиль кнопки «Повторить» — общий).
            FailedStateView(message: loadError) {
                await loadDebts()
            }
        } else if let debts {
            if debts.isEmpty {
                ContentUnavailableView {
                    Label("Все в расчёте", systemImage: "checkmark.circle")
                } description: {
                    Text("У вас нет долгов в этой группе")
                }
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Ваши долги")
                            .sectionHeaderStyle()
                            .padding(.horizontal, 4)
                        ForEach(debts) { debt in
                            Button {
                                select(debt)
                            } label: {
                                debtRow(debt)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(20)
                }
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .task { await loadDebts() }
        }
    }

    /// Карточка долга: «А → Б», подпись и сумма семантическим цветом.
    private func debtRow(_ debt: Debt) -> some View {
        HStack(spacing: 10) {
            UserAvatarView(user: debt.debtor, size: 40)
            Image(systemName: "arrow.right")
                .font(.caption.weight(.bold))
                .foregroundStyle(Color.inkSecondary)
            UserAvatarView(user: debt.lender, size: 40)
            VStack(alignment: .leading, spacing: 3) {
                Text(debtTitle(debt))
                    .scaledFont(size: 15, weight: .medium)
                    .foregroundStyle(Color.ink)
                    .lineLimit(2)
                MoneyText(
                    debt.sum,
                    role: debt.debtor.id == meId ? .negative : .positive,
                    size: 16,
                    currency: currency
                )
            }
            Spacer(minLength: 8)
            Image(systemName: "chevron.right")
                .font(.caption.weight(.semibold))
                .foregroundStyle(Color.inkSecondary)
        }
        .surfaceCard()
    }

    private func debtTitle(_ debt: Debt) -> String {
        if debt.debtor.id == meId {
            return String(localized: "Вы должны \(debt.lender.displayName)")
        }
        return String(localized: "\(debt.debtor.displayName) должен(на) вам")
    }

    // MARK: Форма платежа

    private func paymentForm(debt: Debt) -> some View {
        ScrollView {
            VStack(spacing: 20) {
                header(debt: debt)
                sumCard(debt: debt)
                Button {
                    Task { await repay(debt: debt) }
                } label: {
                    if isSaving {
                        ProgressView()
                            .tint(.white)
                    } else {
                        Text("Записать платёж")
                    }
                }
                .buttonStyle(.primaryPill)
                // Офлайн — кнопка задизейблена с подписью-причиной ниже,
                // а не alert по тапу (кнопка «работала», но ругалась).
                .disabled(!isSumValid(debt: debt) || isSaving || !session.isOnline)

                // Приложение только ведёт счёт: деньги оно не переводит.
                // Без этой строки «Записать платёж» читается как перевод,
                // и человек ждёт, что деньги уйдут сами
                Text("Splitty только записывает: деньги передайте наличными или переводом")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(Color.inkSecondary)
                    .frame(maxWidth: .infinity)

                if !session.isOnline {
                    Text("Погашение доступно только онлайн")
                        .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                        .foregroundStyle(Color.negativeText)
                }
            }
            .padding(20)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    /// Карточка-шапка: аватар должника → аватар кредитора, «Загир платит Алмаз».
    private func header(debt: Debt) -> some View {
        VStack(spacing: 16) {
            HStack(spacing: 20) {
                UserAvatarView(user: debt.debtor, size: 64)
                Image(systemName: "arrow.right")
                    .font(.title3.weight(.bold))
                    .foregroundStyle(Color.accentText)
                UserAvatarView(user: debt.lender, size: 64)
            }
            Text(paymentTitle(debt: debt))
                .scaledFont(size: 17, weight: .semibold)
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .surfaceCard(padding: 24)
    }

    private func paymentTitle(debt: Debt) -> String {
        // Прошедшее время: экран записывает состоявшийся факт, а не начинает
        // перевод. «Вы платите» обещало действие, которого приложение не делает
        if debt.debtor.id == meId {
            return String(localized: "Вы отдали — \(debt.lender.displayName)")
        }
        if debt.lender.id == meId {
            return String(localized: "\(debt.debtor.displayName) отдал(а) вам")
        }
        return String(localized: "\(debt.debtor.displayName) отдал(а) — \(debt.lender.displayName)")
    }

    /// Карточка суммы: hero-поле rounded + monospacedDigit по центру,
    /// hairline и подсказка «Долг: …» / предупреждение о превышении.
    private func sumCard(debt: Debt) -> some View {
        VStack(spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(currencySymbol(currency))
                    .scaledFont(size: 28, weight: .medium, relativeTo: .title)
                    .foregroundStyle(Color.inkSecondary)
                TextField("0", text: $sumText)
                    .scaledFont(size: 42, weight: .semibold, relativeTo: .title)
                    .monospacedDigit()
                    .foregroundStyle(Color.ink)
                    .keyboardType(.numberPad)
                    .multilineTextAlignment(.center)
                    .focused($isSumFocused)
                    .fixedSize()
                    .onChange(of: sumText) { _, newValue in
                        let filtered = String(newValue.filter(\.isNumber).prefix(9))
                        if filtered != newValue {
                            sumText = filtered
                        }
                    }
            }
            .frame(maxWidth: .infinity)
            .contentShape(Rectangle())
            .onTapGesture { isSumFocused = true }

            Rectangle()
                .fill(Color.hairline)
                .frame(height: 1)
                .frame(maxWidth: 160)

            if let sum = Int(sumText), sum > debt.sum {
                Text("Не больше долга: \(money(debt.sum, currency: currency))")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .monospacedDigit()
                    .foregroundStyle(Color.negativeText)
            } else {
                Text("Долг: \(money(debt.sum, currency: currency))")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        .surfaceCard(padding: 20)
    }

    private func isSumValid(debt: Debt) -> Bool {
        guard let sum = Int(sumText) else { return false }
        return sum >= 1 && sum <= debt.sum
    }

    // MARK: Действия

    private func select(_ debt: Debt) {
        selectedDebt = debt
        sumText = String(debt.sum)
    }

    private func loadDebts() async {
        loadError = nil
        do {
            debts = try await session.api.debts(roomId: roomId, involving: "me")
            // Единственный долг выбираем сразу.
            if let debts, debts.count == 1, let only = debts.first {
                select(only)
            }
        } catch {
            // Отмена .task (закрыли sheet) — не ошибка.
            if error.isTaskCancellation { return }
            loadError = humanErrorText(error)
        }
    }

    private func repay(debt: Debt) async {
        // Защита от двойного тапа по «Записать платёж»: второй Task в том же
        // кадре не должен отправить второй POST (isSaving выставляется до await).
        guard !isSaving else { return }
        // Погашения офлайн не работают (зафиксированный дизайн v1):
        // CTA задизейблен с подписью-причиной, это молчаливая страховка.
        guard session.isOnline else { return }
        guard let sum = Int(sumText), isSumValid(debt: debt) else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            _ = try await session.api.repay(
                roomId: roomId,
                debtorId: debt.debtor.id,
                lenderId: debt.lender.id,
                sum: sum,
                clientOpId: idempotency.key(debtorId: debt.debtor.id, lenderId: debt.lender.id, sum: sum)
            )
            Haptics.success()
            // Единая инвалидация: списки и экран группы перезагрузятся по dataVersion.
            session.noteDataChanged()
            session.confirm(String(localized: "Погашение записано"))
            onDone?()
            dismiss()
        } catch {
            // 409 — долг успели погасить или уменьшить параллельным платежом.
            // Общий текст «Действие сейчас невозможно» оставлял человека перед
            // формой с уже введённой суммой и устаревшим долгом: понять, что
            // произошло, можно было только закрыв экран (порт Android).
            if let apiError = error as? APIError,
               case .server(let status, _, _, _) = apiError, status == 409 {
                await recoverFromSettledDebt()
                return
            }
            alertMessage = humanErrorText(error)
        }
    }

    /// Перечитывает долги после 409 и возвращает к выбору.
    ///
    /// Если перечитать не удалось, выбор НЕ сбрасываем: иначе экран откатился бы
    /// на шаг «выберите долг» поверх устаревшего списка, а при единственном
    /// долге — ещё и без кнопки возврата к нему.
    private func recoverFromSettledDebt() async {
        alertMessage = String(localized: "Долг уже погашен")
        guard let fresh = try? await session.api.debts(roomId: roomId, involving: "me") else {
            return
        }
        debts = fresh
        session.noteDataChanged()
        if let only = fresh.first, fresh.count == 1 {
            select(only)
        } else {
            selectedDebt = nil
            sumText = ""
        }
    }
}

/// Ключ идемпотентности погашения.
///
/// Один и тот же для повторов ОДНОЙ попытки: иначе «ошибка сети, жму ещё раз»
/// списывает дважды — сервер отклоняет только возврат СВЕРХ долга, а два
/// частичных погашения долг не превышают и проходят оба.
///
/// И обязательно новый, когда поправили сумму или выбрали другой долг: на
/// повтор со старым ключом сервер вернёт прежнюю операцию, и правка молча
/// потеряется.
struct RepayIdempotency {
    private var intent: String?
    private var current: String?

    mutating func key(debtorId: Int, lenderId: Int, sum: Int) -> String {
        let next = "\(debtorId)-\(lenderId)-\(sum)"
        if intent == next, let current {
            return current
        }
        let fresh = UUID().uuidString
        intent = next
        current = fresh
        return fresh
    }
}
