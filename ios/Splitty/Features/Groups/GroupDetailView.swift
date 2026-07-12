import SwiftUI

/// Экран группы: hero-карточка долга, чипы действий, операции по месяцам
/// карточными секциями, плавающая кнопка «+ Расход», настройки группы.
struct GroupDetailView: View {
    let roomId: String

    /// Вкладки нижнего бара тусы (контекстная навигация вместо глобальных табов).
    enum TusaTab: Hashable {
        case operations, balances, totals
    }

    @Environment(SessionStore.self) private var session
    @State private var model = GroupDetailViewModel()
    @State private var tusaTab: TusaTab = .operations
    @State private var isSettleUpPresented = false
    @State private var isSettingsPresented = false
    @State private var isAddExpensePresented = false
    /// Фильтр списка операций: только те, где я донор или в получателях
    /// (аналог фильтра «Мои операции» в телеграм-боте).
    @State private var isMineOnly = false
    /// Локальная запись outbox, открытая на редактирование (sheet).
    @State private var editingEntry: OutboxEntry?

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
            .navigationTitle(model.room?.name ?? "Группа")
            .navigationBarTitleDisplayMode(.large)
            // Туса — «центр мира»: глобальный таб-бар скрыт, внизу — бар тусы.
            .toolbar(.hidden, for: .tabBar)
            // Скрыть и глобальную overlay-кнопку «+» (см. HidesGlobalAddButtonKey).
            .preference(key: HidesGlobalAddButtonKey.self, value: true)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                tusaBar
            }
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
                Task { await model.load(repo: session.repo, roomId: roomId) }
            }
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
                        preselectedDebt: myDebts.count == 1 ? myDebts.first : nil
                    )
                }
            }
            .sheet(isPresented: $isSettingsPresented) {
                if let room = model.room {
                    GroupSettingsView(room: room) {}
                }
            }
            .alert("Ошибка", isPresented: alertPresented) {
                Button("Ок", role: .cancel) {}
            } message: {
                Text(model.alertMessage ?? "")
            }
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { model.alertMessage != nil },
            set: { if !$0 { model.alertMessage = nil } }
        )
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
            } description: {
                Text(message)
            } actions: {
                Button("Повторить") {
                    Task { await retry() }
                }
            }
        case .loaded:
            if let room = model.room {
                if let meId {
                    switch tusaTab {
                    case .operations:
                        operationsList(room: room, meId: meId)
                    case .balances:
                        GroupBalancesView(room: room, embedded: true) {}
                    case .totals:
                        GroupTotalsView(roomId: roomId, embedded: true)
                    }
                } else {
                    // Профиль ещё не загружен — нейтральное состояние вместо
                    // неверных подписей «не участвует» с фейковым id.
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
                        .buttonStyle(.borderedProminent)
                        .tint(Color.accent)
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

    private func operationsList(room: RoomDetail, meId: Int) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                headerCard(room: room, meId: meId)
                mineSegment
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
                            .font(.system(size: 15, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 24)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 4)
            .padding(.bottom, 16)
        }
        .refreshable {
            // Pull-to-refresh — триггер синка outbox перед перечиткой.
            await session.syncOutbox()
            await model.load(repo: session.repo, roomId: roomId)
        }
    }

    /// Секции операций с учётом фильтра «Со мной»: пустые месяцы скрываются.
    private func displaySections(meId: Int) -> [GroupDetailViewModel.MonthSection] {
        guard isMineOnly else { return model.sections }
        return model.sections.compactMap { section in
            let ops = section.operations.filter { $0.involves(meId) }
            return ops.isEmpty
                ? nil
                : GroupDetailViewModel.MonthSection(id: section.id, title: section.title, operations: ops)
        }
    }

    /// Hero-карточка статуса долга (+ бейдж архива, + подпись про
    /// неотправленные операции: их суммы в балансах сервера не учтены).
    private func headerCard(room: RoomDetail, meId: Int) -> some View {
        let canSettle = !model.debtsInvolving(meId).isEmpty
        return VStack(alignment: .leading, spacing: 10) {
            if room.isArchived {
                Label("Группа в архиве", systemImage: "archivebox")
                    .font(.system(size: 13, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
            HStack(alignment: .center, spacing: 12) {
                debtHero(room: room, meId: meId)
                Spacer(minLength: 0)
                // «Погасить» живёт рядом с долгом, а не в общем ряду кнопок.
                if canSettle {
                    Button {
                        // Погашения офлайн не работают (зафиксированный дизайн v1).
                        if session.isOnline {
                            isSettleUpPresented = true
                        } else {
                            model.alertMessage = "Нет соединения. Погашение долга доступно только онлайн"
                        }
                    } label: {
                        Text("Погасить")
                            .font(.system(size: 15, weight: .semibold, design: .rounded))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 20)
                            .padding(.vertical, 10)
                            .background(Color.accent, in: Capsule())
                    }
                    .buttonStyle(.plain)
                }
            }
            if !localEntries.isEmpty {
                Text("без учёта \(localEntries.count) неотправленных")
                    .font(.system(size: 13, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(padding: 20)
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
                if entry.id != localEntries.last?.id {
                    Rectangle()
                        .fill(Color.hairline)
                        .frame(height: 1)
                        .padding(.leading, 62)
                }
            }
        }
        .surfaceCard(padding: 0)
    }

    /// Hero-статус: «Вам должны … ₽» / «Вы должны … ₽ (Имени)» / «Нет долгов».
    @ViewBuilder
    private func debtHero(room: RoomDetail, meId: Int) -> some View {
        if room.myBalance > 0 {
            VStack(alignment: .leading, spacing: 4) {
                Text("Вам должны")
                    .sectionHeaderStyle()
                MoneyText(room.myBalance, size: 40, currency: room.currency)
            }
        } else if room.myBalance < 0 {
            let creditors = model.debtsOwedBy(meId)
            VStack(alignment: .leading, spacing: 4) {
                if creditors.count == 1, let debt = creditors.first {
                    Text("Вы должны \(debt.lender.displayName)")
                        .sectionHeaderStyle()
                    MoneyText(debt.sum, role: .negative, size: 40, currency: room.currency)
                } else {
                    Text("Вы должны")
                        .sectionHeaderStyle()
                    MoneyText(room.myBalance, size: 40, currency: room.currency)
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 4) {
                Text("Нет долгов")
                    .font(.system(size: 22, weight: .semibold, design: .rounded))
                    .foregroundStyle(Color.ink)
                Text("Все участники в расчёте")
                    .font(.system(size: 14))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
    }

    /// Сегмент фильтра операций: «Все | Со мной».
    private var mineSegment: some View {
        HStack(spacing: 0) {
            segmentButton("Все", isOn: !isMineOnly) { isMineOnly = false }
            segmentButton("Со мной", isOn: isMineOnly) { isMineOnly = true }
        }
        .padding(3)
        .background(Color.surface, in: Capsule())
        .overlay(Capsule().stroke(Color.hairline, lineWidth: 1))
        .animation(.spring(duration: 0.25), value: isMineOnly)
    }

    private func segmentButton(_ title: String, isOn: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 13.5, weight: .semibold, design: .rounded))
                .foregroundStyle(isOn ? .white : Color.inkSecondary)
                .padding(.horizontal, 16)
                .padding(.vertical, 6)
                .background(isOn ? Color.accent : .clear, in: Capsule())
        }
        .buttonStyle(.plain)
    }

    // MARK: Нижний бар тусы

    /// Контекстный таб-бар тусы: [Операции][Балансы] (+) [Итоги][Настройки].
    /// Заменяет глобальный таб-бар, пока пользователь внутри тусы.
    private var tusaBar: some View {
        HStack(alignment: .bottom, spacing: 0) {
            tusaTabButton("Операции", icon: "list.bullet", tab: .operations)
            tusaTabButton("Балансы", icon: "arrow.left.arrow.right", tab: .balances)
            Button {
                isAddExpensePresented = true
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: 24, weight: .medium))
                    .foregroundStyle(.white)
                    .frame(width: 52, height: 52)
                    .background(Color.accent, in: Circle())
                    .shadow(color: Color.accent.opacity(0.25), radius: 5, y: 2)
            }
            .padding(.bottom, 6)
            .frame(maxWidth: .infinity)
            .accessibilityLabel("Добавить расход")
            .disabled(model.room == nil)
            tusaTabButton("Итоги", icon: "chart.pie", tab: .totals)
            tusaBarButton("Настройки", icon: "gearshape") {
                isSettingsPresented = true
            }
            .disabled(model.room == nil)
        }
        .padding(.top, 8)
        .padding(.bottom, 2)
        .background(.bar)
        .overlay(alignment: .top) {
            Rectangle().fill(Color.hairline).frame(height: 1)
        }
    }

    private func tusaTabButton(_ title: String, icon: String, tab: TusaTab) -> some View {
        tusaBarButton(title, icon: icon, isActive: tusaTab == tab) {
            tusaTab = tab
        }
    }

    private func tusaBarButton(
        _ title: String,
        icon: String,
        isActive: Bool = false,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            VStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 20))
                Text(title)
                    .font(.system(size: 10, weight: .semibold, design: .rounded))
            }
            .foregroundStyle(isActive ? Color.accent : Color.inkSecondary)
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.plain)
    }

    private var emptyOperations: some View {
        ContentUnavailableView {
            Label("Пока нет расходов", systemImage: "doc.plaintext")
        } description: {
            Text("Добавьте первый расход кнопкой «+ Расход»")
        }
        .frame(maxWidth: .infinity)
        .surfaceCard(padding: 8)
    }

    /// Секция месяца: тихий заголовок + карточка со строками операций.
    private func monthSection(
        _ section: GroupDetailViewModel.MonthSection,
        meId: Int,
        currency: String
    ) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(section.title)
                .sectionHeaderStyle()
                .padding(.leading, 4)
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
                    // Тонкий разделитель — только между строками внутри карточки.
                    if operation.id != section.operations.last?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                            .padding(.leading, 62)
                    }
                }
            }
            .surfaceCard(padding: 0)
        }
    }

}

