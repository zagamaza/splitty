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
                    ContentUnavailableView {
                        Label("Все в расчёте", systemImage: "checkmark.circle")
                    } description: {
                        Text("Все участники в расчёте")
                    }
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
                    .buttonStyle(.borderedProminent)
                    .tint(Color.accent)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.bg)
        .errorAlert($alertMessage)
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
    private func debtsList(meId: Int) -> some View {
        ScrollView {
            VStack(spacing: 0) {
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
        HStack(spacing: 10) {
            // Инфо-часть строки — один элемент VoiceOver («Петя должен(на)
            // Вася, 500 рублей»); кнопка «Погасить» остаётся отдельной.
            HStack(spacing: 10) {
                UserAvatarView(user: debt.debtor, size: 36)
                // Стрелка — чистая декорация, направление уже есть в тексте.
                Image(systemName: "arrow.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(Color.inkSecondary)
                    .accessibilityHidden(true)
                UserAvatarView(user: debt.lender, size: 36)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Color.ink)
                        .lineLimit(2)
                    MoneyText(debt.sum, role: sumRole, size: 15, currency: currency)
                }
            }
            .accessibilityElement(children: .combine)
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
