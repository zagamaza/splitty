import SwiftUI

/// Куда ведёт навигация вкладки «Группы». Переходы значениями, а не
/// view-ссылками: путь задаётся и СНАРУЖИ — тап по push открывает комнату,
/// а это возможно только с типизированным стеком.
enum GroupsRoute: Hashable {
    case room(id: String)
    /// Карточка операции по id — путь тапа по push про расход. Отдельный случай
    /// от перехода из списка: там операция уже загружена целиком, а из payload
    /// известен только её id (см. `PushOperationView`).
    case operation(roomId: String, operationId: String)
    case archive
}

/// Пункт меню «+» на списке групп.
enum GroupsAddAction: CaseIterable {
    case create, joinByCode

    /// `LocalizedStringKey`, а не `String`: `Label(строка, systemImage:)` берёт
    /// обычную строку как есть и не ходит в каталог — пункт меню оставался
    /// русским на любом языке.
    var title: LocalizedStringKey {
        switch self {
        case .create: return "Создать группу"
        case .joinByCode: return "Присоединиться по коду"
        }
    }

    var systemImage: String {
        switch self {
        case .create: return "plus"
        case .joinByCode: return "number"
        }
    }
}

/// Что сказать при нулевом общем итоге. Ноль — не всегда «всё погашено»:
/// +500 в одной группе и −500 в другой дают ноль при двух открытых долгах,
/// а группы со старыми данными в итог не входят вовсе.
enum ZeroBalanceSummary: Equatable {
    /// Открытые долги есть, но в сумме гасятся.
    case cancelOut
    /// Ни в одной группе долги не считаются.
    case unavailable
    /// Где считаются — ноль, но часть групп не считается: «всё погашено»
    /// было бы утверждением о деньгах, которых никто не видел.
    case partial
    case settled

    static func of(_ rooms: [RoomSummary]) -> ZeroBalanceSummary {
        if rooms.contains(where: { !$0.debtsUnavailable && $0.exactMyBalanceMinor != 0 }) { return .cancelOut }
        if rooms.contains(where: \.debtsUnavailable) {
            return rooms.allSatisfy(\.debtsUnavailable) ? .unavailable : .partial
        }
        return .settled
    }
}

/// Что предлагает меню «+».
///
/// Вход по коду жил ТОЛЬКО в пустом состоянии: человек с одной группой попасть
/// в него не мог никак — а код присылают как раз тем, у кого группы уже есть.
/// Отсюда параметр: набор пунктов не зависит от того, пуст ли список.
func groupsAddMenuActions(hasGroups: Bool) -> [GroupsAddAction] {
    GroupsAddAction.allCases
}

/// Вкладка «Группы»: сводка остатка, корешки счетов групп, архив.
struct GroupsListView: View {
    @Environment(SessionStore.self) private var session
    @State private var model = GroupsListViewModel()
    @State private var isCreatePresented = false
    @State private var isJoinPresented = false
    /// Задача перезагрузки по dataVersion (отменяем прежнюю — см. GroupDetailView).
    @State private var reloadTask: Task<Void, Never>?
    /// Путь стека вкладки — владеет им `MainTabView`: тап по push обязан
    /// открыть комнату, а изнутри списка это недостижимо.
    @Binding private var path: [GroupsRoute]

