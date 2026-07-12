import SwiftUI

/// Балансы группы: карточный список вычисленных долгов «кто → кому»,
/// у долгов с участием текущего пользователя — кнопка «Погасить».
struct GroupBalancesView: View {
    private let room: RoomDetail
    private let embedded: Bool
    private let onChange: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var settleDebt: Debt?

    /// `embedded: true` — вкладка бара тусы (без своего NavigationStack
    /// и кнопки «Готово»); false — прежний самостоятельный sheet.
    init(room: RoomDetail, embedded: Bool = false, onChange: @escaping () -> Void) {
        self.room = room
        self.embedded = embedded
        self.onChange = onChange
    }

    private var meId: Int { session.me?.id ?? 0 }

    var body: some View {
        if embedded {
            content
        } else {
            NavigationStack {
                content
                    .navigationTitle("Балансы")
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
            if room.debts.isEmpty {
                ContentUnavailableView {
                    Label("Нет долгов", systemImage: "checkmark.circle")
                } description: {
                    Text("Все участники в расчёте")
                }
            } else {
                debtsList
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.bg)
        .sheet(item: $settleDebt) { debt in
            SettleUpView(roomId: room.id, currency: room.currency, preselectedDebt: debt) {
                onChange()
                // В embedded-режиме экран остаётся: данные перечитаются
                // через session.dataVersion; sheet закрываем только сам.
                if !embedded {
                    dismiss()
                }
            }
        }
    }

    /// Одна карточка со строками долгов и hairline-разделителями.
    private var debtsList: some View {
        ScrollView {
            VStack(spacing: 0) {
                ForEach(room.debts) { debt in
                    DebtRow(debt: debt, meId: meId, currency: room.currency) {
                        settleDebt = debt
                    }
                    if debt.id != room.debts.last?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                            .padding(.leading, 16)
                    }
                }
            }
            .surfaceCard(padding: 0)
            .padding(16)
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
        HStack(spacing: 10) {
            UserAvatarView(user: debt.debtor, size: 36)
            Image(systemName: "arrow.right")
                .font(.caption.weight(.bold))
                .foregroundStyle(Color.inkSecondary)
            UserAvatarView(user: debt.lender, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                    .lineLimit(2)
                MoneyText(debt.sum, role: sumRole, size: 15, currency: currency)
            }
            Spacer(minLength: 8)
            if involvesMe {
                Button("Погасить", action: onSettle)
                    .buttonStyle(.softChip)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    private var title: String {
        if debt.debtor.id == meId {
            return "Вы должны \(debt.lender.displayName)"
        }
        if debt.lender.id == meId {
            return "\(debt.debtor.displayName) должен(на) вам"
        }
        return "\(debt.debtor.displayName) → \(debt.lender.displayName)"
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
