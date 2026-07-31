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

// MARK: - Экран входа

/// Экран входа: премиум-велком на нейтральном фоне — словомарка «Splitty»,
/// кнопка Sign in with Apple, карточка «Вход через Telegram» (одноразовый код
/// из бота, POST /auth/code) и dev-вход через POST /auth/dev (на симуляторе
/// раскрыт — от его полей зависят UI-тесты, на устройстве свёрнут в
/// DisclosureGroup); настройка сервера — тихий DisclosureGroup внизу.
struct LoginView: View {
    @Environment(SessionStore.self) private var session
    @Environment(\.colorScheme) private var colorScheme

    @State private var codeText = ""
    @State private var isLoggingIn = false
    @State private var errorMessage: String?

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
                    telegramLoginCard
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
            let rawNonce = AppleNonce.random()
            appleRawNonce = rawNonce
            request.requestedScopes = [.fullName, .email]
            // В системный запрос уходит ХЕШ — именно он попадёт в подписанный
            // Apple токен. Сырое значение остаётся здесь и уедет на сервер
            // телом запроса, чтобы серверу было что с чем сверять.
            request.nonce = AppleNonce.sha256Hex(rawNonce)
        } onCompletion: { result in
            handleAppleCompletion(result)
        }
        .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
        .frame(height: 52)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .disabled(isLoggingIn)
    }

    /// Вход через Google — СТРОГО под кнопкой Apple: Apple требует, чтобы её
    /// способ входа был не менее заметен, чем сторонние.
    ///
    /// Оформление нейтральное (подложка + hairline, геометрия один в один с
    /// кнопкой Apple), а не `.primaryPill`: акцентная заливка сделала бы
    /// Google заметнее Apple, то есть ровно то, что запрещено. Логотип G не
    /// рисуем — своя вёрстка чужого логотипа хуже, чем её отсутствие.
    private var googleLoginButton: some View {
        Button {
            loginWithGoogle()
        } label: {
            Text("Войти через Google")
                .scaledFont(size: 17, weight: .semibold)
                .foregroundStyle(Color.ink)
                .frame(maxWidth: .infinity, minHeight: 52)
        }
        .background(
            Color.surface,
            in: RoundedRectangle(cornerRadius: 14, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Color.hairline, lineWidth: 1)
        }
        .disabled(isLoggingIn)
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
                Text("Нажмите «Открыть бота» — он сразу пришлёт код для входа")
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

            TextField("Код из Telegram", text: $codeText)
                .textInputAutocapitalization(.characters)
                .autocorrectionDisabled()
                .scaledFont(size: 17, weight: .semibold, design: .monospaced)
                .modifier(LoginFieldStyle())
                .submitLabel(.go)
                .onSubmit { loginWithCode() }

            // Пока код короче минимума, объясняем, почему кнопка неактивна.
            if !LoginCode.isValid(codeText) {
                Text("Код из бота — 8 символов")
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

        switch result {
        case .success(let authorization):
            guard let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
                  let identityToken = credential.identityToken,
                  let idToken = String(data: identityToken, encoding: .utf8),
                  let rawNonce
            else {
                errorMessage = "Apple не вернул данные для входа. Попробуйте ещё раз"
                return
            }
            // authorizationCode одноразовый и живёт минуты — сервер меняет его
            // на refresh token для отзыва доступа при удалении аккаунта
            // (Apple Guideline 5.1.1(v)). Забрать его позже нельзя, поэтому
            // отправляем сразу вместе с токеном.
            let authorizationCode = credential.authorizationCode
                .flatMap { String(data: $0, encoding: .utf8) } ?? ""
            loginWithApple(
                idToken: idToken,
                displayName: appleDisplayName(credential.fullName),
                nonce: rawNonce,
                authorizationCode: authorizationCode
            )
        case .failure(let error):
            if let authError = error as? ASAuthorizationError, authError.code == .canceled {
                return
            }
            errorMessage = humanErrorText(error)
        }
    }

    /// Имя из `ASAuthorizationAppleIDCredential.fullName`. Apple отдаёт его
    /// ТОЛЬКО при первом входе — дальше приходит nil, и пустая строка тут
    /// нормальна: сервер в этом случае не трогает уже сохранённое имя.
    private func appleDisplayName(_ components: PersonNameComponents?) -> String {
        guard let components else { return "" }
        return PersonNameComponentsFormatter
            .localizedString(from: components, style: .default)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func loginWithApple(
        idToken: String,
        displayName: String,
        nonce: String,
        authorizationCode: String
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
