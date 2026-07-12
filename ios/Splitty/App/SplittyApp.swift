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

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(session)
                .tint(.accent)
                // Все цветовые токены адаптивные (light/dark) — override схемы
                // на корне переключает их во всей иерархии.
                .preferredColorScheme((AppTheme(rawValue: themeRaw) ?? .system).colorScheme)
                .task {
                    await session.refreshMe()
                }
        }
    }
}
