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
    @FocusState private var isSumFocused: Bool

    init(
        roomId: String,
        currency: String = "RUB",
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

    private var meId: Int { session.me?.id ?? 0 }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Записать платёж")
                .navigationBarTitleDisplayMode(.inline)
                .background(Color.bg)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Отмена") { dismiss() }
                    }
                    if selectedDebt != nil && preselectedDebt == nil {
                        ToolbarItem(placement: .topBarLeading) {
                            Button {
                                selectedDebt = nil
                            } label: {
                                Image(systemName: "chevron.left")
                            }
                            .accessibilityLabel("К списку долгов")
                        }
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
        if let debt = selectedDebt {
            paymentForm(debt: debt)
        } else {
            debtPicker
        }
    }

    // MARK: Список «Ваши долги»

    @ViewBuilder
    private var debtPicker: some View {
        if let loadError {
            ContentUnavailableView {
                Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
            } description: {
                Text(loadError)
            } actions: {
                Button("Повторить") {
                    Task { await loadDebts() }
                }
            }
        } else if let debts {
            if debts.isEmpty {
                ContentUnavailableView {
                    Label("Нет долгов", systemImage: "checkmark.circle")
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
                    .font(.system(size: 15, weight: .medium, design: .rounded))
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
            return "Вы должны \(debt.lender.displayName)"
        }
        return "\(debt.debtor.displayName) должен(на) вам"
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
                .disabled(!isSumValid(debt: debt) || isSaving)
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
                    .foregroundStyle(Color.accent)
                UserAvatarView(user: debt.lender, size: 64)
            }
            Text(paymentTitle(debt: debt))
                .font(.system(size: 17, weight: .semibold, design: .rounded))
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .surfaceCard(padding: 24)
    }

    private func paymentTitle(debt: Debt) -> String {
        if debt.debtor.id == meId {
            return "Вы платите: \(debt.lender.displayName)"
        }
        if debt.lender.id == meId {
            return "\(debt.debtor.displayName) платит вам"
        }
        return "\(debt.debtor.displayName) платит: \(debt.lender.displayName)"
    }

    /// Карточка суммы: hero-поле rounded + monospacedDigit по центру,
    /// hairline и подсказка «Долг: …» / предупреждение о превышении.
    private func sumCard(debt: Debt) -> some View {
        VStack(spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(currencySymbol(currency))
                    .font(.system(size: 28, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                TextField("0", text: $sumText)
                    .font(.system(size: 42, weight: .semibold, design: .rounded))
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
                    .font(.system(size: 13, weight: .medium, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.negative)
            } else {
                Text("Долг: \(money(debt.sum, currency: currency))")
                    .font(.system(size: 13, weight: .medium, design: .rounded))
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
            loadError = error.localizedDescription
        }
    }

    private func repay(debt: Debt) async {
        // Защита от двойного тапа по «Записать платёж»: второй Task в том же
        // кадре не должен отправить второй POST (isSaving выставляется до await).
        guard !isSaving else { return }
        // Погашения офлайн не работают (зафиксированный дизайн v1):
        // экран группы не даёт открыть sheet без сети, это страховка.
        guard session.isOnline else {
            alertMessage = "Нет соединения. Погашение долга доступно только онлайн"
            return
        }
        guard let sum = Int(sumText), isSumValid(debt: debt) else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            _ = try await session.api.repay(
                roomId: roomId,
                debtorId: debt.debtor.id,
                lenderId: debt.lender.id,
                sum: sum
            )
            Haptics.success()
            // Единая инвалидация: списки и экран группы перезагрузятся по dataVersion.
            session.noteDataChanged()
            onDone?()
            dismiss()
        } catch {
            alertMessage = error.localizedDescription
        }
    }
}
