import AuthenticationServices
import SwiftUI

/// Вкладка «Профиль»: профиль-шапка с большим аватаром, секции настроек
/// карточками, способы входа, сервер, выход и удаление аккаунта.
struct AccountView: View {
    @AppStorage(AppTheme.storageKey) private var themeRaw = AppTheme.system.rawValue
    @Environment(SessionStore.self) private var session
    @Environment(\.colorScheme) private var colorScheme

    @State private var nameDraft = ""
    @State private var isEditNamePresented = false
    @State private var isLogoutConfirmPresented = false
    @State private var errorMessage: String?

    /// Сообщение-предупреждение сервера (отвязка Telegram): не ошибка, но
    /// показать обязаны — там про то, что бот заведёт отдельный профиль.
    @State private var noticeMessage: String?

    /// Способ входа, для которого запрошено подтверждение отвязки.
    @State private var providerToUnlink: LoginProvider?

    /// true — привязка/отвязка в полёте: кнопки секции блокируются, чтобы
    /// два запроса не гонялись за один и тот же список способов входа.
    @State private var isIdentityBusy = false

    /// Сырой nonce текущей попытки привязки Apple: в системный запрос уходит
    /// его SHA256, а на сервер — само значение (протокол см. `AppleNonce`).
    @State private var appleRawNonce: String?

    @State private var isDeleteConfirmPresented = false
    /// true — DELETE /me в полёте: кнопка удаления заблокирована.
    @State private var isDeleting = false

    // Локальная копия настройки: правки применяются через PATCH /me,
    // при ошибке откатываются к значениям из профиля.
    @State private var lang = "ru"