// MARK: - Стиль FAB

/// Акцентный pill-FAB с мягкой цветной тенью и spring-нажатием.
private struct FabPillButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .semibold, design: .rounded))
            .foregroundStyle(.white)
            .padding(.horizontal, 22)
            .frame(minHeight: 54)
            .background(
                configuration.isPressed ? Color.accentPressed : Color.accent,
                in: Capsule()
            )
            .shadow(color: Color.accentPressed.opacity(0.35), radius: 12, x: 0, y: 6)
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.spring(duration: 0.25), value: configuration.isPressed)
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
        HStack(spacing: 12) {
            dateColumn
            iconBox
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                    .lineLimit(1)
                statusBadge
            }
            Spacer(minLength: 8)
            MoneyText(entry.payload?.sum ?? 0, role: .neutral, size: 15, currency: currency)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
    }

    private var title: String {
        let description = entry.payload?.description ?? ""
        return description.isEmpty ? "Расход" : description
    }

    /// Бейдж статуса: «не отправлено» (inkSecondary); failed — negative
    /// + короткий текст ошибки сервера.
    private var statusBadge: some View {
        HStack(spacing: 4) {
            Image(systemName: "icloud.slash")
                .font(.system(size: 10, weight: .semibold))
            Text(statusText)
                .font(.caption)
                .lineLimit(1)
        }
        .foregroundStyle(entry.isFailed ? Color.negative : Color.inkSecondary)
    }

    private var statusText: String {
        if let message = entry.status.failureMessage {
            return "не отправлено · \(message)"
        }
        return "не отправлено"
    }

    // Колонка даты — в стиле строк операций («июл» сверху, «5» снизу).
    private var dateColumn: some View {
        let parts = DateFmt.dayMonth(entry.createdAt).split(separator: " ")
        return VStack(spacing: 0) {
            Text(parts.count > 1 ? String(parts[1]) : "")
                .font(.caption2)
            Text(parts.first.map(String.init) ?? "")
                .font(.system(size: 16, weight: .semibold, design: .rounded))
                .monospacedDigit()
        }
        .foregroundStyle(Color.inkSecondary)
        .frame(width: 34)
    }

    private var iconBox: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(Color.ink.opacity(0.06))
            .frame(width: 36, height: 36)
            .overlay {
                Image(systemName: "doc.plaintext")
                    .font(.system(size: 16))
                    .foregroundStyle(Color.inkSecondary)
            }
    }
}

