import SwiftUI

/// Балансы группы: план переводов на листе чека — долги «кто → кому»
/// словами, у долгов с участием текущего пользователя — ссылка «Погасить».
struct GroupBalancesView: View {
    private let room: RoomDetail
    private let embedded: Bool
    private let onChange: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var settleDebt: Debt?
    @State private var alertMessage: String?

    /// `embedded: true` — вкладка бара тусы (без своего NavigationStack
    /// и кнопки «Готово»); false — прежний самостоятельный sheet.
    init(room: RoomDetail, embedded: Bool = false, onChange: @escaping () -> Void) {
        self.room = room
        self.embedded = embedded
        self.onChange = onChange
    }

    /// nil, пока профиль не загружен: с фейковым id все долги молча
    /// выглядели бы «чужими» — без кнопок «Погасить» и без выделения.
    private var meId: Int? { session.me?.id }

    var body: some View {
        if embedded {
            content
        } else {
            NavigationStack {
                content
                    .navigationTitle("Балансы")
                    .trackScreen("balances")
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Готово") { dismiss() }
                        }
                    }
            }
        }
    }

    private var content: some View {
        Group {
            if let meId {
                if room.debtsUnavailable {
                    // Легаси-данные бота: сервер шлёт debts=[] и myBalance=0.
                    // Ветка обязана быть ПЕРВОЙ — иначе пустой список уходит
                    // в «Все участники в расчёте», то есть враньё про деньги
                    // (тот же гейт, что в шапке тусы и в списке групп).
                    ContentUnavailableView {
                        Label(Glossary.debtsUnavailableHero, systemImage: "questionmark.circle")
                    } description: {
                        Text(Glossary.debtsUnavailableSubtitle)
                    }
                } else if room.debts.isEmpty {
                    VStack(spacing: 10) {
                        StatusStamp(text: String(localized: "Все в расчёте").localizedUppercase)
                        Text("Все участники в расчёте")
                            .scaledFont(size: 15)
                            .foregroundStyle(Color.inkSecondary)
                    }
                    .frame(maxWidth: .infinity)
                    .receiptPaper(padding: 28)
                    .padding(16)
                    .frame(maxHeight: .infinity, alignment: .top)
                } else {
                    debtsList(meId: meId)
                }
            } else {
                // Профиль ещё не загружен — честное состояние вместо списка,
                // где все долги выглядят чужими (тот же паттерн, что в тусе).
                ContentUnavailableView {
                    Label("Профиль не загружен", systemImage: "person.crop.circle.badge.exclamationmark")
                } description: {
                    Text("Не удалось получить данные вашего профиля")
                } actions: {
                    Button("Повторить") {
                        Task { await session.refreshMe() }
                    }
                    .buttonStyle(.primaryPill)
                    .frame(maxWidth: 220)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.bg)
        .errorAlert($alertMessage)
        .sheet(item: $settleDebt) { debt in
            SettleUpView(roomId: room.id, currency: room.currency, fractional: room.fractional, preselectedDebt: debt) {
                onChange()
                // В embedded-режиме экран остаётся: данные перечитаются
                // через session.dataVersion; sheet закрываем только сам.
                if !embedded {
                    dismiss()
                }
            }
        }
    }

    /// План переводов — лист чека: строки обязательств, направление словами.
    private func debtsList(meId: Int) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Text("Балансы").receiptLabel()
                    .padding(.bottom, 6)
                ForEach(room.debts) { debt in
                    DebtRow(debt: debt, meId: meId, currency: room.currency) {
                        // Погашения офлайн не работают (зафиксированный дизайн
                        // v1) — тот же гейт, что у «Погасить» в шапке тусы.
                        if session.isOnline {
                            settleDebt = debt
                        } else {
                            alertMessage = String(localized: "Нет соединения. Погашение долга доступно только онлайн")
                        }
                    }
                    if debt.id != room.debts.last?.id {
                        ReceiptDivider()
                    }
                }
            }
            .receiptPaper(padding: 22)
            .padding(16)
        }
        .refreshable {
            // Тот же жест обновления, что на вкладке операций: синк outbox
            // и единая инвалидация — комнату перечитает родительский экран.
            await session.syncOutbox()
            session.noteDataChanged()
        }
    }
}

// MARK: - Строка долга

private struct DebtRow: View {
    let debt: Debt
    let meId: Int
    let currency: String
    let onSettle: () -> Void

    private var involvesMe: Bool {
        debt.debtor.id == meId || debt.lender.id == meId
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: 0) {
            // Кто кому и сколько — один элемент VoiceOver («Петя должен(на)
            // Вася, 500 рублей»); ссылка «Погасить» остаётся отдельной.
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(title)
                        .scaledFont(size: 15, weight: .medium)
                        .foregroundStyle(Color.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(verbatim: "\(debt.debtor.displayName) → \(debt.lender.displayName)")
                        .scaledFont(size: 12, relativeTo: .caption)
                        .foregroundStyle(Color.inkSecondary)
                        .accessibilityHidden(true)
                }
                Spacer(minLength: 8)
                MoneyText(debt.sum, exactMinor: debt.exactMinor, role: sumRole, size: 15, weight: .bold, currency: currency)
            }
            .accessibilityElement(children: .combine)
            if involvesMe {
                Button("Погасить", action: onSettle)
                    .buttonStyle(.textLink(size: 13))
            }
        }
        .padding(.vertical, 10)
    }

    private var title: String {
        if debt.debtor.id == meId {
            return String(localized: "Вы должны \(debt.lender.displayName)")
        }
        if debt.lender.id == meId {
            return String(localized: "\(debt.debtor.displayName) должен(на) вам")
        }
        // Имя кредитора не склоняем (нет надёжной морфологии) — тире вместо
        // датива, по образцу «Вы платите — Алмаз» в форме погашения.
        return String(localized: "\(debt.debtor.displayName) должен(на) — \(debt.lender.displayName)")
    }

    /// Цвет суммы: мой долг — negative, долг мне — accent, чужие — нейтрально.
    private var sumRole: MoneyText.Role {
        if debt.debtor.id == meId {
            return .negative
        }
        if debt.lender.id == meId {
            return .positive
        }
        return .neutral
    }
}
