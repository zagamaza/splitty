import AuthenticationServices
import SwiftUI
import UIKit

// MARK: - Одноразовый код входа

/// Нормализация и валидация одноразового кода входа из Telegram-бота.
/// Чистая строковая логика — покрыта юнит-тестами (LoginCodeTests).
enum LoginCode {
    /// Минимальная длина кода: бот генерирует ровно 8 символов
    /// (internal/bot loginCodeLen) — кнопка активна от 8.
    static let minLength = 8

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

// MARK: - Вход по email и паролю

/// Проверки формы email + пароль. Чистая логика — покрыта юнит-тестами
/// (EmailLoginFormTests); сервер проверяет то же самое ещё раз.
enum EmailLoginForm {
    /// Минимум сервера (`minPasswordLen` в password_auth.go).
    static let minPasswordLength = 8

    static func normalizeEmail(_ raw: String) -> String {
        raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }

    /// Грубая проверка формы адреса: точный разбор — за сервером
    /// (`mail.ParseAddress`), здесь только чтобы не слать заведомый мусор.
    static func isValidEmail(_ raw: String) -> Bool {
        let email = normalizeEmail(raw)
        let parts = email.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty else { return false }
        let domain = parts[1]
        return domain.count >= 3 && domain.contains(".") && !domain.hasPrefix(".") && !domain.hasSuffix(".")
    }

    static func isValidPassword(_ password: String) -> Bool {
        password.count >= minPasswordLength && password.utf8.count <= 72
    }

    static func canLogin(email: String, password: String) -> Bool {
        isValidEmail(email) && !password.isEmpty
    }