// MARK: - Строка операции

/// Карточная строка операции: колонка даты, иконка, описание,
/// моя позиция справа (MoneyText). Все суммы — в валюте комнаты.
private struct OperationRow: View {
    let operation: Operation
    let currentUserId: Int
    let currency: String

    var body: some View {
        HStack(spacing: 12) {
            dateColumn
            iconBox
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                    .lineLimit(1)
                if let subtitle {
                    Text(subtitle)
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            trailing
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
    }

    // Колонка даты: «июл» сверху, «5» снизу, вторичным цветом.
    private var dateColumn: some View {
        let parts = DateFmt.dayMonth(operation.createdAt).split(separator: " ")
        return VStack(spacing: 0) {
            Text(parts.count > 1 ? String(parts[1]) : "")
                .font(.caption2)
            Text(parts.first.map(String.init) ?? "")
                .font(.system(size: 16, weight: .semibold, design: .rounded))
                .monospacedDigit()
        }
        .foregroundStyle(Color.inkSecondary)
        .frame(width: 34)
    }

    private var iconBox: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(operation.isDebtRepayment ? Color.accent.opacity(0.14) : Color.ink.opacity(0.06))
            .frame(width: 36, height: 36)
            .overlay {
                Image(systemName: operation.isDebtRepayment ? "banknote" : "doc.plaintext")
                    .font(.system(size: 16))
                    .foregroundStyle(operation.isDebtRepayment ? Color.accent : Color.inkSecondary)
            }
            .overlay(alignment: .bottomTrailing) {
                if operation.hasFiles {
                    Image(systemName: "paperclip.circle.fill")
                        .font(.system(size: 13))
                        .foregroundStyle(Color.accent, Color.surface)
                        .offset(x: 4, y: 4)
                }
            }
    }

    private var title: String {
        if operation.isDebtRepayment {
            return repaymentTitle
        }
        return operation.description.isEmpty ? "Расход" : operation.description
    }

    private var repaymentTitle: String {
        let donor = operation.donor
        let recipient = operation.recipients.first?.user
        if donor.id == currentUserId {
            return "Вы заплатили \(recipient?.displayName ?? "")"
        }
        if recipient?.id == currentUserId {
            return "\(donor.displayName) заплатил(а) вам"
        }
        return "\(donor.displayName) заплатил(а) \(recipient?.displayName ?? "")"
    }

    private var subtitle: String? {
        if operation.isDebtRepayment {
            return nil
        }
        if operation.donor.id == currentUserId {
            return "Вы заплатили \(money(operation.sum, currency: currency))"
        }
        return "\(operation.donor.displayName) заплатил(а) \(money(operation.sum, currency: currency))"
    }

    /// Моя позиция по операции: >0 — одолжил, <0 — должен, nil — не участвую.
    /// Доля — ХРАНИМАЯ сумма из `recipients[].sum` (Operation.netPosition):
    /// при делении «по суммам» канонический пересчёт дал бы неверную цифру.
    private var myNet: Int? {
        operation.netPosition(of: currentUserId)
    }

    @ViewBuilder
    private var trailing: some View {
        if operation.isDebtRepayment {
            MoneyText(operation.sum, role: .neutral, size: 15, weight: .regular, currency: currency)
        } else if let net = myNet, net != 0 {
            VStack(alignment: .trailing, spacing: 2) {
                Text(net > 0 ? "вы одолжили" : "вы должны")
                    .font(.caption2)
                    .foregroundStyle(Color.inkSecondary)
                MoneyText(net, size: 15, currency: currency)
            }
        } else if myNet != nil {
            Text("расчёт")
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
        } else {
            Text("не участвует")
                .font(.caption)
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
