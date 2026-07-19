import SwiftUI

/// Настройки группы: участники, приглашение (ShareLink с кодом и ссылкой),
/// архивирование/разархивирование — карточными секциями.
struct GroupSettingsView: View {
    private let room: RoomDetail
    private let embedded: Bool
    private let onChange: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var isArchiving = false
    @State private var alertMessage: String?
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

    /// `embedded: true` — вкладка бара тусы (без своего NavigationStack
    /// и кнопки «Готово»); false — прежний самостоятельный sheet.
    init(room: RoomDetail, embedded: Bool = false, onChange: @escaping () -> Void) {
        self.room = room
        self.embedded = embedded
        self.onChange = onChange
        _selectedCurrency = State(initialValue: room.currency)
    }

    /// Ссылка-приглашение, совместимая с deep-link бота.
    private var inviteLink: String {
        "https://t.me/split_money_bot?start=room\(room.id)"
    }

    private var inviteMessage: String {
        "Присоединяйся к группе «\(room.name)» в Splitty: \(inviteLink)\nКод группы: \(room.id)"
    }

    var body: some View {
        if embedded {
            content
        } else {
            NavigationStack {
                content
                    .navigationTitle("Настройки группы")
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
                membersSection
                currencySection
                inviteSection
                archiveSection
            }
            .padding(16)
        }
        .background(Color.bg)
        .task { await loadCurrencies() }
        .errorAlert($alertMessage)
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

    private var membersSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Участники")
                .sectionHeaderStyle()
                .padding(.leading, 4)
            VStack(spacing: 0) {
                ForEach(room.members) { member in
                    HStack(spacing: 12) {
                        UserAvatarView(user: member, size: 36)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(member.id == session.me?.id ? "\(member.displayName) (вы)" : member.displayName)
                                .font(.subheadline.weight(.medium))
                                .foregroundStyle(Color.ink)
                            if let username = member.username, !username.isEmpty {
                                Text("@\(username)")
                                    .font(.caption)
                                    .foregroundStyle(Color.inkSecondary)
                            }
                        }
                        Spacer(minLength: 0)
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

    private var inviteSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(spacing: 0) {
                ShareLink(item: inviteMessage) {
                    HStack {
                        Label("Пригласить в группу", systemImage: "square.and.arrow.up")
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(Color.accent)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 14)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                Rectangle()
                    .fill(Color.hairline)
                    .frame(height: 1)
                    .padding(.leading, 16)
                HStack {
                    Text("Код группы")
                        .font(.subheadline)
                        .foregroundStyle(Color.ink)
                    Spacer(minLength: 8)
                    Text(room.id)
                        .font(.caption.monospaced())
                        .foregroundStyle(Color.inkSecondary)
                        .textSelection(.enabled)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
            }
            .surfaceCard(padding: 0)
            Text("Отправьте другу ссылку или код — по нему можно присоединиться через «Присоединиться по коду».")
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
        }
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
