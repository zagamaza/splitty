import SwiftUI

/// Вкладка «Друзья»: hero-карточка «Общий баланс» и карточные строки друзей.
struct FriendsListView: View {
    @Environment(SessionStore.self) private var session
    @State private var model = FriendsViewModel()

    init() {}

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle("Друзья")
                // .task на контенте (не на NavigationStack): срабатывает и при
                // возврате (pop) с экрана друга/группы — балансы обновляются.
                .task {
                    await model.load(repo: session.repo)
                }
                // Единая инвалидация: перезагрузка после любой мутации данных.
                .onChange(of: session.dataVersion) {
                    Task { await model.load(repo: session.repo) }
                }
                .alert(
                    "Ошибка",
                    isPresented: Binding(
                        get: { model.errorMessage != nil },
                        set: { if !$0 { model.errorMessage = nil } }
                    )
                ) {
                    Button("Ок", role: .cancel) {}
                } message: {
                    Text(model.errorMessage ?? "")
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            ProgressView()
        case .failed(let message):
            failedView(message)
        case .loaded:
            friendsList
        }
    }

    /// Ошибка первичной загрузки: текст + кнопка «Повторить».
    private func failedView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
        } description: {
            Text(message)
        } actions: {
            Button("Повторить") {
                Task { await model.load(repo: session.repo) }
            }
            .buttonStyle(.borderedProminent)
            .tint(Color.accent)
        }
    }

    /// Лента: hero-карточка общего баланса + карточные строки друзей на Color.bg.
    private var friendsList: some View {
        ScrollView {
            LazyVStack(spacing: 12) {
                if !model.friends.isEmpty {
                    totalHeader
                        .padding(.bottom, 4)
                    ForEach(model.friends) { friend in
                        NavigationLink {
                            FriendDetailView(friend: friend)
                        } label: {
                            FriendRow(friend: friend)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
        }
        .refreshable {
            await model.refresh(repo: session.repo)
        }
        // Запас под центральную кнопку «+»: на устройствах без home indicator
        // она выступает над таб-баром и перекрывала бы последнюю строку.
        .contentMargins(.bottom, 40, for: .scrollContent)
        .overlay {
            if model.friends.isEmpty {
                ContentUnavailableView(
                    "Пока нет друзей",
                    systemImage: "person.2",
                    description: Text("Друзья появятся, когда у вас будут общие группы с расходами")
                )
            }
        }
    }

    /// Hero-карточка «Общий баланс»: нетто по всем друзьям. Разные валюты
    /// не складываются: основная крупно, остальные — вторичной строкой.
    private var totalHeader: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Общий баланс")
                .sectionHeaderStyle()
            MoneyTotalsText(totals: model.totals)
            Text(totalCaption)
                .font(.system(size: 15, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(padding: 20)
    }

    /// Подпись — по знаку основной валюты (цвет — в самих суммах).
    private var totalCaption: String {
        let primary = model.totals.first?.sum ?? 0
        if primary > 0 {
            return "Вам должны"
        }
        if primary < 0 {
            return "Вы должны"
        }
        return "Все долги погашены"
    }
}

/// Карточная строка друга: градиентный аватар, имя, справа нетто-баланс.
private struct FriendRow: View {
    let friend: FriendBalance

    var body: some View {
        HStack(spacing: 14) {
            UserAvatarView(user: friend.user, size: 48)
            Text(friend.user.displayName)
                .font(.system(size: 17, weight: .semibold, design: .rounded))
                .foregroundStyle(Color.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            trailing
        }
        .surfaceCard()
    }

    /// Нетто с другом: подпись по основной валюте, суммы по валютам
    /// (основная — обычным размером, остальные — мельче вторичной строкой).
    @ViewBuilder
    private var trailing: some View {
        let totals = friend.totals
        if let primary = totals.first {
            VStack(alignment: .trailing, spacing: 2) {
                Text(primary.sum > 0 ? "должен(на) вам" : "вы должны")
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                MoneyTotalsText(
                    totals: totals,
                    primarySize: 17,
                    secondarySize: 13,
                    alignment: .trailing
                )
            }
        } else {
            Text("расчёт")
                .font(.system(size: 15, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
        }
    }
}

#Preview {
    FriendsListView()
        .environment(SessionStore())
}
