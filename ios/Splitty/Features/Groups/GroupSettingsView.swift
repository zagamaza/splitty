import PhotosUI
import SwiftUI

/// Настройки группы: участники, приглашение (ShareLink с кодом и ссылкой),
/// архивирование/разархивирование — карточными секциями.
struct GroupSettingsView: View {
    private let room: RoomDetail
    private let embedded: Bool
    /// Переход к расходам, которые держат в группе; nil — вкладки нет (sheet).
    private let onShowBlocking: (() -> Void)?
    private let onChange: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var isArchiving = false
    @State private var alertMessage: String?
    /// Выбор друзей — основной путь приглашения; ссылка осталась запасным.
    @State private var isInviteFriendsPresented = false
    /// Участник, которого собираются убрать (подтверждение).
    @State private var memberToRemove: User?
    @State private var isLeaveConfirmPresented = false
    @State private var isMutating = false
    /// Справочник валют (GET /currencies); nil — ещё грузится.
    @State private var currencies: [CurrencyInfo]?
    /// Текст ошибки загрузки справочника, когда кеша нет: без него секция
    /// застревала бы на вечном спиннере.
    @State private var currenciesError: String?
    /// Текущая валюта группы (локально, чтобы чекмарк обновлялся сразу).
    @State private var selectedCurrency: String
    /// Код валюты, PUT которой сейчас в полёте (спиннер у строки).
    @State private var savingCurrency: String?
    /// Валюта, ожидающая подтверждения смены (confirmationDialog): смена
    /// видна всем участникам группы — без подтверждения слишком легко
    /// сменить случайным тапом.
    @State private var pendingCurrency: CurrencyInfo?
    /// Фото группы: выбранный в пикере элемент и id уже загруженной картинки.
    /// Локальная копия id нужна, чтобы фото сменилось сразу после загрузки, не
    /// дожидаясь перечитывания комнаты.
    @State private var avatarItem: PhotosPickerItem?
    @State private var avatarFileId: String?
    @State private var isAvatarSaving = false

    /// `embedded: true` — вкладка бара тусы (без своего NavigationStack
    /// и кнопки «Готово»); false — прежний самостоятельный sheet.
    init(
        room: RoomDetail,
        embedded: Bool = false,
        onShowBlocking: (() -> Void)? = nil,
        onChange: @escaping () -> Void
    ) {
        self.room = room
        self.embedded = embedded
        self.onShowBlocking = onShowBlocking
        self.onChange = onChange
        _selectedCurrency = State(initialValue: room.currency)
        _avatarFileId = State(initialValue: room.avatarFileId)
    }

