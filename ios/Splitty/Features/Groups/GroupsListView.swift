import SwiftUI

/// Вкладка «Группы»: hero-карточка общего баланса, карточки групп, архив.
struct GroupsListView: View {
    @Environment(SessionStore.self) private var session
    @State private var model = GroupsListViewModel()
    @State private var isCreatePresented = false
    @State private var isJoinPresented = false
    /// Задача перезагрузки по dataVersion (отменяем прежнюю — см. GroupDetailView).
    @State private var reloadTask: Task<Void, Never>?

    init() {}

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Группы")
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        // Пункт один — прямая кнопка вместо меню-матрёшки.
                        Button {
                            isJoinPresented = true
                        } label: {
                            Image(systemName: "arrow.right.circle")
                        }
                        .accessibilityLabel("Присоединиться по коду")
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            isCreatePresented = true
                        } label: {
                            Image(systemName: "plus")
                        }
                        .accessibilityLabel("Создать группу")
                    }
                }
                .sheet(isPresented: $isCreatePresented) {
                    // Список обновится через session.dataVersion (bump внутри).
                    CreateGroupView {}
                }
                .sheet(isPresented: $isJoinPresented) {
                    JoinGroupView {}
                }
                .errorAlert($model.alertMessage)
                // .task на контенте (не на NavigationStack): срабатывает при первом
                // показе И при возврате (pop) с экрана группы — балансы обновляются.
                .task { await model.load(repo: session.repo) }
                // Единая инвалидация: перезагрузка после любой мутации данных.
                .onChange(of: session.dataVersion) {
                    reloadTask?.cancel()
                    reloadTask = Task { await model.load(repo: session.repo) }
                }
                .onDisappear { reloadTask?.cancel() }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
        case .failed(let message):
            FailedStateView(message: message) {
                await model.load(repo: session.repo)
            }
            .background(Color.bg)
        case .loaded:
            list
        }
    }

    private var list: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                if model.rooms.isEmpty {
                    // Без строки «Архив»: в пустом состоянии она отвлекает
                    // от первого шага — создать группу или присоединиться.
                    emptyState
                } else {
                    summaryCard
                    groupCards
                    archiveRow
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 16)
        }
        .background(Color.bg)
        .refreshable {
            // Pull-to-refresh — триггер синка outbox перед перечиткой.
            await session.syncOutbox()
            await model.load(repo: session.repo)
        }
        // Запас под центральную кнопку «+»: на устройствах без home indicator
        // она выступает над таб-баром и перекрывала бы последнюю строку.
        .contentMargins(.bottom, 40, for: .scrollContent)
    }

    /// Пустое состояние — дружелюбная карточка вместо системного списка,
    /// оба первых шага доступны прямо отсюда, не только из тулбара.
    private var emptyState: some View {
        ContentUnavailableView {
            Label("Пока нет групп", systemImage: "person.3")
        } description: {
            Text("Создайте группу или присоединитесь по коду приглашения")
        } actions: {
            Button("Создать группу") {
                isCreatePresented = true
            }
            .buttonStyle(.borderedProminent)
            .tint(Color.accent)
            Button("Присоединиться по коду") {
                isJoinPresented = true
            }
            .tint(Color.accent)
        }
        .frame(maxWidth: .infinity)
        .surfaceCard(padding: 8)
    }

    /// Hero-карточка: суммарный баланс по всем группам крупной суммой.
    /// Разные валюты не складываются: основная валюта крупно,
    /// остальные — вторичной строкой (MoneyTotalsText).
    private var summaryCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Общий баланс")
                .sectionHeaderStyle()
            MoneyTotalsText(totals: model.totals)
            Text(summarySubtitle)
                .scaledFont(size: 15)
                .foregroundStyle(Color.inkSecondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(padding: 20)
    }

    /// Подпись под hero-суммой — по знаку основной валюты
    /// (цветовое правило — в самой сумме).
    private var summarySubtitle: String {
        let primary = model.totals.first?.sum ?? 0
        if primary > 0 {
            return "Вам должны"
        }
        if primary < 0 {
            return "Вы должны"
        }
        return "Все долги погашены"
    }

    /// Карточки групп: аватар-градиент, название, баланс справа;
    /// маленький бейдж icloud.slash — есть неотправленные (outbox) операции.
    private var groupCards: some View {
        ForEach(model.rooms) { room in
            NavigationLink {
                GroupDetailView(roomId: room.id)
            } label: {
                GroupCardRow(
                    room: room,
                    hasLocalOperations: !session.outbox.entries(roomId: room.id).isEmpty
                )
            }
            .buttonStyle(.plain)
        }
    }

    /// «Архив» — тихая строка внизу списка, без карточки.
    private var archiveRow: some View {
        NavigationLink {
            ArchivedGroupsView(model: model)
        } label: {
            HStack(spacing: 10) {
                Image(systemName: "archivebox")
                Text("Архив")
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .opacity(0.6)
            }
            .scaledFont(size: 15, weight: .medium)
            .foregroundStyle(Color.inkSecondary)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Карточка группы

private struct GroupCardRow: View {
    let room: RoomSummary
    var showsBalance = true
    /// true — в outbox есть неотправленные операции этой группы (бейдж).
    var hasLocalOperations = false

    var body: some View {
        HStack(spacing: 14) {
            GroupAvatarView(roomId: room.id, name: room.name, size: 46)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 5) {
                    Text(room.name)
                        .scaledFont(size: 16, weight: .semibold)
                        .foregroundStyle(Color.ink)
                        .lineLimit(1)
                    if hasLocalOperations {
                        Image(systemName: "icloud.slash")
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(Color.inkSecondary)
                            .accessibilityLabel("Есть неотправленные операции")
                    }
                }
                Text(memberCountText(room.memberCount))
                    .font(.system(size: 13))
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            if showsBalance {
                trailingBalance
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Color.inkSecondary.opacity(0.6))
            }
        }
        .contentShape(Rectangle())
        .surfaceCard()
    }

    @ViewBuilder
    private var trailingBalance: some View {
        if room.debtsUnavailable {
            // Долги неисчислимы (легаси-данные бота): сервер шлёт myBalance=0 —
            // без этой ветки строка утверждала бы «все в расчёте», хотя долги есть.
            Text(Glossary.debtsUnavailableShort)
                .font(.system(size: 14))
                .foregroundStyle(Color.inkSecondary)
        } else if room.myBalance == 0 {
            Text(Glossary.settled)
                .font(.system(size: 14))
                .foregroundStyle(Color.inkSecondary)
        } else {
            VStack(alignment: .trailing, spacing: 2) {
                Text(room.myBalance > 0 ? "вам должны" : "вы должны")
                    .font(.caption2)
                    .foregroundStyle(Color.inkSecondary)
                MoneyText(room.myBalance, size: 15, currency: room.currency)
            }
        }
    }
}