    static func canRegister(email: String, password: String, name: String) -> Bool {
        isValidEmail(email)
            && isValidPassword(password)
            && !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

// MARK: - Экран входа

/// Экран входа: Apple, Google, Telegram, форма email + пароль, свёрнутый вход
/// по коду из бота. Ниже — только в DEBUG — dev-вход (на симуляторе раскрыт,
/// от его полей зависят UI-тесты) и настройка адреса сервера.
struct LoginView: View {
    @Environment(SessionStore.self) private var session
    @Environment(\.colorScheme) private var colorScheme

    @State private var codeText = ""
    @State private var isLoggingIn = false
    @State private var errorMessage: String?
    /// Свёрнут по умолчанию: экран входа — это три кнопки, код нужен меньшинству
    @State private var isCodeLoginExpanded = false

    @State private var email = ""
    @State private var password = ""
    @State private var registerName = ""
    /// Та же карточка работает и на вход, и на регистрацию: отличается полем
    /// «Имя» и текстом кнопки.
    @State private var isRegistering = false

    /// Сырой nonce текущей попытки входа через Apple: в системный запрос
    /// уходит его SHA256, а на сервер — само значение. Живёт между колбэками
    /// `onRequest` и `onCompletion`, поэтому и хранится состоянием экрана.
    @State private var appleRawNonce: String?

    #if DEBUG
    @State private var telegramIdText = ""
    @State private var displayName = ""
    @State private var username = ""

    /// Начальное состояние dev-блока: на симуляторе раскрыт (UI-тесты зависят
    /// от полей «Telegram ID»/«Имя»/«Войти»), на устройстве — свёрнут.
    #if targetEnvironment(simulator)
    @State private var isDevLoginExpanded = true
    #else
    @State private var isDevLoginExpanded = false
    #endif
    #endif

    init() {}

    var body: some View {
        // Binding нужен только полю «Сервер» (DEBUG); в релизе шадоу-копия
        // была бы неиспользуемой переменной.
        #if DEBUG
        @Bindable var session = session
        #endif
        ZStack {
            Color.bg
                .ignoresSafeArea()

            ScrollView {
                VStack(spacing: 20) {
                    logo
                    appleLoginButton
                    googleLoginButton
                    telegramWebLoginButton
                    orDivider
                    emailPasswordCard
                    // Не убирать: через это поле входит ревьюер App Store
                    // (REVIEW_LOGIN_CODE в /auth/code), и это единственный вход,
                    // не зависящий от нашего домена
                    codeLoginDisclosure
                    // Dev-вход и настройка сервера — только в DEBUG-сборках:
                    // в релизе это бэкдор мимо авторизации через Telegram.
                    #if DEBUG
                    devLoginSection
                    serverDisclosure(baseURL: $session.baseURLString)
                    #endif
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
                .scaledFont(size: 46, weight: .bold, relativeTo: .title)
                .foregroundStyle(Color.accent)
            Text("Делите расходы с друзьями")
                .scaledFont(size: 17, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
        }
        .padding(.top, 72)
        .padding(.bottom, 12)
    }

    /// Sign in with Apple — НАД карточкой Telegram: Apple требует, чтобы её
    /// кнопка была не менее заметной, чем остальные способы входа.
    /// Системная кнопка, а не своя: собственная вёрстка логотипа и текста —
    /// повод для отказа на ревью.
    private var appleLoginButton: some View {
        SignInWithAppleButton(.signIn) { request in
            // Скоупы и ХЕШ nonce проставляет общий сервис: протокол Apple
            // обязан совпадать со стороной привязки (AccountView).
            appleRawNonce = AppleSignInService.prepare(request)
        } onCompletion: { result in
            handleAppleCompletion(result)
        }
        .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
        .frame(height: 52)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .disabled(isLoggingIn)
    }

    /// Вход через веб-виджет Telegram: ASWebAuthenticationSession на
    /// <baseURL>/tg-auth, ссылку на oauth.telegram.org собирает сервер.
    private var telegramWebLoginButton: some View {
        Button {
            loginWithTelegramWidget()
        } label: {
            HStack(spacing: 10) {
                Image(systemName: "paperplane.fill")
                    .font(.system(size: 17, weight: .semibold))
                Text("Войти через Telegram")
                    .scaledFont(size: 17, weight: .semibold)
            }
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity, minHeight: 52)
        }
        .background(
            Color.telegramBlue,
            in: RoundedRectangle(cornerRadius: 14, style: .continuous)
        )
        .disabled(isLoggingIn)
    }

    /// Оформление по Google Identity Branding Guidelines: официальный знак из
    /// ресурсов SDK, заданные Google цвета. Геометрия — как у кнопки Apple:
    /// по 4.8 она не должна выглядеть второстепенной.
    private var googleLoginButton: some View {
        Button {
            loginWithGoogle()
        } label: {
            HStack(spacing: 12) {
                Image("GoogleG")
                    .resizable()
                    // иначе SwiftUI перекрасит четырёхцветный знак в tint
                    .renderingMode(.original)
                    .frame(width: 20, height: 20)
                Text("Войти через Google")
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.googleLabel)
            }
            .frame(maxWidth: .infinity, minHeight: 52)
        }
        .background(
            Color.googleSurface,
            in: RoundedRectangle(cornerRadius: 14, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Color.googleBorder, lineWidth: 1)
        }
        .disabled(isLoggingIn)
    }

    private var orDivider: some View {
        HStack(spacing: 12) {
            Rectangle().fill(Color.hairline).frame(height: 1)
            Text("или")
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
            Rectangle().fill(Color.hairline).frame(height: 1)
        }
        .padding(.vertical, 4)
    }

    /// Вход и регистрация по email с паролем — для тех, у кого нет ни Apple ID,
    /// ни Google, ни Telegram. Под соцкнопками: по Guideline 4.8 кнопка Apple
    /// не должна выглядеть менее заметной.
    private var emailPasswordCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(isRegistering ? "Регистрация" : "Вход по email")
                .sectionHeaderStyle()

            if isRegistering {
                TextField("Имя", text: $registerName)
                    .textContentType(.name)
                    .modifier(LoginFieldStyle())
            }

            TextField("Email", text: $email)
                .textContentType(.emailAddress)
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .modifier(LoginFieldStyle())

            SecureField("Пароль", text: $password)
                // .newPassword подсказывает Связке ключей сгенерировать пароль
                .textContentType(isRegistering ? .newPassword : .password)
                .modifier(LoginFieldStyle())
                .submitLabel(.go)
                .onSubmit { submitEmailForm() }

            if isRegistering, !password.isEmpty, !EmailLoginForm.isValidPassword(password) {
                Text("Пароль — не короче \(EmailLoginForm.minPasswordLength) символов")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }

            Button {
                submitEmailForm()
            } label: {
                Text(isRegistering ? "Зарегистрироваться" : "Войти")
            }
            .buttonStyle(.primaryPill)
            .disabled(!isEmailFormValid || isLoggingIn)
            .padding(.top, 4)

            Button {
                isRegistering.toggle()
            } label: {
                Text(isRegistering ? "Уже есть аккаунт? Войти" : "Нет аккаунта? Зарегистрироваться")
                    .scaledFont(size: 15, weight: .medium)
                    .foregroundStyle(Color.accent)
            }
            .frame(maxWidth: .infinity)
            .disabled(isLoggingIn)
        }
        .surfaceCard(padding: 20)
    }

    private var isEmailFormValid: Bool {
        isRegistering
            ? EmailLoginForm.canRegister(email: email, password: password, name: registerName)
            : EmailLoginForm.canLogin(email: email, password: password)
    }

    /// Вход или регистрация по email — POST /auth/login или /auth/register.
    /// Сообщения об ошибках берём с сервера: он намеренно отвечает одинаково
    /// на неверный пароль и незнакомый адрес.
    private func submitEmailForm() {
        guard isEmailFormValid, !isLoggingIn else { return }
        let mail = EmailLoginForm.normalizeEmail(email)
        let name = registerName.trimmingCharacters(in: .whitespacesAndNewlines)
        let registering = isRegistering

        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                if registering {
                    try await session.register(email: mail, password: password, displayName: name)
                } else {
                    try await session.loginWithPassword(email: mail, password: password)
                }
                password = ""
            } catch {
                errorMessage = humanErrorText(error)
            }
        }
    }

