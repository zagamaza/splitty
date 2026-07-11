import SwiftUI

// MARK: - Одноразовый код входа

/// Нормализация и валидация одноразового кода входа из Telegram-бота.
/// Чистая строковая логика — покрыта юнит-тестами (LoginCodeTests).
enum LoginCode {
    /// Минимальная длина кода: кнопка «Войти по коду» активна от 6 символов.
    static let minLength = 6

    /// Убирает все пробельные символы (хвосты и разрывы при вставке из чата)
    /// и приводит к верхнему регистру — канонический формат кода бота (ABCD2345).
    static func normalize(_ raw: String) -> String {
        String(raw.filter { !$0.isWhitespace }).uppercased()
    }

    /// true, когда нормализованный код достаточно длинный, чтобы отправлять.
    static func isValid(_ raw: String) -> Bool {
        normalize(raw).count >= minLength
    }
}

// MARK: - Экран входа

/// Экран входа: премиум-велком на нейтральном фоне — словомарка «Splitty»,
/// основная карточка «Вход через Telegram» (одноразовый код из бота,
/// POST /auth/code) и dev-вход через POST /auth/dev (на симуляторе раскрыт —
/// от его полей зависят UI-тесты, на устройстве свёрнут в DisclosureGroup);
/// настройка сервера — тихий DisclosureGroup внизу.
struct LoginView: View {
    @Environment(SessionStore.self) private var session

    @State private var codeText = ""
    @State private var telegramIdText = ""
    @State private var displayName = ""
    @State private var username = ""
    @State private var isLoggingIn = false
    @State private var errorMessage: String?

    /// Начальное состояние dev-блока: на симуляторе раскрыт (UI-тесты зависят
    /// от полей «Telegram ID»/«Имя»/«Войти»), на устройстве — свёрнут.
    #if targetEnvironment(simulator)
    @State private var isDevLoginExpanded = true
    #else
    @State private var isDevLoginExpanded = false
    #endif

    init() {}

