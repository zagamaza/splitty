import AuthenticationServices
import SwiftUI
import UIKit

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

/// Экран входа: иконка с названием в верхней половине, стопка способов входа
/// прижата к низу. Форма email живёт в шторке за тихой ссылкой — на первом
/// экране её нет. Настройка адреса сервера (DEBUG) спрятана за неявным жестом
/// по иконке, см. `serverRevealTaps`.
struct LoginView: View {
    @Environment(SessionStore.self) private var session
    @Environment(\.colorScheme) private var colorScheme

    @State private var isLoggingIn = false
    @State private var errorMessage: String?

    @State private var email = ""
    @State private var password = ""
    @State private var registerName = ""
    /// Та же карточка работает и на вход, и на регистрацию: отличается полем
    /// «Имя» и текстом кнопки.
    @State private var isRegistering = false
    /// Форма email — в шторке: на первом экране остаются только три кнопки.
    @State private var isEmailSheetPresented = false
    /// Ошибка формы email. Отдельно от `errorMessage`: тот показывается
    /// алертом с корневого экрана, который шторка перекрыла бы собой.
    @State private var emailErrorMessage: String?

    /// Сырой nonce текущей попытки входа через Apple: в системный запрос
    /// уходит его SHA256, а на сервер — само значение. Живёт между колбэками
    /// `onRequest` и `onCompletion`, поэтому и хранится состоянием экрана.
    @State private var appleRawNonce: String?

    #if DEBUG
    /// Сколько раз подряд ткнули в логотип. Поле «Сервер» — инструмент
    /// разработки, а не настройка приложения: на экране его нет, и находит
    /// его только тот, кто знает жест.
    @State private var logoTapCount = 0
    @State private var isServerRevealed = false

    /// Тапов по логотипу до появления поля «Сервер».
    private static let serverRevealTaps = 5
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

            VStack(spacing: 0) {
                // Распорки равные, а нижний отступ самого блока поднимает его
                // на половину своей величины выше геометрического центра:
                // ровно по центру блок читается съехавшим к кнопкам.
                Spacer(minLength: 0)
                appMark
                valueProps
                    .padding(.bottom, 28)
                Spacer(minLength: 0)

                VStack(spacing: 10) {
                    appleLoginButton
                    googleLoginButton
                    telegramWebLoginButton
                }
                emailDisclosure
                    .padding(.top, 14)
                // Только в DEBUG и только после жеста: в релизе поле
                // «Сервер» — способ увести Bearer-токен на чужой адрес.
                #if DEBUG
                if isServerRevealed {
                    serverField(baseURL: $session.baseURLString)
                        .padding(.top, 16)
                }
                #endif
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 12)

