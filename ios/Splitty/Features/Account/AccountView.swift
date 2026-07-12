import SwiftUI

/// Вкладка «Профиль»: профиль-шапка с большим аватаром, секции настроек
/// карточками, сервер и выход из аккаунта.
struct AccountView: View {
    @AppStorage(AppTheme.storageKey) private var themeRaw = AppTheme.system.rawValue
    @Environment(SessionStore.self) private var session

    @State private var nameDraft = ""
    @State private var isEditNamePresented = false
    @State private var isLogoutConfirmPresented = false
    @State private var errorMessage: String?

    // Локальные копии настроек: правки применяются через PATCH /me,
    // при ошибке откатываются к значениям из профиля.
    @State private var lang = "ru"
    @State private var notificationOn = true

    init() {}

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    headerSection
                    settingsSection
                    serverSection
                    logoutSection
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
            }
            .background(Color.bg)
            // Запас под центральную кнопку «+» — как на остальных вкладках.
            .contentMargins(.bottom, 40, for: .scrollContent)
            .navigationTitle("Профиль")
        }
        .task {
            await session.refreshMe()
            syncFromMe()
        }
        .onChange(of: session.me) {
            syncFromMe()
        }
        .alert("Изменить имя", isPresented: $isEditNamePresented) {
            TextField("Имя", text: $nameDraft)
            Button("Сохранить") { saveName() }
            Button("Отмена", role: .cancel) {}
        }
        .confirmationDialog(
            logoutConfirmTitle,
            isPresented: $isLogoutConfirmPresented,
            titleVisibility: .visible
        ) {
            Button("Выйти", role: .destructive) {
                // Вместе с сессией чистятся офлайн-кеш и outbox
                // (неотправленные операции пропадут — о них предупреждает title).
                session.logout()
            }
            Button("Отмена", role: .cancel) {}
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

    /// Заголовок подтверждения выхода: при непустом outbox предупреждает,
    /// что неотправленные операции будут удалены вместе с кешем.
    private var logoutConfirmTitle: String {
        let pending = session.outbox.entries.count
        guard pending > 0 else { return "Выйти из аккаунта?" }
        return "Есть \(pending) неотправленных операций — выйти и удалить их?"
    }

    // MARK: - Секции

    /// Профиль-шапка: большой градиентный аватар, имя, @username, id.
    @ViewBuilder
    private var headerSection: some View {
        if let me = session.me {
            VStack(spacing: 14) {
                UserAvatarView(
                    user: User(id: me.id, username: me.username, displayName: me.displayName),
                    size: 88
                )
                VStack(spacing: 2) {
                    Text(me.displayName)
                        .font(.system(size: 24, weight: .semibold, design: .rounded))
                        .foregroundStyle(Color.ink)
                    if let username = me.username, !username.isEmpty {
                        Text("@\(username)")
                            .font(.system(size: 15, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                    }
                    Text("ID: \(String(me.id))")
                        .font(.system(size: 12, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(Color.inkSecondary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 12)
        }
    }

    /// Карточка настроек: имя, язык, уведомления — с hairline-разделителями.
    private var settingsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Настройки")
                .sectionHeaderStyle()
                .padding(.horizontal, 4)
            VStack(spacing: 0) {
                nameRow
                rowDivider
                langRow
                rowDivider
                themeRow
                rowDivider
                notificationsLink
                rowDivider
                notificationRow
            }
            .surfaceCard(padding: 0)
        }
    }

    /// Строка «Имя»: текущее displayName, тап открывает alert редактирования.
    private var nameRow: some View {
        Button {
            nameDraft = session.me?.displayName ?? ""
            isEditNamePresented = true
        } label: {
            HStack(spacing: 12) {
                Text("Имя")
                    .font(.system(size: 16, design: .rounded))
                    .foregroundStyle(Color.ink)
                Spacer(minLength: 8)
                Text(session.me?.displayName ?? "")
                    .font(.system(size: 16, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                    .lineLimit(1)
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Color.inkSecondary.opacity(0.6))
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// Строка «Язык»: menu-picker справа (ru/en).
    private var langRow: some View {
        HStack(spacing: 12) {
            Text("Язык")
                .font(.system(size: 16, design: .rounded))
                .foregroundStyle(Color.ink)
            Spacer(minLength: 8)
            Picker("Язык", selection: $lang) {
                Text("Русский").tag("ru")
                Text("English").tag("en")
            }
            .pickerStyle(.menu)
            .labelsHidden()
            .tint(Color.inkSecondary)
            .onChange(of: lang) { _, newValue in
                guard newValue != session.me?.lang else { return }
                Task { await save(lang: newValue) }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 7)
    }

    /// Строка «Тема»: menu-picker (системная/светлая/тёмная), UserDefaults.
    private var themeRow: some View {
        HStack(spacing: 12) {
            Text("Тема")
                .font(.system(size: 16, design: .rounded))
                .foregroundStyle(Color.ink)
            Spacer(minLength: 8)
            Picker("Тема", selection: $themeRaw) {
                ForEach(AppTheme.allCases, id: \.rawValue) { theme in
                    Text(theme.title).tag(theme.rawValue)
                }
            }
            .pickerStyle(.menu)
            .labelsHidden()
            .tint(Color.inkSecondary)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 7)
    }

    /// Строка «Уведомления»: переход к настройкам категорий и каналов.
    private var notificationsLink: some View {
        NavigationLink {
            NotificationSettingsView()
        } label: {
            HStack(spacing: 12) {
                Text("Уведомления")
                    .font(.system(size: 16, design: .rounded))
                    .foregroundStyle(Color.ink)
                Spacer(minLength: 8)
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Color.inkSecondary.opacity(0.6))
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// Строка «Уведомления»: toggle с акцентным tint.
    private var notificationRow: some View {
        Toggle(isOn: $notificationOn) {
            Text("Уведомления")
                .font(.system(size: 16, design: .rounded))
                .foregroundStyle(Color.ink)
        }
        .tint(Color.accent)
        .onChange(of: notificationOn) { _, newValue in
            guard newValue != session.me?.notificationOn else { return }
            Task { await save(notificationOn: newValue) }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 11)
    }

    /// Карточка «Сервер»: текущий base URL и версия приложения (read-only, мелко).
    private var serverSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Сервер")
                .sectionHeaderStyle()
                .padding(.horizontal, 4)
            VStack(alignment: .leading, spacing: 6) {
                Text(session.baseURLString)
                    .font(.system(size: 13, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
                Text("Версия \(Self.appVersion)")
                    .font(.system(size: 13, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .surfaceCard()
        }
    }

    /// «1.2 (3)» из бандла — чтобы отличать сборки в TestFlight/на устройстве.
    static var appVersion: String {
        let short = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "?"
        return "\(short) (\(build))"
    }

    /// Выход из аккаунта: карточка-кнопка с negative-текстом.
    private var logoutSection: some View {
        Button(role: .destructive) {
            isLogoutConfirmPresented = true
        } label: {
            Text("Выйти")
                .font(.system(size: 16, weight: .semibold, design: .rounded))
                .foregroundStyle(Color.negative)
                .frame(maxWidth: .infinity)
                .surfaceCard()
        }
        .buttonStyle(.plain)
    }

    /// Hairline-разделитель между строками внутри карточки.
    private var rowDivider: some View {
        Rectangle()
            .fill(Color.hairline)
            .frame(height: 1)
            .padding(.leading, 16)
    }

    // MARK: - Действия

    private func syncFromMe() {
        guard let me = session.me else { return }
        lang = me.lang
        notificationOn = me.notificationOn
    }

    private func saveName() {
        let trimmed = nameDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            errorMessage = "Имя не может быть пустым"
            return
        }
        guard trimmed != session.me?.displayName else { return }
        Task { await save(displayName: trimmed) }
    }

    /// PATCH /me: обновляет профиль на сервере и в сессии;
    /// при ошибке показывает alert и откатывает локальные настройки.
    private func save(
        displayName: String? = nil,
        lang: String? = nil,
        notificationOn: Bool? = nil
    ) async {
        do {
            session.me = try await session.api.updateMe(
                displayName: displayName,
                lang: lang,
                notificationOn: notificationOn
            )
            Haptics.success()
        } catch {
            errorMessage = error.localizedDescription
            syncFromMe()
        }
    }
}

#Preview {
    AccountView()
        .environment(SessionStore())
}