    init(path: Binding<[GroupsRoute]>) {
        _path = path
    }

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationDestination(for: GroupsRoute.self) { route in
                    switch route {
                    case .room(let id):
                        GroupDetailView(roomId: id)
                    case .operation(let roomId, let operationId):
                        PushOperationView(roomId: roomId, operationId: operationId)
                    case .archive:
                        ArchivedGroupsView(model: model)
                    }
                }
                .navigationTitle("Группы")
                .trackScreen("groups")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        // Меню, а не кнопка: вход по коду был доступен ТОЛЬКО из
                        // пустого состояния, и человек с одной группой попасть
                        // в него не мог никак — приглашение по коду становилось
                        // нерабочим ровно после первой группы
                        Menu {
                            ForEach(groupsAddMenuActions(hasGroups: !model.rooms.isEmpty), id: \.self) { action in
                                Button {
                                    switch action {
                                    case .create: isCreatePresented = true
                                    case .joinByCode: isJoinPresented = true
                                    }
                                } label: {
                                    Label(action.title, systemImage: action.systemImage)
                                }
                            }
                        } label: {
                            Image(systemName: "plus")
                        }
                        .accessibilityLabel("Создать группу или присоединиться по коду")
                    }
                }
                .sheet(isPresented: $isCreatePresented) {
                    // Список обновится через session.dataVersion (bump внутри).
                    // Валюта первой тусы в списке — подсказка для следующей:
                    // поездки чаще заводят подряд в одной валюте.
                    CreateGroupView(recentCurrency: model.rooms.first?.currency) { _ in }
                }
                .sheet(isPresented: $isJoinPresented) {
                    JoinGroupView {}
                }
                .errorAlert($model.alertMessage)
                // .task на контенте (не на NavigationStack): срабатывает при первом
                // показе И при возврате (pop) с экрана группы — балансы обновляются.
                .task {
                    await model.load(repo: session.repo)
                }
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
                    // В пустом состоянии «Архив» отвлекает от первого шага —
                    // создать группу или присоединиться. Но если архив не пуст,
                    // строка обязана остаться: заархивировав ПОСЛЕДНЮЮ группу,
                    // человек терял единственный вход в архив и не мог достать
                    // её обратно
                    emptyState
                    if !model.archivedRooms.isEmpty {
                        archiveRow
                    }
                } else {
                    summaryCard
                    groupCards
                    archiveRow
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 4)
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
            Label("Начните с группы", systemImage: "person.3")
        } description: {
            Text("Поездка, общая квартира или один ужин — всё, что вы с кем-то делите")
        } actions: {
            Button("Создать группу") {
                isCreatePresented = true
            }
            .buttonStyle(.primaryPill)
            .frame(maxWidth: 260)
            Button("Присоединиться по коду") {
                isJoinPresented = true
            }
            .buttonStyle(.textLink)
        }
        .frame(maxWidth: .infinity)
        .receiptPaper(padding: 8)
    }

    /// Сводка по всем группам — реквизит на столе, без карточки: словами,
    /// кто кому, и сумма. Разные валюты не складываются: основная крупно,
    /// остальные — вторичной строкой (MoneyTotalsText).
    private var summaryCard: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Общий баланс").receiptLabel()
            if model.totals.isEmpty {
                Text(zeroSummary)
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.ink)
            } else {
                BalanceSummaryLines(totals: model.totals)
            }
            // Группы со старыми данными в итог не входят — и это сказано.
            if hasUnavailableRooms && hasCountableRooms
                && !(model.totals.isEmpty && ZeroBalanceSummary.of(model.rooms) == .partial) {
                Text("Не во всех группах долги считаются")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            freshnessNote
            ReceiptDivider().padding(.top, 8)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Подпись о свежести — общая с карточкой группы, друзьями и активностью.
    private var freshnessNote: some View {
        CacheNote(isFromCache: model.isFromCache, updatedAt: model.lastUpdatedAt)
    }

    private var hasUnavailableRooms: Bool { model.rooms.contains { $0.debtsUnavailable } }
    private var hasCountableRooms: Bool { model.rooms.contains { !$0.debtsUnavailable } }

    private var zeroSummary: String {
        switch ZeroBalanceSummary.of(model.rooms) {
        case .cancelOut: return String(localized: "Долги есть, но в сумме взаимно гасятся")
        case .unavailable: return Glossary.debtsUnavailableHero
        case .partial: return String(localized: "Не во всех группах долги считаются")
        case .settled: return Glossary.settledHero
        }
    }

    /// Карточки групп: аватар-градиент, название, баланс справа;
    /// маленький бейдж icloud.slash — есть неотправленные (outbox) операции.
    private var groupCards: some View {
        ForEach(model.rooms) { room in
            NavigationLink(value: GroupsRoute.room(id: room.id)) {
                GroupCardRow(
                    room: room,
                    hasLocalOperations: !session.outbox.entries(roomId: room.id).isEmpty
                )
            }
            .buttonStyle(.plain)
            // Долгое зажатие вместо свайпа: список это ScrollView + LazyVStack
            // с кастомными карточками, а .swipeActions живёт только внутри List
            // — переводить экран на List значило бы потерять их вид.
            .contextMenu {
                Button("В архив", systemImage: "archivebox") {
                    Task {
                        await model.archive(repo: session.repo, roomId: room.id)
                        session.noteDataChanged()
                    }
                }
            }
        }
    }

    /// «Архив» — закрытые счета: тихая подчёркнутая ссылка под корешками.
    private var archiveRow: some View {
        NavigationLink(value: GroupsRoute.archive) {
            HStack(spacing: 8) {
                Text("Архив")
                Image(systemName: "arrow.up.right")
                    .font(.system(size: 12, weight: .semibold))
                    .accessibilityHidden(true)
            }
        }
        .buttonStyle(.textLink)
        .padding(.top, 4)
    }
}

// MARK: - Карточка группы

