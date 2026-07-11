import SwiftUI

/// Экран друга: шапка с большим аватаром и hero-суммой нетто,
/// разбивка «По группам» — карточка со строками-ссылками в группы.
struct FriendDetailView: View {
    @Environment(SessionStore.self) private var session
    /// Переданный из списка снапшот — только начальное состояние;
    /// актуальный баланс подтягивается по .task/.refreshable/dataVersion.
    @State private var friend: FriendBalance
    @State private var errorMessage: String?

    init(friend: FriendBalance) {
        _friend = State(initialValue: friend)
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                header
                groupsSection
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
        }
        .background(Color.bg)
        .navigationTitle(friend.user.displayName)
        .navigationBarTitleDisplayMode(.inline)
        // Актуальный баланс: при показе (и возврате из группы), pull-to-refresh
        // и после любой мутации данных (dataVersion).
        .task { await reload() }
        .refreshable { await reload() }
        .onChange(of: session.dataVersion) {
            Task { await reload() }
        }
        .alert(
            "Ошибка",
            isPresented: Binding(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }
            )
        ) {
            Button("Ок", role: .cancel) {}
        } message: {
            Text(errorMessage ?? "")
        }
    }

    /// Загружает свежий FriendBalance: GET /friends и поиск друга по id.
    private func reload() async {
        do {
            let friends = try await session.api.friends()
            if let updated = friends.first(where: { $0.user.id == friend.user.id }) {
                friend = updated
            } else {
                // Друга больше нет в списке — все долги погашены.
                friend = FriendBalance(user: friend.user, totalsByCurrency: [], rooms: [])
            }
        } catch {
            // Отмена .task (ушли с экрана) — не ошибка.
            if error.isTaskCancellation { return }
            errorMessage = error.localizedDescription
        }
    }

    /// Шапка: большой градиентный аватар, имя, @username и hero-сумма нетто.
    private var header: some View {
        VStack(spacing: 14) {
            UserAvatarView(user: friend.user, size: 88)
            VStack(spacing: 2) {
                Text(friend.user.displayName)
                    .font(.system(size: 24, weight: .semibold, design: .rounded))
                    .foregroundStyle(Color.ink)
                if let username = friend.user.username, !username.isEmpty {
                    Text("@\(username)")
                        .font(.system(size: 15, design: .rounded))
                        .foregroundStyle(Color.inkSecondary)
                }
            }
            VStack(spacing: 4) {
                Text(totalCaption)
                    .sectionHeaderStyle()
                // Нетто по валютам: основная крупно, остальные —
                // вторичной строкой (разные валюты не складываются).
                MoneyTotalsText(totals: friend.totals, primarySize: 38, alignment: .center)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
    }

    /// Подпись — по знаку основной валюты (цвет — в самих суммах).
    private var totalCaption: String {
        let primary = friend.totals.first?.sum ?? 0
        if primary > 0 {
            return "Должен(на) вам"
        }
        if primary < 0 {
            return "Вы должны"
        }
        return "Вы рассчитались"
    }

    /// Секция «По группам»: карточка со строками групп и hairline-разделителями.
    private var groupsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("По группам")
                .sectionHeaderStyle()
                .padding(.horizontal, 4)
            VStack(spacing: 0) {
                if friend.rooms.isEmpty {
                    Text("Долгов по группам нет")
                        .font(.system(size: 15, design: .rounded))
                        .foregroundStyle(Color.inkSecondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(16)
                } else {
                    ForEach(friend.rooms) { room in
                        NavigationLink {
                            GroupDetailView(roomId: room.roomId)
                        } label: {
                            roomRow(room)
                        }
                        .buttonStyle(.plain)
                        if room.id != friend.rooms.last?.id {
                            Rectangle()
                                .fill(Color.hairline)
                                .frame(height: 1)
                                .padding(.leading, 16)
                        }
                    }
                }
            }
            .surfaceCard(padding: 0)
        }
    }

    /// Строка группы: название и баланс с другом в этой группе.
    private func roomRow(_ room: FriendRoomBalance) -> some View {
        HStack(spacing: 12) {
            Text(room.roomName)
                .font(.system(size: 16, weight: .medium, design: .rounded))
                .foregroundStyle(Color.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                Text(room.balance > 0 ? "вам должны" : "вы должны")
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                // Баланс комнаты — в валюте самой комнаты.
                MoneyText(room.balance, size: 16, currency: room.currency)
            }
            Image(systemName: "chevron.right")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Color.inkSecondary.opacity(0.6))
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
    }
}

#Preview {
    NavigationStack {
        FriendDetailView(friend: FriendBalance(
            user: User(id: 42, username: "almaz", displayName: "Алмаз"),
            totalsByCurrency: [
                CurrencySum(currency: "RUB", sum: 500),
                CurrencySum(currency: "USD", sum: -120),
            ],
            rooms: [
                FriendRoomBalance(roomId: "1", roomName: "Стамбул", currency: "RUB", balance: 800),
                FriendRoomBalance(roomId: "2", roomName: "Дача", currency: "RUB", balance: -300),
                FriendRoomBalance(roomId: "3", roomName: "Бали", currency: "USD", balance: -120),
            ]
        ))
    }
    .environment(SessionStore())
}