    /// Основной вход: одноразовый код из Telegram-бота → POST /auth/code.
    /// Свёрнутый вход по коду: не «для разработки», а рабочий путь для всех,
    /// кто пришёл через бота.
    private var codeLoginDisclosure: some View {
        DisclosureGroup(isExpanded: $isCodeLoginExpanded) {
            telegramLoginCard
                .padding(.top, 12)
        } label: {
            Text("Вход по коду из бота")
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
        }
        .tint(Color.inkSecondary)
        .padding(.horizontal, 4)
    }

    private var telegramLoginCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Вход по коду")
                .sectionHeaderStyle()

            HStack(alignment: .top, spacing: 10) {
                Image(systemName: "paperplane.fill")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Color.accent)
                    .padding(.top, 2)
                // Ревьюеру App Store выдают готовый код, Telegram ему не нужен
                Text("Вставьте код в поле ниже. Нет кода — получите его в боте.")
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            // Прямой переход в бота: инструкция без кнопки заставляла
            // руками искать бота в Telegram.
            Button {
                openBot()
            } label: {
                Label("Открыть бота", systemImage: "paperplane")
            }
            .buttonStyle(.softChip)

            TextField("Код входа", text: $codeText)
                .textInputAutocapitalization(.characters)
                .autocorrectionDisabled()
                .scaledFont(size: 17, weight: .semibold, design: .monospaced)
                .modifier(LoginFieldStyle())
                .submitLabel(.go)
                .onSubmit { loginWithCode() }

            // Пока код короче минимума, объясняем, почему кнопка неактивна.
            if !LoginCode.isValid(codeText) {
                // не «ровно 8»: код для проверки приложения длиннее
                Text("Введите код — не короче 8 символов")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }

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