    var body: some View {
        if embedded {
            content
        } else {
            NavigationStack {
                content
                    .navigationTitle("Настройки группы")
                    .trackScreen("group_settings")
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
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                avatarSection
                membersSection
                currencySection
                archiveSection
                leaveSection
            }
            .padding(16)
        }
        .background(Color.bg)
        .task { await loadCurrencies() }
        .errorAlert($alertMessage)
        .sheet(isPresented: $isInviteFriendsPresented) {
            InviteFriendView(
                roomId: room.id,
                existingMemberIds: Set(room.members.map(\.id)),
                onInvited: onChange
            )
        }
        .confirmationDialog(
            "Убрать \(memberToRemove?.displayName ?? "") из группы?",
            isPresented: Binding(
                get: { memberToRemove != nil },
                set: { if !$0 { memberToRemove = nil } }
            ),
            titleVisibility: .visible,
            presenting: memberToRemove
        ) { member in
            Button("Убрать", role: .destructive) {
                Task { await removeMember(member) }
            }
            Button("Отмена", role: .cancel) {}
        } message: { _ in
            Text("Расходы, которые уже записаны, останутся в группе")
        }
        .confirmationDialog(
            "Выйти из «\(room.name)»?",
            isPresented: $isLeaveConfirmPresented,
            titleVisibility: .visible
        ) {
            Button("Выйти", role: .destructive) {
                Task { await leaveRoom() }
            }
            Button("Отмена", role: .cancel) {}
        } message: {
            Text("Группа исчезнет из вашего списка. Вернуться можно только по приглашению участника")
        }
        .confirmationDialog(
            "Сменить валюту на \(pendingCurrency?.code ?? "")?",
            isPresented: Binding(
                get: { pendingCurrency != nil },
                set: { if !$0 { pendingCurrency = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingCurrency
        ) { currency in
            Button("Сменить на \(currency.code)") {
                Task { await changeCurrency(to: currency.code) }
            }
            Button("Отмена", role: .cancel) {}
        } message: { _ in
            Text("Суммы не пересчитываются — изменится только обозначение, у всех участников группы")
        }
    }

    // MARK: Секции

    /// Фото группы. Крупная ава + два действия: заменить и убрать. Загрузка
    /// идёт через тот же `ReceiptCapture`, что и снимок чека, — сжатие до
    /// 1024 px уже написано там, второе такое же заводить незачем.
    private var avatarSection: some View {
        VStack(spacing: 12) {
            GroupAvatarView(roomId: room.id, name: room.name, size: 84, avatarFileId: avatarFileId)
                .overlay {
                    if isAvatarSaving {
                        Circle().fill(.black.opacity(0.35))
                        ProgressView().tint(.white)
                    }
                }

            HStack(spacing: 8) {
                PhotosPicker(selection: $avatarItem, matching: .images) {
                    Label(avatarFileId == nil ? "Добавить фото" : "Заменить фото", systemImage: "photo")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.accentText)
                }
                .disabled(isAvatarSaving)
                .accessibilityIdentifier("groupAvatarPick")

                if avatarFileId != nil {
                    Button("Убрать") { Task { await removeAvatar() } }
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Color.inkSecondary)
                        .disabled(isAvatarSaving)
                        .accessibilityIdentifier("groupAvatarRemove")
                }
            }
        }
        .frame(maxWidth: .infinity)
        .surfaceCard(padding: 20)
        .onChange(of: avatarItem) { _, item in
            guard let item else { return }
            Task { await uploadAvatar(item) }
        }
    }

    /// Грузит выбранное фото. Пикер сбрасывается всегда: без этого повторный
    /// выбор того же снимка не менял бы `avatarItem` и не запускал загрузку.
    private func uploadAvatar(_ item: PhotosPickerItem) async {
        defer { avatarItem = nil }
        isAvatarSaving = true
        defer { isAvatarSaving = false }

        let capture = ReceiptCapture()
        guard await capture.load(from: item), let data = capture.imageData else {
            alertMessage = String(localized: "Не удалось прочитать фото")
            return
        }
        do {
            let previous = avatarFileId
            let fileId = try await session.api.setRoomAvatar(roomId: room.id, image: data)
            Analytics.shared.track(.roomSettingsChanged(what: "avatar"))
            avatarFileId = fileId
            if let previous { session.avatars.forgetFile(previous) }
            session.noteDataChanged()
            onChange()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }

    private func removeAvatar() async {
        isAvatarSaving = true
        defer { isAvatarSaving = false }
        do {
            let previous = avatarFileId
            try await session.api.deleteRoomAvatar(roomId: room.id)
            avatarFileId = nil
            if let previous { session.avatars.forgetFile(previous) }
            session.noteDataChanged()
            onChange()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }

    private var membersSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("Участники")
                    .sectionHeaderStyle()
                Spacer(minLength: 8)
                Button {
                    isInviteFriendsPresented = true
                } label: {
                    // «Добавить», а не «Пригласить»: раньше два пункта экрана
                    // назывались одинаково, а вели в разные места
                    Label("Добавить", systemImage: "person.badge.plus")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.accentText)
                }
            }
            .padding(.horizontal, 4)
            VStack(spacing: 0) {
                ForEach(room.members) { member in
                    HStack(spacing: 12) {
                        UserAvatarView(user: member, size: 36)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(memberLabel(member.displayName, isMe: member.id == session.me?.id))
                                .font(.subheadline.weight(.medium))
                                .foregroundStyle(Color.ink)
                            if let username = member.username, !username.isEmpty {
                                Text(verbatim: "@\(username)")
                                    .font(.caption)
                                    .foregroundStyle(Color.inkSecondary)
                            }
                        }
                        Spacer(minLength: 0)
                        // Лекарство от «позвал не того»: убрать участника может
                        // любой в комнате, как и править расходы.
                        if member.id != session.me?.id {
                            Menu {
                                Button("Убрать из группы", role: .destructive) {
                                    memberToRemove = member
                                }
                            } label: {
                                Image(systemName: "ellipsis")
                                    .foregroundStyle(Color.inkSecondary)
                                    .padding(.leading, 8)
                            }
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    if member.id != room.members.last?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                            .padding(.leading, 64)
                    }
                }
            }
            .padding(.vertical, 6)
            .surfaceCard(padding: 0)
        }
    }

    /// Выход из группы — деструктивным стилем внизу экрана.
    private var leaveSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                isLeaveConfirmPresented = true
            } label: {
                HStack {
                    Label("Выйти из группы", systemImage: "rectangle.portrait.and.arrow.right")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Color.negativeText)
                    Spacer(minLength: 0)
                    if isMutating {
                        ProgressView()
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            // Кнопка гаснет ЗАРАНЕЕ: расходы видно в самой комнате, и ждать
            // отказа сервера, чтобы сообщить об этом, незачем
            .disabled(isMutating || !blockingOperations.isEmpty)
            .surfaceCard(padding: 0)
            Text(leaveFooterText)
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
            // Совет «уберите себя из расходов» невыполним, пока непонятно, из
            // каких именно: расходов в группе бывают сотни
            if !blockingOperations.isEmpty, let onShowBlocking {
                Button("Показать эти расходы", action: onShowBlocking)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Color.accentText)
                    .padding(.horizontal, 4)
            }
        }
    }

    /// Расходы, которые держат человека в группе (считаются по комнате в памяти).
    private var blockingOperations: [Operation] {
        guard let meId = session.me?.id else { return [] }
        return room.operationsBlockingLeave(for: meId)
    }

    /// Подпись под кнопкой выхода: пока расходы есть, говорим сколько их и что
    /// с ними делать. Раньше человек узнавал об этом отказом сервера после тапа
    private var leaveFooterText: String {
        let count = blockingOperations.count
        if count == 0 {
            return String(localized: "Группа исчезнет из вашего списка. Вернуться можно только по приглашению участника")
        }
        return String(localized: "На вас записано расходов: \(count). Уберите себя из них, а если платили вы — смените плательщика или удалите расход. Править расходы может любой участник")
    }

    private func leaveRoom() async {
        isMutating = true
        defer { isMutating = false }
        do {
            try await session.api.leaveRoom(roomId: room.id)
            Analytics.shared.track(.roomLeft)
            session.noteDataChanged()
            session.confirm(String(localized: "Вы вышли из группы"))
            onChange()
            dismiss()
        } catch {
            alertMessage = leaveErrorText(error)
        }
    }

    private func removeMember(_ member: User) async {
        isMutating = true
        defer { isMutating = false }
        do {
            try await session.api.removeMember(roomId: room.id, userId: member.id)
            Analytics.shared.track(.memberRemoved)
            session.noteDataChanged()
            session.confirm(String(localized: "\(member.displayName) убран(а) из группы"))
            onChange()
        } catch {
            alertMessage = leaveErrorText(error, isSelf: false)
        }
    }

    /// Пикер «Валюта»: строки из GET /currencies (флаг + код), чекмарк
    /// у текущей; тап — PUT /rooms/{id}/currency и единая инвалидация.
    private var currencySection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Валюта")
                .sectionHeaderStyle()
                .padding(.leading, 4)
            VStack(spacing: 0) {
                if let currencies {
                    ForEach(currencies) { currency in
                        currencyRow(currency)
                        if currency.id != currencies.last?.id {
                            Rectangle()
                                .fill(Color.hairline)
                                .frame(height: 1)
                                .padding(.leading, 52)
                        }
                    }
                } else if let currenciesError {
                    // Справочник не загрузился и кеша нет — честная ошибка
                    // с повтором вместо вечного спиннера.
                    VStack(spacing: 10) {
                        Text(currenciesError)
                            .font(.caption)
                            .foregroundStyle(Color.inkSecondary)
                            .multilineTextAlignment(.center)
                        Button("Повторить") {
                            Task { await loadCurrencies() }
                        }
                        .buttonStyle(.softChip)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 16)
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 20)
                }
            }
            .padding(.vertical, 2)
            .surfaceCard(padding: 0)
            Text("В валюте группы показываются все её суммы: расходы, долги и итоги.")
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
        }
    }

    private func currencyRow(_ currency: CurrencyInfo) -> some View {
        Button {
            // Не сам PUT, а подтверждение: смена валюты видна всем участникам.
            if currency.code != selectedCurrency {
                pendingCurrency = currency
            }
        } label: {
            HStack(spacing: 12) {
                Text(currency.flag)
                    .font(.system(size: 22))
                Text(currency.code)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                Text(currency.symbol)
                    .font(.subheadline)
                    .foregroundStyle(Color.inkSecondary)
                Spacer(minLength: 8)
                if savingCurrency == currency.code {
                    ProgressView()
                } else if selectedCurrency == currency.code {
                    Image(systemName: "checkmark")
                        .fontWeight(.semibold)
                        .foregroundStyle(Color.accent)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(savingCurrency != nil)
    }

    private var archiveSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                Task { await toggleArchive() }
            } label: {
                HStack {
                    if room.isArchived {
                        Label("Разархивировать", systemImage: "tray.and.arrow.up")
                            .foregroundStyle(Color.accent)
                    } else {
                        // Обычный ink, не «опасный» красный: действие обратимо
                        // и скрывает группу только у самого пользователя.
                        Label("Архивировать", systemImage: "archivebox")
                            .foregroundStyle(Color.ink)
                    }
                    Spacer(minLength: 0)
                    if isArchiving {
                        ProgressView()
                    }
                }
                .font(.subheadline.weight(.medium))
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(isArchiving)
            .surfaceCard(padding: 0)
            Text("Архив скрывает группу из списка только у вас. Операции и долги сохраняются.")
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
        }
    }

    // MARK: Действия

    /// Загружает справочник валют для пикера. Через офлайн-кеш: без сети
    /// пикер рисуется по последнему успешному ответу справочника; ошибка
    /// без кеша — состояние в секции с «Повторить», с кешем — алерт.
    private func loadCurrencies() async {
        guard currencies == nil else { return }
        currenciesError = nil
        do {
            let result = try await session.repo.currencies { [self] cached in
                if currencies == nil {
                    currencies = cached
                }
            }
            currencies = result.value
        } catch {
            // Закрытие sheet посреди запроса — не ошибка (конвенция проекта).
            if error.isTaskCancellation { return }
            if currencies == nil {
                // Кеша нет — ошибка живёт в самой секции с кнопкой «Повторить»:
                // алерт закрывается, и остался бы вечный спиннер.
                currenciesError = humanErrorText(error)
            } else {
                alertMessage = humanErrorText(error)
            }
        }
    }

    /// PUT /rooms/{id}/currency: меняет валюту группы, затем единая инвалидация.
    private func changeCurrency(to code: String) async {
        guard code != selectedCurrency, savingCurrency == nil else { return }
        savingCurrency = code
        defer { savingCurrency = nil }
        do {
            try await session.api.setRoomCurrency(roomId: room.id, currency: code)
            Analytics.shared.track(.roomSettingsChanged(what: "currency"))
            selectedCurrency = code
            Haptics.tap()
            // Единая инвалидация: экран группы и списки перечитают суммы
            // уже в новой валюте.
            session.noteDataChanged()
            onChange()
        } catch {
            if error.isTaskCancellation { return }
            alertMessage = humanErrorText(error)
        }
    }

    private func toggleArchive() async {
        isArchiving = true
        defer { isArchiving = false }
        do {
            if room.isArchived {
                try await session.api.unarchiveRoom(id: room.id)
            } else {
                try await session.api.archiveRoom(id: room.id)
            }
            // Единая инвалидация: экран группы и списки перезагрузятся по dataVersion.
            session.noteDataChanged()
            Haptics.success()
            onChange()
            dismiss()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }
}

// MARK: - Тексты отказов при выходе и удалении участника

/// Человеческий текст отказа `409` при выходе (`isSelf`) или удалении участника.
///
/// Текст свой, а не серверный: отказ обязан объяснять путь наружу — правка и
/// удаление расхода открыты любому участнику, поэтому себя (или его) можно
/// убрать из операции и выйти. Без этого человек упирается в глухое «конфликт».
func leaveErrorText(_ error: Error, isSelf: Bool = true) -> String {
    guard let apiError = error as? APIError, case .server(_, let code, _, _) = apiError else {
        return humanErrorText(error)
    }
    switch code {
    case "has_operations":
        return isSelf
            ? String(localized: "На вас записаны расходы. Уберите себя из них, а если платили вы — смените плательщика или удалите расход. Править расходы может любой участник. После этого выход сработает")
            : String(localized: "На участнике записаны расходы. Уберите его из них, а если платил он — смените плательщика или удалите расход. После этого его можно будет убрать")
    case "last_member":
        // Без ветки isSelf: последним участником можешь быть только ты сам —
        // убирать в такой комнате больше некого.
        return String(localized: "Вы последний участник. Заархивируйте группу, если она больше не нужна")
    default:
        return humanErrorText(error)
    }
}