/// «1 участник», «2 участника», «5 участников».
private func memberCountText(_ count: Int) -> String {
    let rem100 = count % 100
    let rem10 = count % 10
    let word: String
    if (11...14).contains(rem100) {
        word = "участников"
    } else if rem10 == 1 {
        word = "участник"
    } else if (2...4).contains(rem10) {
        word = "участника"
    } else {
        word = "участников"
    }
    return "\(count) \(word)"
}

// MARK: - Аватар группы

/// Круглый аватар группы: детерминированный пастельный градиент по id комнаты
/// и первая буква названия — через общий UserAvatarView (тот же стиль,
/// что и у аватаров людей).
private struct GroupAvatarView: View {
    let roomId: String
    let name: String
    var size: CGFloat = 44

    /// Стабильный (между запусками) хэш id комнаты — задаёт пару градиента.
    private var stableId: Int {
        roomId.unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) & 0x7FFF_FFFF }
    }

    var body: some View {
        UserAvatarView(
            user: User(id: stableId, username: nil, displayName: String(name.prefix(1))),
            size: size,
            // stableId — хэш строки, а НЕ telegram id: фото по нему не грузим,
            // иначе при совпадении диапазонов группа получала бы чужое фото.
            avatarUserId: nil
        )
        .accessibilityHidden(true)
    }
}

// MARK: - Архив

/// Список архивных групп с кнопкой «Разархивировать».
private struct ArchivedGroupsView: View {
    @Bindable var model: GroupsListViewModel
    @Environment(SessionStore.self) private var session

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 16) {
                ForEach(model.archivedRooms) { room in
                    // Кнопка «Разархивировать» — СОСЕД NavigationLink, а не
                    // часть его label: вложенная кнопка конфликтовала хит-зоной
                    // с переходом в группу.
                    HStack(spacing: 14) {
                        // Архивная группа открывается так же, как обычная
                        // (внутри — read-only бейдж «Группа в архиве»).
                        NavigationLink {
                            GroupDetailView(roomId: room.id)
                        } label: {
                            HStack(spacing: 14) {
                                GroupAvatarView(roomId: room.id, name: room.name, size: 46)
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(room.name)
                                        .scaledFont(size: 16, weight: .semibold)
                                        .foregroundStyle(Color.ink)
                                        .lineLimit(1)
                                    Text(memberCountText(room.memberCount))
                                        .font(.system(size: 13))
                                        .foregroundStyle(Color.inkSecondary)
                                }
                                Spacer(minLength: 8)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        Button("Разархивировать") {
                            Task {
                                await model.unarchive(repo: session.repo, roomId: room.id)
                                session.noteDataChanged()
                            }
                        }
                        .buttonStyle(.softChip)
                    }
                    .surfaceCard()
                }
            }
            .padding(16)
        }
        .background(Color.bg)
        // Свой alert: корневой экран закрыт пушем, его alert не показывается —
        // ошибки loadArchive/unarchive иначе глотались бы.
        .errorAlert($model.alertMessage)
        .overlay {
            if model.isArchiveLoading {
                ProgressView()
            } else if model.archivedRooms.isEmpty {
                ContentUnavailableView {
                    Label("Архив пуст", systemImage: "archivebox")
                } description: {
                    Text("Архивные группы появятся здесь")
                }
            }
        }
        .navigationTitle("Архив")
        .navigationBarTitleDisplayMode(.inline)
        .task { await model.loadArchive(repo: session.repo) }
        .refreshable { await model.loadArchive(repo: session.repo) }
    }
}

#Preview {
    GroupsListView()
        .environment(SessionStore())
}