    #if DEBUG
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
    /// Лейблы «Telegram ID»/«Имя» фиксированы: их ищет DemoFlowUITests.
    /// У кнопки лейбл «Войти» НЕ уникален (такой же в карточке входа по email),
    /// поэтому тесты берут её по `devLoginSubmit` — не переименовывать.
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
            .accessibilityIdentifier("devLoginSubmit")
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
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
        }
        .tint(Color.inkSecondary)
        .padding(.horizontal, 4)
    }
    #endif

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

    /// Открывает Telegram-бота сразу с командой входа: `start=login` —
    /// бот понимает «/start login» как /login и присылает код одним тапом,
    /// без ручного ввода команды. Deeplink в приложение, fallback — t.me.
    private func openBot() {
        guard let appURL = URL(string: "tg://resolve?domain=split_money_bot&start=login"),
              let webURL = URL(string: "https://t.me/split_money_bot?start=login") else { return }
        UIApplication.shared.open(appURL) { opened in
            if !opened {
                UIApplication.shared.open(webURL)
            }
        }
    }

    /// Вход по коду из бота; 401 (invalid_code) — человеческое сообщение,
    /// остальные — через humanErrorText (без сетевого жаргона в алерте).
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
                errorMessage = humanErrorText(error)
            }
        }
    }

    /// Веб-вход через Telegram: сессия → payload виджета → POST /auth/telegram.
    /// Отмену глотаем молча, как у Apple и Google.
    private func loginWithTelegramWidget() {
        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                let payload = try await TelegramWebAuth.authenticate(
                    baseURL: session.baseURLString,
                    presenter: nil
                )
                try await session.loginWithTelegram(payload)
            } catch TelegramWebAuth.Failure.cancelled {
                // человек закрыл окно — это не ошибка
            } catch TelegramWebAuth.Failure.badResponse {
                errorMessage = "Telegram не подтвердил вход. Попробуйте ещё раз"
            } catch {
                errorMessage = humanErrorText(error)
            }
        }
    }

    /// Вход через Google: системный лист → id-токен → POST /auth/google.
    ///
    /// Отмена обрабатывается ТИХО, как и у Apple: человек сам закрыл лист,
    /// алерт на это был бы навязчивым. Проверка идёт по `isCancellation`, а не
    /// по типу ошибки — SDK возвращает `NSError` домена Google, и до нашего
    /// `GoogleSignInError.cancelled` он превращается только внутри сервиса.
    private func loginWithGoogle() {
        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                let idToken = try await GoogleSignInService.signIn()
                try await session.loginWithGoogle(idToken: idToken)
            } catch GoogleSignInError.cancelled {
                return
            } catch let error as APIError where error.isUnauthorized {
                errorMessage = "Google не подтвердил вход. Попробуйте ещё раз"
            } catch {
                errorMessage = humanErrorText(error)
            }
        }
    }

    /// Разбор ответа системного листа Apple.
    ///
    /// Отмена (`ASAuthorizationError.canceled`) — не ошибка: человек сам
    /// закрыл лист, алерт на это был бы навязчивым.
    private func handleAppleCompletion(_ result: Result<ASAuthorization, Error>) {
        let rawNonce = appleRawNonce
        appleRawNonce = nil

        do {
            let credential = try AppleSignInService.credential(from: result, rawNonce: rawNonce)
            loginWithApple(
                idToken: credential.idToken,
                displayName: credential.displayName,
                nonce: credential.rawNonce,
                authorizationCode: credential.authorizationCode
            )
        } catch AppleSignInError.cancelled {
            return
        } catch AppleSignInError.nonceUnavailable {
            errorMessage = "Не удалось начать вход через Apple. Попробуйте ещё раз"
        } catch AppleSignInError.missingCredential {
            errorMessage = "Apple не вернул данные для входа. Попробуйте ещё раз"
        } catch {
            errorMessage = humanErrorText(error)
        }
    }

    private func loginWithApple(
        idToken: String,
        displayName: String,
        nonce: String,
        authorizationCode: String?
    ) {
        isLoggingIn = true
        Task {
            defer { isLoggingIn = false }
            do {
                try await session.loginWithApple(
                    idToken: idToken,
                    displayName: displayName,
                    nonce: nonce,
                    authorizationCode: authorizationCode
                )
            } catch let error as APIError where error.isUnauthorized {
                errorMessage = "Apple не подтвердил вход. Попробуйте ещё раз"
            } catch {
                errorMessage = humanErrorText(error)
            }
        }
    }

    #if DEBUG
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
                errorMessage = humanErrorText(error)
            }
        }
    }
    #endif
}

/// Поле ввода: подложка цвета фона экрана + hairline-бордер —
/// читается и внутри surface-карточки, и на Color.bg (поле «Сервер»).
private struct LoginFieldStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .scaledFont(size: 17)
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