    init() {}

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    headerSection
                    settingsSection
                    loginMethodsSection
                    // Адрес сервера — отладочная информация, пользователю в
                    // релизе не нужна (менять его всё равно можно только в DEBUG).
                    #if DEBUG
                    serverSection
                    #endif
                    logoutSection
                    deleteAccountSection
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
                // Отвязать FCM-токен ПОКА JWT валиден, затем выйти. Вместе с
                // сессией чистятся офлайн-кеш и outbox (неотправленные операции
                // пропадут — о них предупреждает title).
                Task {
                    await PushManager.shared.unregisterCurrentToken()
                    session.logout()
                }
            }
            Button("Отмена", role: .cancel) {}
        }
        .confirmationDialog(
            "Отвязать способ входа?",
            isPresented: Binding(
                get: { providerToUnlink != nil },
                set: { if !$0 { providerToUnlink = nil } }
            ),
            titleVisibility: .visible,
            presenting: providerToUnlink
        ) { provider in
            Button("Отвязать \(provider.title)", role: .destructive) {
                unlink(provider)
            }
            Button("Отмена", role: .cancel) {}
        } message: { provider in
            Text(unlinkConfirmMessage(provider))
        }
        .confirmationDialog(
            "Удалить аккаунт?",
            isPresented: $isDeleteConfirmPresented,
            titleVisibility: .visible
        ) {
            Button("Удалить аккаунт", role: .destructive) {
                deleteAccount()
            }
            Button("Отмена", role: .cancel) {}
        } message: {
            Text(Self.deleteConfirmMessage)
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
        .alert(
            "Внимание",
            isPresented: Binding(
                get: { noticeMessage != nil },
                set: { if !$0 { noticeMessage = nil } }
            )
        ) {
            Button("Понятно", role: .cancel) {}
        } message: {
            Text(noticeMessage ?? "")
        }
    }

    /// Заголовок подтверждения выхода: при непустом outbox предупреждает,
    /// что неотправленные операции будут удалены вместе с кешем.
    private var logoutConfirmTitle: String {
        let pending = session.outbox.entries.count
        guard pending > 0 else { return "Выйти из аккаунта?" }
        let word = pluralRu(pending, "неотправленная операция", "неотправленные операции", "неотправленных операций")
        return "Есть \(pending) \(word) — выйти и удалить?"
    }

    // MARK: - Секции

    /// Профиль-шапка: большой градиентный аватар, имя, @username, id.
    /// Пока профиль не загружен — placeholder той же геометрии,
    /// чтобы шапка не исчезала и контент не прыгал.
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
                        .scaledFont(size: 24, weight: .semibold)
                        .foregroundStyle(Color.ink)
                    if let username = me.username, !username.isEmpty {
                        Text("@\(username)")
                            .scaledFont(size: 15)
                            .foregroundStyle(Color.inkSecondary)
                    }
                    Text("ID: \(String(me.id))")
                        .scaledFont(size: 12, relativeTo: .footnote)
                        .monospacedDigit()
                        .foregroundStyle(Color.inkSecondary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 12)
        } else {
            VStack(spacing: 14) {
                Circle()
                    .fill(Color.hairline)
                    .frame(width: 88, height: 88)
                Text("Имя Фамилия")
                    .scaledFont(size: 24, weight: .semibold)
                    .foregroundStyle(Color.ink)
                    .redacted(reason: .placeholder)
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
                    .scaledFont(size: 16)
                    .foregroundStyle(Color.ink)
                Spacer(minLength: 8)
                Text(session.me?.displayName ?? "")
                    .scaledFont(size: 16)
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

    /// Строка «Язык»: menu-picker справа (ru/en). Настройка уходит в PATCH /me
    /// и влияет на язык сообщений бота — caption объясняет, зачем она здесь.
    private var langRow: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Text("Язык")
                    .scaledFont(size: 16)
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
            .padding(.vertical, 7)
            Text("Язык сообщений бота в Telegram")
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
                .padding(.bottom, 10)
        }
        .padding(.horizontal, 16)
    }

    /// Строка «Тема»: menu-picker (системная/светлая/тёмная), UserDefaults.
    private var themeRow: some View {
        HStack(spacing: 12) {
            Text("Тема")
                .scaledFont(size: 16)
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
    /// Мастер-тумблер живёт на самом экране уведомлений — здесь он дублировал
    /// название строки-ссылки и путал.
    private var notificationsLink: some View {
        NavigationLink {
            NotificationSettingsView()
        } label: {
            HStack(spacing: 12) {
                Text("Уведомления")
                    .scaledFont(size: 16)
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

    // MARK: - Способы входа

    /// Карточка «Способы входа»: по строке на провайдера — привязать/отвязать.
    /// Источник истины — `me.linkedProviders` с сервера: локально список не
    /// досочиняется, каждая мутация приходит ответом на запрос.
    private var loginMethodsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Способы входа")
                .sectionHeaderStyle()
                .padding(.horizontal, 4)
            VStack(spacing: 0) {
                ForEach(Array(visibleProviders.enumerated()), id: \.element) { index, provider in
                    if index > 0 {
                        rowDivider
                    }
                    providerRow(provider)
                }
            }
            .surfaceCard(padding: 0)
            Text(loginMethodsFooter)
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// Какие строки показывать.
    ///
    /// Google и Apple — всегда: их можно и привязать, и отвязать прямо здесь.
    /// Telegram — ТОЛЬКО когда он уже привязан: привязка требует Telegram Login
    /// Widget (подписанные ботом id/auth_date/hash), которого в приложении нет,
    /// и рисовать неработающую кнопку «Привязать» значит обещать несуществующее.
    private var visibleProviders: [LoginProvider] {
        LoginProvider.allCases.filter { provider in
            provider != .telegram || session.me?.isLinked(.telegram) == true
        }
    }

    /// Подпись под карточкой. Когда способ входа остался один, объясняем,
    /// почему его кнопка «Отвязать» неактивна — иначе она выглядит поломкой.
    private var loginMethodsFooter: String {
        let linked = session.me?.linkedProviders.count ?? 0
        if linked <= 1 {
            return "Последний способ входа отвязать нельзя: без него в аккаунт будет не войти. "
                + "Сначала привяжите другой."
        }
        return "Любым из привязанных способов можно войти в этот же аккаунт."
    }

    /// Строка способа входа: название, статус и действие справа.
    @ViewBuilder
    private func providerRow(_ provider: LoginProvider) -> some View {
        let isLinked = session.me?.isLinked(provider) == true
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Label(provider.title, systemImage: provider.symbol)
                    .scaledFont(size: 16)
                    .foregroundStyle(Color.ink)
                Text(isLinked ? "Привязан" : "Не привязан")
                    .scaledFont(size: 12, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            if isLinked {
                Button("Отвязать") {
                    providerToUnlink = provider
                }
                .buttonStyle(.softChip)
                // Кнопка гаснет ДО запроса: сервер ответил бы 409 last_identity,
                // но узнавать о запрете из алерта после действия — плохо.
                .disabled(session.me?.canUnlink(provider) != true || isIdentityBusy)
            } else if provider == .apple {
                appleLinkButton
            } else {
                Button("Привязать") {
                    linkGoogle()
                }
                .buttonStyle(.softChip)
                .disabled(isIdentityBusy)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    /// Привязка Apple ID — системная кнопка, а не своя вёрстка: собственный
    /// логотип Apple в этой роли — прямой повод для отказа на ревью.
    /// Компактная (по правому краю строки), поэтому `.continue`, а не `.signIn`.
    private var appleLinkButton: some View {
        SignInWithAppleButton(.continue) { request in
            // Скоупы и ХЕШ nonce проставляет общий сервис: протокол Apple
            // обязан совпадать со стороной входа (LoginView).
            appleRawNonce = AppleSignInService.prepare(request)
        } onCompletion: { result in
            handleAppleLinkCompletion(result)
        }
        .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
        .frame(width: 170, height: 38)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
        .disabled(isIdentityBusy)
    }

    #if DEBUG
    /// Карточка «Сервер»: текущий base URL и версия приложения (read-only, мелко).
    private var serverSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Сервер")
                .sectionHeaderStyle()
                .padding(.horizontal, 4)
            VStack(alignment: .leading, spacing: 6) {
                Text(session.baseURLString)
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
                Text("Версия \(Self.appVersion)")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .surfaceCard()
        }
    }
    #endif

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
                .scaledFont(size: 16, weight: .semibold)
                .foregroundStyle(Color.negative)
                .frame(maxWidth: .infinity)
                .surfaceCard()
        }
        .buttonStyle(.plain)
    }

    /// Удаление аккаунта: последняя карточка экрана, деструктивный текст.
    ///
    /// Требование Apple Guideline 5.1.1(v): удаление обязано быть доступно
    /// внутри приложения — вкладка «Профиль» → прокрутка вниз → подтверждение,
    /// без переписки с поддержкой и без похода на сайт.
    private var deleteAccountSection: some View {
        VStack(spacing: 8) {
            Button(role: .destructive) {
                isDeleteConfirmPresented = true
            } label: {
                HStack(spacing: 8) {
                    if isDeleting {
                        ProgressView()
                            .controlSize(.small)
                            .tint(Color.negative)
                    }
                    Text("Удалить аккаунт")
                        .scaledFont(size: 16, weight: .semibold)
                        .foregroundStyle(Color.negative)
                }
                .frame(maxWidth: .infinity)
                .surfaceCard()
            }
            .buttonStyle(.plain)
            .disabled(isDeleting)
            Text("Профиль удаляется безвозвратно, расходы и долги в группах остаются.")
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
        }
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

    // MARK: - Действия: способы входа

    /// Текст подтверждения отвязки. Для Telegram он честно предупреждает
    /// о том, что произойдёт дальше: вернуть привязку будет нельзя, а бот
    /// заведёт отдельный профиль без групп (см. `telegramUnlinkWarning`
    /// на сервере — тот же смысл, но там его читают уже ПОСЛЕ действия).
    private func unlinkConfirmMessage(_ provider: LoginProvider) -> String {
        switch provider {
        case .telegram:
            return "Войти через Telegram больше не получится, а бот при следующем сообщении "
                + "заведёт отдельный профиль без ваших групп. Привязать этот Telegram обратно нельзя."
        case .google, .apple:
            return "Войти через \(provider.title) больше не получится. "
                + "Остальные способы входа продолжат работать."
        }
    }

    /// Привязка Google: системный лист → id-токен → POST /me/link/google.
    /// Отмена обрабатывается тихо, как и на экране входа.
    private func linkGoogle() {
        isIdentityBusy = true
        Task {
            defer { isIdentityBusy = false }
            do {
                let idToken = try await GoogleSignInService.signIn()
                try await session.linkGoogle(idToken: idToken)
                Haptics.success()
            } catch GoogleSignInError.cancelled {
                return
            } catch {
                errorMessage = identityErrorText(error)
            }
        }
    }

    /// Разбор ответа системного листа Apple при ПРИВЯЗКЕ (не входе).
    /// Отмена — не ошибка, алерта не показываем.
    private func handleAppleLinkCompletion(_ result: Result<ASAuthorization, Error>) {
        let rawNonce = appleRawNonce
        appleRawNonce = nil

        let credential: AppleSignInService.Credential
        do {
            credential = try AppleSignInService.credential(from: result, rawNonce: rawNonce)
        } catch AppleSignInError.cancelled {
            return
        } catch AppleSignInError.nonceUnavailable {
            errorMessage = "Не удалось начать привязку Apple. Попробуйте ещё раз"
            return
        } catch AppleSignInError.missingCredential {
            errorMessage = "Apple не вернул данные для привязки. Попробуйте ещё раз"
            return
        } catch {
            errorMessage = humanErrorText(error)
            return
        }

        // authorizationCode одноразовый и живёт минуты — сервер меняет его
        // на Apple refresh token, которым при удалении аккаунта зовётся
        // auth/revoke (Guideline 5.1.1(v)). Без него у человека, вошедшего
        // через Telegram/Google и привязавшего Apple здесь, отозвать доступ
        // будет нечем: «добрать» код позже Apple не даёт.
        isIdentityBusy = true
        Task {
            defer { isIdentityBusy = false }
            do {
                try await session.linkApple(
                    idToken: credential.idToken,
                    nonce: credential.rawNonce,
                    authorizationCode: credential.authorizationCode
                )
                Haptics.success()
            } catch {
                errorMessage = identityErrorText(error)
            }
        }
    }

    /// Отвязка способа входа. Предупреждение сервера (Telegram) показываем
    /// отдельным алертом — это не ошибка, но и не «просто получилось».
    private func unlink(_ provider: LoginProvider) {
        isIdentityBusy = true
        Task {
            defer { isIdentityBusy = false }
            do {
                let warning = try await session.unlink(provider)
                if let warning, !warning.isEmpty {
                    noticeMessage = warning
                } else {
                    Haptics.success()
                }
            } catch {
                errorMessage = identityErrorText(error)
            }
        }
    }

    // MARK: - Действия: удаление аккаунта

    /// Текст подтверждения. Про сохранение расходов и долгов сказано прямо:
    /// снимки участника остаются во всех группах (имя заменяется на
    /// «Удалённый пользователь»), и обещать «удалим всё» было бы ложью.
    static let deleteConfirmMessage =
        "Профиль, имя и способы входа будут удалены безвозвратно — восстановить аккаунт нельзя.\n\n"
        + "Расходы и долги в группах останутся: участники увидят «Удалённый пользователь» "
        + "вместо вашего имени, а суммы и расчёты не изменятся."

    /// DELETE /me → полный logout (Keychain, офлайн-кеш, outbox, отложенное
    /// вступление по ссылке) и возврат на экран входа делает `RootView`
    /// по `isAuthenticated`. Так — только при 204: любая ошибка оставляет
    /// сессию нетронутой, чтобы удаление можно было повторить (и чтобы
    /// транзиентный сбой не уносил офлайн-очередь), см. `deleteAccount`.
    private func deleteAccount() {
        isDeleting = true
        Task {
            defer { isDeleting = false }
            // Отвязать FCM-токен ПОКА JWT валиден: после tombstone сервер
            // отвергнет запрос, а токен устройства остался бы висеть.
            await PushManager.shared.unregisterCurrentToken()
            do {
                try await session.deleteAccount()
            } catch {
                // Аккаунт остался жив, а токен устройства мы уже отвязали.
                // Регистрируем обратно: иначе человек сидит в живой сессии
                // вообще без пушей, и вернуть их могло бы только следующее
                // переключение входа или холодный старт. Исключение —
                // `purge_incomplete`: там аккаунт УЖЕ удалён, регистрировать
                // на tombstone нечего (сервер ответит 401), а сессия оставлена
                // только чтобы доделать чистку повторным запросом.
                if (error as? APIError)?.isPurgeIncomplete != true {
                    PushManager.shared.registerCurrentToken()
                }
                errorMessage = deleteAccountErrorText(error)
            }
        }
    }

    /// PATCH /me: обновляет профиль на сервере и в сессии;
    /// при ошибке показывает alert и откатывает локальные настройки.
    private func save(
        displayName: String? = nil,
        lang: String? = nil
    ) async {
        do {
            session.me = try await session.api.updateMe(
                displayName: displayName,
                lang: lang,
                notificationOn: nil
            )
            Haptics.success()
        } catch {
            errorMessage = humanErrorText(error)
            syncFromMe()
        }
    }
}

// MARK: - Тексты ошибок способов входа

/// Человеческий текст ошибки привязки/отвязки способа входа.
///
/// Коды сервера (`identity_taken`, `last_identity`) пользователю не показываем:
/// им нужно объяснение и следующий шаг, а не идентификатор ошибки. Собственный
/// текст, а не серверный `message`, потому что решение «что делать дальше»
/// зависит от экрана — здесь это «войдите через тот профиль» и «сначала
/// привяжите другой способ».
func identityErrorText(_ error: Error) -> String {
    guard let apiError = error as? APIError,
          case .server(let status, let code, _) = apiError
    else {
        return humanErrorText(error)
    }
    switch code {
    case "identity_taken":
        return "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него"
    case "identity_already_linked":
        // У аккаунта уже есть ДРУГАЯ личность этого провайдера. Сервер не
        // подменяет её молча: подмена отцепила бы прежний Apple ID без
        // auth/revoke, и Splitty остался бы в его списке «Вход через Apple».
        return "К аккаунту уже привязан другой аккаунт этого способа входа. Сначала отвяжите текущий"
    case "last_identity":
        return "Нельзя отвязать единственный способ входа. Сначала привяжите другой"
    case "provider_rejected":
        // Отказ ПРОВАЙДЕРА (подпись, nonce, срок id-токена) сервер отдаёт
        // 400 с этим кодом — именно чтобы его нельзя было спутать с мёртвой
        // сессией Splitty и чтобы клиент не выкидывал человека на экран входа
        // из-за одной неудачной привязки.
        return "Не удалось подтвердить аккаунт. Попробуйте ещё раз"
    default:
        break
    }
    if status == 401 {
        // С контрактом «отказ провайдера = 400 provider_rejected» 401 отсюда
        // означает ровно одно: сессия Splitty мертва. Сброс уже сделал
        // APIClient (`onUnauthorized`), нам остаётся объяснить, что произошло.
        return "Сессия истекла. Войдите ещё раз"
    }
    return humanErrorText(error)
}

// MARK: - Текст ошибки удаления аккаунта

/// Человеческий текст неудавшегося удаления аккаунта.
///
/// Сессия при ЛЮБОЙ ошибке остаётся живой (см. `SessionStore.deleteAccount`),
/// поэтому обещать «данные с устройства удалены» нельзя ни в одной ветке —
/// это была бы прямая ложь человеку, который сидит в приложении со своей
/// очередью неотправленных расходов. Отличаются только следующие шаги.
///
/// `purge_incomplete` — аккаунт УЖЕ удалён, но чистка данных не доделана.
/// Доводится она повторным нажатием той же кнопки: сессия для этого нарочно
/// сохранена, и это единственная возможность — войти заново в удалённый
/// аккаунт нельзя. Так и говорим: повторите.
///
/// 403 — демонстрационный аккаунт ревьюеров: аккаунт жив, удалять его нельзя
/// вовсе, и говорить надо именно это.
///
/// Всё остальное — сбой ДО tombstone (сервер ответил `internal`) или запрос,
/// не дошедший вовсе: аккаунт цел и нетронут, достаточно попробовать снова.
func deleteAccountErrorText(_ error: Error) -> String {
    guard let apiError = error as? APIError else { return humanErrorText(error) }
    if apiError.isPurgeIncomplete {
        return "Аккаунт удалён, но очистка данных не завершена. "
            + "Нажмите «Удалить аккаунт» ещё раз, чтобы её доделать"
    }
    if apiError.isForbidden {
        return apiError.errorDescription ?? "Этот аккаунт удалить нельзя"
    }
    if !apiError.isServerResponse {
        return humanErrorText(apiError)
    }
    return "Не удалось удалить аккаунт. Аккаунт и данные на месте — попробуйте ещё раз"
}

#Preview {
    AccountView()
        .environment(SessionStore())
}
