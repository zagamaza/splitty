import SwiftUI

/// Вкладка «Друзья»: hero-карточка «Общий баланс» и карточные строки друзей.
struct FriendsListView: View {
    @Environment(SessionStore.self) private var session
    @State private var model = FriendsViewModel()
    /// Sheet создания группы из empty state: друзья появляются только
    /// через общие группы, поэтому действие ведёт именно туда.
    @State private var isCreateGroupPresented = false
    /// Созданная группа: в неё уходим сразу после закрытия шита. Без этого
    /// экран не менялся вообще — новая группа без участников не даёт друзей,
    /// и человек создавал её снова и снова, думая, что кнопка не работает.
    @State private var createdRoomId: String?
    @State private var openedRoomId: String?
    /// Задача перезагрузки по dataVersion (отменяем прежнюю — см. GroupDetailView).
    @State private var reloadTask: Task<Void, Never>?

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
                    reloadTask?.cancel()
                    reloadTask = Task { await model.load(repo: session.repo) }
                }
                .onDisappear { reloadTask?.cancel() }
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
                ContentUnavailableView {
                    Label("Друзья — те, с кем уже был общий счёт", systemImage: "person.2")
                } description: {
                    Text("Их можно звать в новые группы одним тапом, без кода")
                } actions: {
                    Button("Создать группу") {
                        isCreateGroupPresented = true
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Color.accent)
                }
            }
        }
        // Пуш только после закрытия шита: одновременный dismiss и push SwiftUI
        // иногда съедает.
        .sheet(isPresented: $isCreateGroupPresented) {
            if let roomId = createdRoomId {
                createdRoomId = nil
                openedRoomId = roomId
            }
        } content: {
            // Список обновится через session.dataVersion (bump внутри),
            // как при создании из GroupsListView.
            CreateGroupView { createdRoomId = $0.id }
        }
        .navigationDestination(item: $openedRoomId) { roomId in
            GroupDetailView(roomId: roomId)
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
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
            CacheNote(isFromCache: model.isFromCache, updatedAt: model.lastUpdatedAt)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(padding: 20)
    }

    /// Подпись — по знаку основной валюты (цвет — в самих суммах).
    private var totalCaption: String {
        let primary = model.totals.first?.sum ?? 0
        if primary > 0 {
            return String(localized: "Вам должны")
        }
        if primary < 0 {
            return String(localized: "Вы должны")
        }
        return Glossary.settledHero
    }
}

/// Карточная строка друга: градиентный аватар, имя, справа нетто-баланс.
private struct FriendRow: View {
    let friend: FriendBalance

    var body: some View {
        HStack(spacing: 14) {
            UserAvatarView(user: friend.user, size: 48)
            Text(friend.user.displayName)
                .scaledFont(size: 17, weight: .semibold)
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
                Text(caption(totals: totals, primary: primary))
                    .scaledFont(size: 12, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                MoneyTotalsText(
                    totals: totals,
                    primarySize: 17,
                    secondarySize: 13,
                    alignment: .trailing
                )
            }
        } else {
            Text(Glossary.settled)
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
        }
    }

    /// Подпись направления: суммы разных знаков по валютам — «взаимные долги»
    /// (единая подпись «должен вам»/«вы должны» тут врала бы), иначе — по
    /// знаку основной валюты с обязательной нулевой веткой.
    private func caption(totals: [CurrencySum], primary: CurrencySum) -> String {
        let hasPositive = totals.contains { $0.sum > 0 }
        let hasNegative = totals.contains { $0.sum < 0 }
        if hasPositive && hasNegative { return String(localized: "взаимные долги") }
        if primary.sum > 0 { return String(localized: "должен(на) вам") }
        if primary.sum < 0 { return String(localized: "вы должны") }
        return Glossary.settled
    }
}

#Preview {
    FriendsListView()
        .environment(SessionStore())
}
