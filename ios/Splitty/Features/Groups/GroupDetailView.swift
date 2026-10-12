import SwiftUI

/// Экран группы: hero-карточка долга, чипы действий, операции по месяцам
/// карточными секциями, плавающая кнопка «+ Расход», настройки группы.
struct GroupDetailView: View {
    let roomId: String

    /// Вкладки нижнего бара тусы (контекстная навигация вместо глобальных
    /// табов). `.add` — заглушка под центральную кнопку «+», как в MainTabView.
    enum TusaTab: Hashable {
        case operations, balances, add, totals, settings
    }

    @Environment(SessionStore.self) private var session
    @State private var model = GroupDetailViewModel()
    @State private var tusaTab: TusaTab = .operations
    @State private var isSettleUpPresented = false
    @State private var isAddExpensePresented = false
    @State private var isInvitePresented = false
    /// Фильтр списка операций: только те, где я донор или в получателях
    /// (аналог фильтра «Мои операции» в телеграм-боте).
    @State private var isMineOnly = false
    /// Фильтр «только мешающие выходу»: включается из отказа в настройках.
    @State private var isBlockingOnly = false
    /// Сводка документа уехала за верх экрана: внизу показывается её короткая
    /// строка, чтобы остаток и расчёт не терялись за длинной историей.
    @State private var isSummaryScrolledAway = false
    @Environment(\.dynamicTypeSize) private var typeSize
    /// Локальная запись outbox, открытая на редактирование (sheet).
    @State private var editingEntry: OutboxEntry?
    /// Задача перезагрузки по dataVersion: держим ссылку и отменяем прежнюю —
    /// иначе серия мутаций плодит параллельные загрузки одной и той же комнаты.
    @State private var reloadTask: Task<Void, Never>?

    init(roomId: String) {
        self.roomId = roomId
    }

    /// nil, пока профиль не загружен: подписи и кнопки, зависящие от meId,
    /// не активируем с фейковым id.
    private var meId: Int? { session.me?.id }

    /// Локальные (неотправленные) операции этой группы, новые первыми —
    /// рендерятся СВЕРХУ списка операций с бейджем «не отправлено».
    private var localEntries: [OutboxEntry] {
        session.outbox.entries(roomId: roomId).reversed()
    }