            if isLoggingIn {
                loadingOverlay
            }
        }
        .sheet(isPresented: $isEmailSheetPresented) {
            emailSheet
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

    /// Иконка приложения, словомарка и тихий подзаголовок.
    /// В DEBUG он же — тайная дверь к полю «Сервер» (см. `serverRevealTaps`).
    ///
    /// Иконка — отдельный ресурс `AppMark`, копия `icon-1024`: сам AppIcon
    /// Три пункта вместо строки «Делите расходы с друзьями»: она описывала любое
    /// приложение категории и не отвечала ни на один реальный вопрос. Отвечаем
    /// на три: что я записываю, что это даёт, переводит ли приложение деньги.
    /// Статичный блок, не карусель: он обязан помещаться на маленьком экране
    /// вместе с кнопками входа и при увеличенном системном шрифте.
    private var valueProps: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(LoginValueProp.all) { prop in
                valueProp(icon: prop.icon, title: prop.title, detail: prop.detail)
            }
        }
        .padding(.top, 22)
        .padding(.horizontal, 4)
        .frame(maxWidth: 380)
    }

    private func valueProp(icon: String, title: String, detail: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(Color.accentText)
                .frame(width: 30, height: 30)
                .background(Color.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .scaledFont(size: 14.5, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Text(detail)
                    .scaledFont(size: 12.5, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
    }

    /// из кода недоступен, его забирает система.
    private var appMark: some View {
        VStack(spacing: 14) {
            Image("AppMark")
                .resizable()
                .frame(width: 84, height: 84)
                .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                .shadow(color: .black.opacity(0.14), radius: 14, y: 7)
                .accessibilityHidden(true)

            Text("Splitty")
                .scaledFont(size: 40, weight: .bold, relativeTo: .title)
                .foregroundStyle(Color.accent)
        }
        #if DEBUG
        // contentShape: тапы ловятся и по пустому месту между строками,
        // иначе попасть надо ровно в глифы.
        .contentShape(Rectangle())
        .accessibilityIdentifier("loginLogo")
        .onTapGesture {
            guard !isServerRevealed else { return }
            logoTapCount += 1
            if logoTapCount >= Self.serverRevealTaps {
                isServerRevealed = true
            }
        }
        #endif
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
            // Геометрия знака — как у Google (20×20, отступ 12): системную
            // кнопку Apple не перевёрстывать, но свои две держим одинаковыми.
            HStack(spacing: 12) {
                Image(systemName: "paperplane.fill")
                    .font(.system(size: 17, weight: .semibold))
                    .frame(width: 20, height: 20)
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

    /// Тихая ссылка у нижнего края — единственный след формы email на первом
    /// экране. Guideline 4.8 это не задевает: требование Apple про
    /// относительную заметность сторонних кнопок, а email — свой вход.
    private var emailDisclosure: some View {
        Button {
            isEmailSheetPresented = true
        } label: {
            HStack(spacing: 4) {
                Text("Или")
                    .foregroundStyle(Color.inkSecondary)
                Text("войдите по email")
                    .foregroundStyle(Color.accent)
                    .fontWeight(.semibold)
            }
            .scaledFont(size: 15)
            .frame(maxWidth: .infinity)
        }
        .accessibilityIdentifier("emailLoginDisclosure")
        .disabled(isLoggingIn)
    }

    /// Форма email в шторке. Средний детент: форма короткая, во весь экран
    /// её разворачивать незачем, а полупрозрачный верх сохраняет контекст.
    private var emailSheet: some View {
        ScrollView {
            emailPasswordForm
                .padding(.horizontal, 20)
                .padding(.top, 20)
                .padding(.bottom, 24)
        }
        .scrollBounceBehavior(.basedOnSize)
        .scrollDismissesKeyboard(.interactively)
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    /// Вход и регистрация по email с паролем — для тех, у кого нет ни Apple ID,
    /// ни Google, ни Telegram.
    private var emailPasswordForm: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(isRegistering ? "Регистрация" : "Вход по email")
                .scaledFont(size: 22, weight: .bold, relativeTo: .title3)
                .foregroundStyle(Color.ink)
                .padding(.bottom, 2)

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

            // Ошибка формы — текстом на месте, а не алертом: алерт с корневого
            // экрана шторка перекрывает, и человек не увидел бы ничего.
            if let emailErrorMessage {
                Text(emailErrorMessage)
                    .scaledFont(size: 14, relativeTo: .footnote)
                    .foregroundStyle(Color.negativeText)
                    .fixedSize(horizontal: false, vertical: true)
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
                emailErrorMessage = nil
            } label: {
                Text(isRegistering ? "Уже есть аккаунт? Войти" : "Нет аккаунта? Зарегистрироваться")
                    .scaledFont(size: 15, weight: .medium)
                    .foregroundStyle(Color.accentText)
            }
            .frame(maxWidth: .infinity)
            .disabled(isLoggingIn)
        }
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
        emailErrorMessage = nil
        Task {
            defer { isLoggingIn = false }
            do {
                if registering {
                    try await session.register(email: mail, password: password, displayName: name)
                } else {
                    try await session.loginWithPassword(email: mail, password: password)
                }
                password = ""
                isEmailSheetPresented = false
            } catch {
                emailErrorMessage = humanErrorText(error)
            }
        }
    }

    #if DEBUG
    /// Адрес сервера. Секции с заголовком нет намеренно: поле появляется
    /// только после жеста по логотипу и живёт ровно до перезапуска экрана —
    /// это отладочный тумблер, а не настройка, которую ищут глазами.
    private func serverField(baseURL: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Сервер")
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
            TextField("http://127.0.0.1:7171", text: baseURL)
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .accessibilityIdentifier("serverField")
                .modifier(LoginFieldStyle())
        }
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
                errorMessage = String(localized: "Telegram не подтвердил вход. Попробуйте ещё раз")
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
                errorMessage = String(localized: "Google не подтвердил вход. Попробуйте ещё раз")
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
            errorMessage = String(localized: "Не удалось начать вход через Apple. Попробуйте ещё раз")
        } catch AppleSignInError.missingCredential {
            errorMessage = String(localized: "Apple не вернул данные для входа. Попробуйте ещё раз")
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
                errorMessage = String(localized: "Apple не подтвердил вход. Попробуйте ещё раз")
            } catch {
                errorMessage = humanErrorText(error)
            }
        }
    }

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

/// Пункты на экране входа. Вынесены из вью: их состав — продуктовое решение,
/// и оно должно ломаться тестом, а не молча исчезать при следующей правке.
struct LoginValueProp: Identifiable {
    let id: String
    let icon: String
    let title: String
    let detail: String

    static let all: [LoginValueProp] = [
        .init(
            id: "split",
            icon: "list.bullet",
            title: String(localized: "Общий счёт на всех"),
            detail: String(localized: "Поездка, квартира, ужин — кто за что заплатил")
        ),
        .init(
            id: "once",
            icon: "arrow.triangle.merge",
            title: String(localized: "Платите один раз"),
            detail: String(localized: "Долги сводятся: один перевод вместо нескольких")
        ),
        .init(
            id: "money",
            icon: "arrow.right",
            title: String(localized: "Деньги передаёте сами"),
            detail: String(localized: "Splitty только ведёт учёт")
        ),
    ]
}
