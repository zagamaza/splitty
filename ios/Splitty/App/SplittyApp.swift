import GoogleSignIn
import SwiftUI

/// Выбор темы приложения (настройка на экране «Профиль», UserDefaults).
enum AppTheme: String, CaseIterable {
    case system, light, dark

    static let storageKey = "splitty.theme"

    var title: String {
        switch self {
        case .system: return "Системная"
        case .light: return "Светлая"
        case .dark: return "Тёмная"
        }
    }

    /// nil — следовать системной теме.
    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }
}

/// Точка входа приложения. SessionStore создаётся здесь и пробрасывается
/// во все view через environment.
@main
struct SplittyApp: App {
    @State private var session = SessionStore()
    @AppStorage(AppTheme.storageKey) private var themeRaw = AppTheme.system.rawValue
    // UIKit-делегат нужен для инициализации push (Firebase + APNs), SwiftUI-
    // жизненный цикл сохраняется.
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(session)
                .tint(.accent)
                // Все цветовые токены адаптивные (light/dark) — override схемы
                // на корне переключает их во всей иерархии.
                .preferredColorScheme((AppTheme(rawValue: themeRaw) ?? .system).colorScheme)
                .task {
                    // Привязываем сессию к push-менеджеру: если FCM-токен уже
                    // получен и пользователь авторизован — токен уйдёт на бэкенд.
                    PushManager.shared.attach(session: session)
                    await session.refreshMe()
                }
                // Логин/логаут → (пере)регистрация FCM-токена. Отвязка при выходе
                // делается ЯВНО в AccountView до logout (там JWT ещё валиден).
                .onChange(of: session.isAuthenticated) {
                    PushManager.shared.authStateChanged()
                }
                // Возврат из входа через Google. Основной путь (лист
                // ASWebAuthenticationSession) отдаёт результат сам, минуя эту
                // строку, но альтернативные — редирект через Safari и через
                // приложение Google Device Policy — приходят именно сюда.
                // Без handle такой вход завершается «ничем».
                .onOpenURL { url in
                    _ = GIDSignIn.sharedInstance.handle(url)
                }
        }
    }
}