    var body: some View {
        content
            .background(Color.bg.ignoresSafeArea())
            // Имя тусы — крупно в шапке самого документа; навбар держит
            // только реквизит, как печатный заголовок чека.
            .navigationTitle(String(localized: "Группа"))
            .trackScreen("group")
            .navigationBarTitleDisplayMode(.inline)
            // Туса — один документ: глобальный таб-бар скрыт, разделы
            // переключаются текстовыми вкладками, внизу — одно действие.
            .toolbar(.hidden, for: .tabBar)
            // Скрыть и глобальную overlay-кнопку «+» (см. HidesGlobalAddButtonKey).
            .preference(key: HidesGlobalAddButtonKey.self, value: true)
            .task {
                // Профиль мог не загрузиться на старте (сервер был недоступен) —
                // пробуем ещё раз, экран без meId показывает нейтральный спиннер.
                if session.me == nil {
                    await session.refreshMe()
                }
                await model.load(repo: session.repo, roomId: roomId)
            }
            // Единая инвалидация: перезагрузка после любой мутации данных
            // (расход/платёж/архив из sheet'ов бампают dataVersion сами).
            .onChange(of: session.dataVersion) {
                reloadTask?.cancel()
                reloadTask = Task { await model.load(repo: session.repo, roomId: roomId) }
            }
            .onDisappear { reloadTask?.cancel() }
            // Полноэкранно, а не sheet: форму со введёнными данными нельзя
            // случайно смахнуть — выход только «Отмена»/«Сохранить».
            .fullScreenCover(isPresented: $isAddExpensePresented) {
                AddExpenseView(roomId: roomId)
            }
            // Правка локальной (неотправленной) записи outbox.
            .fullScreenCover(item: $editingEntry) { entry in
                AddExpenseView(roomId: roomId, editEntry: entry)
            }
            .sheet(isPresented: $isSettleUpPresented) {
                if let meId {
                    let myDebts = model.debtsInvolving(meId)
                    SettleUpView(
                        roomId: roomId,
                        currency: model.room?.currency ?? "RUB",
                        fractional: model.room?.fractional ?? false,
                        preselectedDebt: myDebts.count == 1 ? myDebts.first : nil
                    )
                }
            }
            // Выбор друзей, а не шит со ссылкой: главный призыв группы вёл мимо
            // основного способа позвать человека. Ссылка осталась внутри
            // пикера — она нужна тем, с кем расходы ещё не делили
            .sheet(isPresented: $isInvitePresented) {
                if let room = model.room {
                    InviteFriendView(
                        roomId: room.id,
                        existingMemberIds: Set(room.members.map(\.id)),
                        onInvited: { Task { await model.load(repo: session.repo, roomId: roomId) } }
                    )
                }
            }
            .errorAlert($model.alertMessage)
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            FailedStateView(message: message) {
                await retry()
            }
        case .loaded:
            if let room = model.room {
                if let meId {
                    tusaTabView(room: room, meId: meId)
                } else {
                    // Профиль ещё не загружен — нейтральное состояние вместо
                    // неверных подписей «без вас» с фейковым id.
                    // Кнопка обязательна: pull-to-refresh здесь недоступен,
                    // без неё экран застревал бы до повторного входа.
                    ContentUnavailableView {
                        Label("Профиль не загружен", systemImage: "person.crop.circle.badge.exclamationmark")
                    } description: {
                        Text("Не удалось получить данные вашего профиля")
                    } actions: {
                        Button("Повторить") {
                            Task { await retry() }
                        }
                        .buttonStyle(.primaryPill)
                        .frame(maxWidth: 220)
                    }
                }
            }
        }
    }

    /// Повтор после ошибки: профиль мог не загрузиться отдельно от группы.
    private func retry() async {
        if session.me == nil {
            await session.refreshMe()
        }
        await model.load(repo: session.repo, roomId: roomId)
    }

    /// Лента тусы — один документ на бумаге: шапка, сводка остатка, позиции
    /// по месяцам. Сводка стоит НАД позициями: при длинной истории итог и
    /// расчёт не должны прятаться за сотнями строк.
    private func operationsList(room: RoomDetail, meId: Int) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                documentHeader(room: room)
                ReceiptDivider().padding(.vertical, 18)
                headerCard(room: room, meId: meId)
                    .onGeometryChange(for: Bool.self) { proxy in
                        proxy.frame(in: .scrollView).maxY < 0
                    } action: { isSummaryScrolledAway = $0 }
                inviteBanner(room: room)
                ReceiptDivider().padding(.vertical, 18)
                if isBlockingOnly {
                    blockingFilterNote
                } else {
                    mineSegment
                }
                // Локальные (неотправленные) операции — СВЕРХУ списка.
                if !localEntries.isEmpty {
                    localOperationsSection(currency: room.currency)
                }
                if room.operations.isEmpty && localEntries.isEmpty {
                    emptyOperations
                } else {
                    let sections = displaySections(meId: meId)
                    ForEach(sections) { section in
                        monthSection(section, meId: meId, currency: room.currency)
                    }
                    if sections.isEmpty && isMineOnly {
                        Text("Операций с вами нет")
                            .scaledFont(size: 15)
                            .foregroundStyle(Color.inkSecondary)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 24)
                    }
                }
            }
            .receiptPaper(padding: 22)
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 16)
        }
        .refreshable {
            // Pull-to-refresh — триггер синка outbox перед перечиткой.
            await session.syncOutbox()
            await model.load(repo: session.repo, roomId: roomId)
        }
    }

    /// «3 участника» — формы задаёт String Catalog.
    private func membersText(_ count: Int) -> String {
        String(localized: "\(count) участников")
    }

    /// Шапка документа: реквизит (дата · участники), имя тусы, штамп архива.
    private func documentHeader(room: RoomDetail) -> some View {
        VStack(spacing: 8) {
            Text(verbatim: "\(DateFmt.dayMonth(room.createdAt)) · \(membersText(room.members.count))")
                .receiptLabel()
            Text(room.name)
                .scaledFont(size: 26, weight: .heavy, relativeTo: .title)
                .kerning(-0.8)
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            if room.isArchived {
                StatusStamp(text: String(localized: "Группа в архиве"))
            }
        }
        .frame(maxWidth: .infinity)
    }

    /// Плашка фильтра «только мешающие выходу» с выходом из него.
    private var blockingFilterNote: some View {
        HStack(spacing: 10) {
            Text("Показаны только расходы, которые держат вас в группе")
                .scaledFont(size: 13)
                .foregroundStyle(Color.inkSecondary)
            Spacer(minLength: 0)
            Button("Показать все") { isBlockingOnly = false }
                .buttonStyle(.textLink(size: 13))
        }
    }

    /// Секции операций с учётом фильтров: пустые месяцы скрываются.
    private func displaySections(meId: Int) -> [GroupDetailViewModel.MonthSection] {
        // Пришли из отказа в выходе: показываем ровно то, что держит, — совет
        // «уберите себя из расходов» невыполним, пока непонятно, из каких
        if isBlockingOnly, let room = model.room {
            let ids = Set(room.operationsBlockingLeave(for: meId).map(\.id))
            return GroupDetailViewModel.sectionsKeepingOnly(model.sections, ids: ids)
        }
        guard isMineOnly else { return model.sections }
        return model.sections.compactMap { section in
            let ops = section.operations.filter { $0.involves(meId) }
            return ops.isEmpty
                ? nil
                : GroupDetailViewModel.MonthSection(id: section.id, title: section.title, operations: ops)
        }
    }

    /// Приглашение: пока в группе только вы — строка документа со ссылкой.
    /// Прячется, когда участников стало больше, и в архивной группе.
    @ViewBuilder
    private func inviteBanner(room: RoomDetail) -> some View {
        if room.members.count <= 1 && !room.isArchived {
            VStack(alignment: .leading, spacing: 4) {
                ReceiptDivider().padding(.vertical, 14)
                Text("В группе только вы")
                    .scaledFont(size: 16, weight: .semibold)
                    .foregroundStyle(Color.ink)
                // Почему это вообще важно: без второго участника делить нечего.
                Text("Расходы делятся между участниками — пока в группе только вы, делить не с кем")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Пригласить") { isInvitePresented = true }
                    .buttonStyle(.textLink)
            }
        }
    }

    /// Сводка документа: всего, ваша доля, ваш остаток словами и ссылки на
    /// балансы и расчёт (+ подпись про неотправленные операции: их суммы в
    /// балансах сервера не учтены).
    private func headerCard(room: RoomDetail, meId: Int) -> some View {
        let canSettle = !model.debtsInvolving(meId).isEmpty
        return VStack(alignment: .leading, spacing: 12) {
            // При крупном шрифте подпись и сумма в строку не влезают —
            // тогда столбиком, сумма во всю ширину.
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline) {
                    Text("Всего потрачено").receiptLabel().fixedSize()
                    Spacer(minLength: 8)
                    totalSpentText(room)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("Всего потрачено").receiptLabel()
                    totalSpentText(room)
                }
            }
            .accessibilityElement(children: .combine)
            HStack(alignment: .firstTextBaseline) {
                Text("Ваша доля")
                    .scaledFont(size: 14)
                    .foregroundStyle(Color.inkSecondary)
                Spacer(minLength: 8)
                MoneyText(room.mySpent, exactMinor: room.exactMySpentMinor, role: .neutral,
                          size: 14, weight: .medium, currency: room.currency)
            }
            .accessibilityElement(children: .combine)

            ReceiptDivider().padding(.vertical, 4)

            Text("Ваш остаток").receiptLabel()
            debtHero(room: room, meId: meId)

            if !localEntries.isEmpty {
                // Родительный падеж после «без учёта»: 1 → «неотправленной
                // операции», 2 и 5 → «неотправленных операций». Формы —
                // в String Catalog, по правилам языка.
                Text("без учёта \(localEntries.count) неотправленных операций")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            CacheNote(isFromCache: model.isFromCache, updatedAt: model.lastUpdatedAt)

            HStack {
                Button("Все балансы") { tusaTab = .balances }
                    .buttonStyle(.textLink)
                Spacer(minLength: 8)
                // Расчёт живёт рядом с остатком, а не в общем ряду кнопок.
                if canSettle {
                    Button {
                        openSettleUp()
                    } label: {
                        Text("Рассчитаться") + Text(verbatim: " ↗")
                    }
                    .buttonStyle(.textLink)
                    .accessibilityIdentifier("settleUpLink")
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func totalSpentText(_ room: RoomDetail) -> some View {
        MoneyText(room.totalSpent, exactMinor: room.exactTotalSpentMinor, role: .neutral,
                  size: 32, weight: .heavy, currency: room.currency)
            .fixedSize()
    }

    /// Погашения офлайн не работают (зафиксированный дизайн v1).
    private func openSettleUp() {
        if session.isOnline {
            isSettleUpPresented = true
        } else {
            model.alertMessage = String(localized: "Нет соединения. Погашение долга доступно только онлайн")
        }
    }

    /// Карточка локальных операций: строки с бейджем «не отправлено»,
    /// тап открывает форму правки записи outbox.
    private func localOperationsSection(currency: String) -> some View {
        VStack(spacing: 0) {
            ForEach(localEntries) { entry in
                Button {
                    editingEntry = entry
                } label: {
                    LocalOperationRow(entry: entry, currency: currency)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.bottom, 8)
    }

    /// Hero-статус: «Вам должны … ₽» / «Вы должны … ₽ (Имени)» / «Нет долгов».
    @ViewBuilder
    private func debtHero(room: RoomDetail, meId: Int) -> some View {
        if room.debtsUnavailable {
            // Легаси-данные бота: доли не сходятся, сервер шлёт debts=[] и
            // myBalance=0. Ветка обязана быть ПЕРВОЙ — иначе нулевой баланс
            // уходит в «Все участники в расчёте», то есть враньё про деньги.
            VStack(alignment: .leading, spacing: 4) {
                Text(Glossary.debtsUnavailableHero)
                    .scaledFont(size: 20, weight: .bold)
                    .foregroundStyle(Color.ink)
                Text(Glossary.debtsUnavailableSubtitle)
                    .font(.system(size: 14))
                    .foregroundStyle(Color.inkSecondary)
            }
        } else if room.exactMyBalanceMinor > 0 {
            balanceStatement(Text("Вам должны"), room: room)
        } else if room.exactMyBalanceMinor < 0 {
            let creditors = model.debtsOwedBy(meId)
            // Сумма — НЕТТО по группе, а не долг одному кредитору:
            // «должен A 100, C должен мне 30» давало здесь 100, а в
            // списке групп 70 — две разные цифры про одну комнату.
            if creditors.count == 1, let debt = creditors.first {
                balanceStatement(Text("Вы должны \(debt.lender.displayName)"), room: room)
            } else {
                balanceStatement(Text("Вы должны"), room: room)
            }
        } else {
            // Ноль — штамп, а не крупный «0»: это состояние, а не сумма.
            StatusStamp(text: Glossary.settled.localizedUppercase)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// Остаток словами крупно и суммой справа: направление долга читается
    /// текстом, цвет его только подкрепляет.
    private func balanceStatement(_ words: Text, room: RoomDetail) -> some View {
        let wordsView = words
            .scaledFont(size: 22, weight: .heavy, relativeTo: .title3)
            .kerning(-0.5)
            .foregroundStyle(Color.ink)
            .fixedSize(horizontal: false, vertical: true)
        let amount = MoneyText(room.myBalance, exactMinor: room.exactMyBalanceMinor, size: 22,
                               weight: .heavy, currency: room.currency)
            .fixedSize()
        return ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                wordsView
                Spacer(minLength: 8)
                amount
            }
            VStack(alignment: .leading, spacing: 4) {
                wordsView
                amount
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Фильтр позиций «Все | Со мной» — реквизитом над строками.
    private var mineSegment: some View {
        HStack(spacing: 18) {
            Text("Позиции").receiptLabel()
            Spacer(minLength: 8)
            segmentButton("Все", isOn: !isMineOnly) { isMineOnly = false }
            segmentButton("Со мной", isOn: isMineOnly) { isMineOnly = true }
        }
        .padding(.bottom, 6)
    }

    private func segmentButton(_ title: LocalizedStringKey, isOn: Bool, action: @escaping () -> Void) -> some View {
        Button {
            // Отклик только на реальное переключение, не на повторный тап.
            if !isOn { Haptics.tap() }
            action()
        } label: {
            ReceiptTab(title: title, isOn: isOn, size: 13)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isOn ? [.isSelected] : [])
    }

    // MARK: Вкладки тусы

    /// Разделы тусы — текстовые вкладки под навбаром, как у бумажного
    /// документа; внизу одно действие «Добавить расход».
    private func tusaTabView(room: RoomDetail, meId: Int) -> some View {
        VStack(spacing: 0) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 22) {
                    tusaTabButton("Операции", tab: .operations)
                    tusaTabButton("Балансы", tab: .balances)
                    tusaTabButton("Итоги", tab: .totals)
                    tusaTabButton("Настройки", tab: .settings)
                }
                .padding(.horizontal, 20)
                .padding(.top, 6)
            }
            .overlay(alignment: .bottom) { ReceiptDivider(style: .hair) }

            Group {
                switch tusaTab {
                case .operations, .add:
                    operationsList(room: room, meId: meId)
                case .balances:
                    GroupBalancesView(room: room, embedded: true) {}
                case .totals:
                    GroupTotalsView(roomId: roomId, embedded: true)
                case .settings:
                    GroupSettingsView(
                        room: room,
                        embedded: true,
                        onShowBlocking: {
                            isBlockingOnly = true
                            tusaTab = .operations
                        }
                    ) {}
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 10) {
                if tusaTab == .operations && isSummaryScrolledAway {
                    pinnedSummary(room: room)
                }
                Button {
                    isAddExpensePresented = true
                } label: {
                    Label("Добавить расход", systemImage: "plus")
                }
                .buttonStyle(.primaryPill)
            }
            .padding(.horizontal, 20)
            .padding(.top, 10)
            .padding(.bottom, 8)
            .background(Color.bg)
        }
        .animation(.easeInOut(duration: 0.2), value: isSummaryScrolledAway)
    }

    private func tusaTabButton(_ title: LocalizedStringKey, tab: TusaTab) -> some View {
        Button {
            if tusaTab != tab { Haptics.tap() }
            tusaTab = tab
        } label: {
            ReceiptTab(title: title, isOn: tusaTab == tab)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(tusaTab == tab ? [.isSelected] : [])
    }

    /// Короткая сводка над кнопкой, когда шапка документа уехала вверх:
    /// остаток словами и суммой, тап — к балансам.
    @ViewBuilder
    private func pinnedSummary(room: RoomDetail) -> some View {
        Button {
            tusaTab = .balances
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                // На крупном шрифте реквизит и подпись вместе не помещаются —
                // остаётся подпись: без неё сумма не говорит, кто кому должен.
                if !typeSize.isAccessibilitySize {
                    Text("Ваш остаток").receiptLabel(size: 10)
                        .fixedSize()
                    Spacer(minLength: 8)
                }
                if room.debtsUnavailable {
                    Text(Glossary.debtsUnavailableShort)
                        .scaledFont(size: 14)
                        .foregroundStyle(Color.inkSecondary)
                } else if room.exactMyBalanceMinor == 0 {
                    Text(Glossary.settled)
                        .scaledFont(size: 14)
                        .foregroundStyle(Color.inkSecondary)
                } else {
                    Text(Glossary.balanceCaption(room.exactMyBalanceMinor))
                        .scaledFont(size: 14)
                        .foregroundStyle(Color.ink)
                        .fixedSize()
                    if typeSize.isAccessibilitySize { Spacer(minLength: 8) }
                    MoneyText(room.myBalance, exactMinor: room.exactMyBalanceMinor, size: 15, weight: .bold, currency: room.currency)
                        .layoutPriority(2)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(Color.surface, in: RoundedRectangle(cornerRadius: 3, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 3, style: .continuous).strokeBorder(Color.hairline))
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private var emptyOperations: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Добавьте первый расход")
                .scaledFont(size: 17, weight: .semibold)
                .foregroundStyle(Color.ink)
            Text("Кнопка + внизу: продиктуйте, снимите чек или впишите руками")
                .scaledFont(size: 14)
                .foregroundStyle(Color.inkSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 16)
    }

    /// Секция месяца: тихий заголовок + карточка со строками операций.
    private func monthSection(
        _ section: GroupDetailViewModel.MonthSection,
        meId: Int,
        currency: String
    ) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(section.title)
                .receiptLabel()
                .padding(.top, 14)
            VStack(spacing: 0) {
                ForEach(section.operations) { operation in
                    NavigationLink {
                        OperationDetailView(
                            roomId: roomId,
                            operation: operation,
                            currentUserId: meId,
                            currency: currency
                        ) {}
                    } label: {
                        OperationRow(operation: operation, currentUserId: meId, currency: currency)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

}

// MARK: - Строка локальной (неотправленной) операции

/// Строка записи outbox в списке операций группы: колонка даты, иконка,
/// описание и бейдж «icloud.slash + не отправлено» (failed — negative
/// с коротким текстом ошибки), сумма справа. Тап — правка записи.
private struct LocalOperationRow: View {
    let entry: OutboxEntry
    let currency: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.ink)
                    .lineLimit(2)
                statusBadge
            }
            Spacer(minLength: 8)
            MoneyText(
                entry.payload?.sum ?? 0,
                exactMinor: entry.payload.map { $0.sumMinor ?? $0.sum * minorFactor } ?? 0,
                role: .neutral, size: 15, weight: .medium, currency: currency
            )
        }
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private var title: String {
        let description = entry.payload?.description ?? ""
        return description.isEmpty ? String(localized: "Расход") : description
    }

    /// Пометка статуса: дата и «не отправлено» (inkSecondary); failed —
    /// negative + короткий текст ошибки сервера.
    private var statusBadge: some View {
        HStack(spacing: 4) {
            Image(systemName: "icloud.slash")
                .font(.system(size: 10, weight: .semibold))
            Text(verbatim: "\(DateFmt.dayMonth(entry.createdAt)) · \(statusText)")
                .scaledFont(size: 12, relativeTo: .caption)
                .lineLimit(1)
        }
        .foregroundStyle(entry.isFailed ? Color.negative : Color.inkSecondary)
    }

    private var statusText: String {
        if let message = entry.status.failureMessage {
            return String(localized: "не отправлено · \(message)")
        }
        return String(localized: "не отправлено")
    }
}

// MARK: - Строка операции

/// Позиция чека: название и подпись «дата · кто платил» слева, сумма
/// операции справа и под ней моя позиция по ней. Все суммы — в валюте комнаты.
private struct OperationRow: View {
    let operation: Operation
    let currentUserId: Int
    let currency: String
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        // На крупном системном шрифте суммы уходят под название: в одну строку
        // с ним «вы одолжили» рвалось посреди слова.
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 5) {
                    Text(title)
                        .scaledFont(size: 15)
                        .foregroundStyle(Color.ink)
                        .lineLimit(2)
                    if operation.hasFiles {
                        Image(systemName: "paperclip")
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(Color.inkSecondary)
                            .accessibilityLabel("Есть вложения")
                    }
                }
                Text(caption)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Color.inkSecondary)
                    .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
            }
            if !typeSize.isAccessibilitySize { Spacer(minLength: 8) }
            trailing
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    private var title: String {
        if operation.isDebtRepayment {
            return repaymentTitle
        }
        return operation.description.isEmpty ? String(localized: "Расход") : operation.description
    }

    private var repaymentTitle: String {
        let donor = operation.donor
        let recipient = operation.recipients.first?.user
        if donor.id == currentUserId {
            return String(localized: "Вы заплатили \(recipient?.displayName ?? "")")
        }
        if recipient?.id == currentUserId {
            return String(localized: "\(donor.displayName) заплатил(а) вам")
        }
        return String(localized: "\(donor.displayName) заплатил(а) \(recipient?.displayName ?? "")")
    }

    /// «5 окт. · Маша»: когда и кто платил; у перевода — только дата.
    private var caption: String {
        let date = DateFmt.dayMonth(operation.createdAt)
        if operation.isDebtRepayment { return date }
        let payer = operation.donor.id == currentUserId ? String(localized: "Вы") : operation.donor.displayName
        return "\(date) · \(payer)"
    }

    /// Моя позиция по операции: >0 — одолжил, <0 — должен, nil — не участвую.
    /// Доля — ХРАНИМАЯ сумма из `recipients[].sum` (Operation.netPosition):
    /// при делении «по суммам» канонический пересчёт дал бы неверную цифру.
    private var myNet: Int? {
        operation.netPosition(of: currentUserId)
    }

    /// Точная позиция в копейках — по ней и показываем.
    private var myNetMinor: Int? {
        operation.netPositionMinor(of: currentUserId)
    }

    private var trailing: some View {
        VStack(alignment: typeSize.isAccessibilitySize ? .leading : .trailing, spacing: 3) {
            MoneyText(operation.sum, exactMinor: operation.exactMinor, role: .neutral, size: 15,
                      weight: operation.isDebtRepayment ? .regular : .medium, currency: currency)
            if !operation.isDebtRepayment {
                myPosition
            }
        }
    }

    @ViewBuilder
    private var myPosition: some View {
        if let net = myNet, let netMinor = myNetMinor, netMinor != 0 {
            HStack(spacing: 4) {
                Text(netMinor > 0 ? "вы одолжили" : "вы должны")
                    .scaledFont(size: 11, relativeTo: .caption2)
                    .foregroundStyle(Color.inkSecondary)
                    .fixedSize()
                MoneyText(net, exactMinor: netMinor, size: 11, weight: .semibold, currency: currency)
            }
        } else if myNet != nil {
            Text(Glossary.settled)
                .scaledFont(size: 11, relativeTo: .caption2)
                .foregroundStyle(Color.inkSecondary)
        } else {
            Text("без вас")
                .scaledFont(size: 11, relativeTo: .caption2)
                .foregroundStyle(Color.inkSecondary)
        }
    }
}

#Preview {
    NavigationStack {
        GroupDetailView(roomId: "preview")
    }
    .environment(SessionStore())
}