    var body: some View {
        @Bindable var session = session
        ZStack {
            Color.bg
                .ignoresSafeArea()

            ScrollView {
                VStack(spacing: 20) {
                    logo
                    telegramLoginCard
                    devLoginSection
                    serverDisclosure(baseURL: $session.baseURLString)
                }
                .padding(.horizontal, 20)
                .padding(.bottom, 32)
            }
            .scrollDismissesKeyboard(.interactively)

            if isLoggingIn {
                loadingOverlay
            }
        }
        .tint(Color.accent)
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

    // MARK: - Блоки экрана

    /// Крупная словомарка: изумрудный rounded-логотип и тихий подзаголовок.
    private var logo: some View {
        VStack(spacing: 10) {
            Text("Splitty")
                .font(.system(size: 46, weight: .bold, design: .rounded))
                .foregroundStyle(Color.accent)
            Text("Делите расходы с друзьями")
                .font(.system(size: 17, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
        }
        .padding(.top, 72)
        .padding(.bottom, 12)
    }

    /// Основной вход: одноразовый код из Telegram-бота → POST /auth/code.
    private var telegramLoginCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Вход через Telegram")
                .sectionHeaderStyle()

            HStack(alignment: .top, spacing: 10) {
                Image(systemName: "paperplane.fill")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Color.accent)
                    .padding(.top, 2)
                Text("Откройте **@split\\_money\\_bot** и отправьте команду /login — бот пришлёт код")
                    .font(.system(size: 15, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            TextField("Код из Telegram", text: $codeText)
                .textInputAutocapitalization(.characters)
                .autocorrectionDisabled()
                .font(.system(size: 17, weight: .semibold, design: .monospaced))
                .modifier(LoginFieldStyle())
                .submitLabel(.go)
                .onSubmit { loginWithCode() }

            Button {
                loginWithCode()
            } label: {
                Text("Войти по коду")
            }
            .buttonStyle(.primaryPill)
            .disabled(!LoginCode.isValid(codeText) || isLoggingIn)
            .padding(.top, 4)
        }
        .surfaceCard(padding: 20)
    }

    /// Dev-вход: на симуляторе — всегда раскрытая карточка (как раньше),
    /// на устройстве — свёрнутый DisclosureGroup «Вход для разработки».
    @ViewBuilder
    private var devLoginSection: some View {
        #if targetEnvironment(simulator)
        VStack(alignment: .leading, spacing: 12) {
            Text("Вход для разработки")
                .sectionHeaderStyle()
            devLoginFields
        }
        .surfaceCard(padding: 20)
        #else
        DisclosureGroup(isExpanded: $isDevLoginExpanded) {
            devLoginFields
                .padding(.top, 12)
        } label: {
            Text("Вход для разработки")
                .sectionHeaderStyle()
        }
        .tint(Color.inkSecondary)
        .surfaceCard(padding: 20)
        #endif
    }

    /// Поля dev-входа и CTA — общие для симулятора и устройства.
    /// Лейблы «Telegram ID»/«Имя»/«Войти» фиксированы: их ждёт DemoFlowUITests.
    private var devLoginFields: some View {
        VStack(alignment: .leading, spacing: 12) {
            TextField("Telegram ID", text: $telegramIdText)
                .keyboardType(.numberPad)
                .modifier(LoginFieldStyle())

            TextField("Имя", text: $displayName)
                .modifier(LoginFieldStyle())

            TextField("Username (необязательно)", text: $username)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .modifier(LoginFieldStyle())

            Button {
                loginDev()
            } label: {
                Text("Войти")
            }
            .buttonStyle(.primaryPill)
            .disabled(!isDevFormValid || isLoggingIn)
            .padding(.top, 4)
        }
    }

    /// Свёрнутая настройка адреса сервера (advanced) — тихая, внизу экрана.
    private func serverDisclosure(baseURL: Binding<String>) -> some View {
        DisclosureGroup {
            TextField("http://127.0.0.1:7171", text: baseURL)
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .modifier(LoginFieldStyle())
                .padding(.top, 8)
        } label: {
            Text("Сервер")
                .font(.system(size: 15, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
        }
        .tint(Color.inkSecondary)
        .padding(.horizontal, 4)
    }

    private var loadingOverlay: some View {
        ZStack {
            Color.bg.opacity(0.6)
                .ignoresSafeArea()
            ProgressView()
                .controlSize(.large)
                .tint(Color.accent)
        }
    }

    // MARK: - Действия

    /// Вход по коду из бота; 401 (invalid_code) — человеческое сообщение,
    /// остальные ошибки — как есть (localizedDescription по-русски).
    private func loginWithCode() {
        let code = LoginCode.normalize(codeText)
        guard code.count >= LoginCode.minLength else { return }

        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                try await session.loginWithCode(code)
            } catch let error as APIError where error.isUnauthorized {
                errorMessage = "Неверный или просроченный код"
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    private var isDevFormValid: Bool {
        (Int(telegramIdText.trimmingCharacters(in: .whitespaces)) ?? 0) > 0
            && !displayName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func loginDev() {
        guard let userId = Int(telegramIdText.trimmingCharacters(in: .whitespaces)), userId > 0 else {
            errorMessage = "Введите числовой Telegram ID"
            return
        }
        let name = displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else {
            errorMessage = "Введите имя"
            return
        }
        let uname = username.trimmingCharacters(in: .whitespacesAndNewlines)

        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                try await session.loginDev(
                    userId: userId,
                    displayName: name,
                    username: uname.isEmpty ? nil : uname
                )
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }
}

/// Поле ввода: подложка цвета фона экрана + hairline-бордер —
/// читается и внутри surface-карточки, и на Color.bg (поле «Сервер»).
private struct LoginFieldStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .font(.system(size: 17, design: .rounded))
            .foregroundStyle(Color.ink)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .background(
                Color.bg,
                in: RoundedRectangle(cornerRadius: 12, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(Color.hairline, lineWidth: 1)
            }
    }
}

#Preview {
    LoginView()
        .environment(SessionStore())
}