/// Корешок счёта группы: реквизит (дата · участники), название, остаток
/// словами и суммой. Пунктир и вырезы снизу — отрывная часть ленты.
private struct GroupCardRow: View {
    let room: RoomSummary
    var showsBalance = true
    /// true — в outbox есть неотправленные операции этой группы (бейдж).
    var hasLocalOperations = false
    var closed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(verbatim: DateFmt.dayMonth(room.createdAt)).receiptLabel()
                Spacer(minLength: 8)
                if hasLocalOperations {
                    Image(systemName: "icloud.slash")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Color.inkSecondary)
                        .accessibilityLabel("Есть неотправленные операции")
                }
                if let unread = MainTabView.badgeLabel(for: room.unreadCount) {
                    UnreadBadge(text: unread)
                        .accessibilityLabel("Новых событий: \(unread)")
                }
                Text(memberCountText(room.memberCount)).receiptLabel()
            }
            Text(room.name)
                .scaledFont(size: 19, weight: .semibold)
                .foregroundStyle(Color.ink)
                .lineLimit(2)
                .multilineTextAlignment(.leading)
            if showsBalance {
                ReceiptDivider().padding(.top, 2)
                balanceRow
            }
        }
        .contentShape(Rectangle())
        .receiptStub(closed: closed)
    }

    @ViewBuilder
    private var balanceRow: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if room.debtsUnavailable {
                // Долги неисчислимы (легаси-данные бота): сервер шлёт myBalance=0 —
                // без этой ветки строка утверждала бы «все в расчёте», хотя долги есть.
                Text(Glossary.debtsUnavailableShort)
                    .scaledFont(size: 14)
                    .foregroundStyle(Color.inkSecondary)
                Spacer(minLength: 0)
            } else if room.exactMyBalanceMinor == 0 {
                Text(Glossary.settled)
                    .scaledFont(size: 14)
                    .foregroundStyle(Color.inkSecondary)
                Spacer(minLength: 0)
            } else {
                Text(Glossary.balanceCaption(room.exactMyBalanceMinor))
                    .scaledFont(size: 14)
                    .foregroundStyle(Color.ink)
                Spacer(minLength: 8)
                MoneyText(room.myBalance, exactMinor: room.exactMyBalanceMinor, size: 15, weight: .bold, currency: room.currency)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// Счётчик непрочитанного на карточке группы: акцентная капсула рядом с
/// названием. Текст готовит `MainTabView.badgeLabel` — правило «99+» обязано
/// совпадать с бейджем вкладки, иначе одно и то же число выглядело бы по-разному.
private struct UnreadBadge: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 11, weight: .bold, design: .monospaced))
            .foregroundStyle(Color.onAccent)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Color.accent, in: RoundedRectangle(cornerRadius: 2, style: .continuous))
    }
}

/// «1 участник», «2 участника», «5 участников» — формы задаёт String Catalog,
/// у каждого языка свой набор.
private func memberCountText(_ count: Int) -> String {
    String(localized: "\(count) участников")
}

// MARK: - Аватар группы

/// Круглый аватар группы: детерминированный пастельный градиент по id комнаты
/// и первая буква названия — через общий UserAvatarView (тот же стиль,
/// что и у аватаров людей).
struct GroupAvatarView: View {
    let roomId: String
    let name: String
    var size: CGFloat = 44
    /// Загруженное фото группы. nil — рисуем градиент по хэшу id, как раньше.
    var avatarFileId: String?

    @Environment(SessionStore.self) private var session

    /// Стабильный (между запусками) хэш id комнаты — задаёт пару градиента.
    private var stableId: Int {
        roomId.unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) & 0x7FFF_FFFF }
    }

    var body: some View {
        Group {
            if let avatarFileId, let image = session.avatars.fileImages[avatarFileId] {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
                    .frame(width: size, height: size)
                    .clipShape(Circle())
            } else {
                UserAvatarView(
                    user: User(id: stableId, username: nil, displayName: String(name.prefix(1))),
                    size: size,
                    // stableId — хэш строки, а НЕ telegram id: фото по нему не
                    // грузим, иначе при совпадении диапазонов группа получала бы
                    // чужое фото.
                    avatarUserId: nil
                )
            }
        }
        .task(id: avatarFileId) {
            guard let avatarFileId else { return }
            await session.avatars.loadFile(avatarFileId, api: session.api)
        }
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
                    VStack(alignment: .leading, spacing: 4) {
                        // Архивная группа открывается так же, как обычная
                        // (внутри — read-only бейдж «Группа в архиве»).
                        NavigationLink(value: GroupsRoute.room(id: room.id)) {
                            GroupCardRow(room: room, showsBalance: false, closed: true)
                        }
                        .buttonStyle(.plain)
                        .contextMenu {
                            Button("Вернуть из архива", systemImage: "arrow.uturn.backward") {
                                Task {
                                    await model.unarchive(repo: session.repo, roomId: room.id)
                                    session.noteDataChanged()
                                }
                            }
                        }
                        // Кнопка «Разархивировать» — СОСЕД NavigationLink, а не
                        // часть его label: вложенная кнопка конфликтовала хит-зоной
                        // с переходом в группу.
                        Button("Разархивировать") {
                            Task {
                                await model.unarchive(repo: session.repo, roomId: room.id)
                                session.noteDataChanged()
                            }
                        }
                        .buttonStyle(.textLink)
                    }
                }
            }
            .padding(20)
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
    GroupsListView(path: .constant([]))
        .environment(SessionStore())
}
