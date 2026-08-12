import GoogleSignIn
import SwiftUI

/// Выбор темы приложения (настройка на экране «Профиль», UserDefaults).
enum AppTheme: String, CaseIterable {
    case system, light, dark

    static let storageKey = "splitty.theme"

    var title: String {
        switch self {
        case .system: return String(localized: "Системная")
        case .light: return String(localized: "Светлая")
        case .dark: return String(localized: "Тёмная")
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
                    // Бейдж обязан появляться до открытия раздела — иначе
                    // счётчик показывался бы ровно тогда, когда его гасят.
                    await session.refreshUnreadCount()
                }
                // Логин/логаут → (пере)регистрация FCM-токена. Отвязка при выходе
                // делается ЯВНО в AccountView до logout (там JWT ещё валиден).
                .onChange(of: session.isAuthenticated) {
                    PushManager.shared.authStateChanged()
                    // Вход: счётчик у нового человека свой, и `.task` корня
                    // второй раз уже не выполнится — без этого бейдж оставался
                    // бы пустым до возврата из фона.
                    if session.isAuthenticated {
                        Task { await session.refreshUnreadCount() }
                    } else {
                        session.unreadNotifications = 0
                    }
                }
                // Возврат из фона и push — второй и третий источники счётчика
                // помимо старта. Приход (баннер поверх открытого приложения) и
                // тап — разные события: по первому бейдж обязан обновиться, не
                // дожидаясь, пока человек тапнет.
                .onReceive(NotificationCenter.default.publisher(for: .splittyPushTapped)) { _ in
                    // Не только счётчик: пуш означает, что данные на сервере
                    // изменились — расход добавили, долг погасили. Раньше
                    // менялся бейдж, а открытый экран продолжал показывать
                    // старые суммы, пока человек не потянет список
                    session.noteDataChanged()
                    Task { await session.refreshUnreadCount() }
                }
                .onReceive(NotificationCenter.default.publisher(for: .splittyPushReceived)) { _ in
                    session.noteDataChanged()
                    Task { await session.refreshUnreadCount() }
                }
                // ЕДИНСТВЕННЫЙ onOpenURL сцены: второй такой модификатор не
                // «добавляется», а побеждает — один из обработчиков перестал бы
                // получать ссылки вовсе. Поэтому новые схемы дописываются сюда.
                .onOpenURL { url in
                    // Возврат из входа через Google. Основной путь (лист
                    // ASWebAuthenticationSession) отдаёт результат сам, минуя
                    // эту строку, но альтернативные — редирект через Safari и
                    // через приложение Google Device Policy — приходят именно
                    // сюда. Без handle такой вход завершается «ничем».
                    if GIDSignIn.sharedInstance.handle(url) {
                        return
                    }
                    // Кнопка «Открыть в приложении» на странице приглашения:
                    // splitty://join/<roomId> (см. internal/rest/deeplink.go).
                    handleJoinLink(url)
                }
                // Universal link https://<domain>/join/<roomId> — тап по самой
                // ссылке в мессенджере. Приходит отдельным каналом (NSUserActivity),
                // onOpenURL его НЕ видит.
                .onContinueUserActivity(NSUserActivityTypeBrowsingWeb) { activity in
                    guard let url = activity.webpageURL else { return }
                    handleJoinLink(url)
                }
        }
    }

    /// Ссылка-приглашение: намерение только запоминается, исполняет его
    /// `RootView`. Так одна и та же дорога работает и для авторизованного
    /// (RootView заберёт намерение сразу), и для гостя (заберёт после входа),
    /// и для холодного старта, когда ссылка приходит раньше, чем появляется
    /// хоть один экран.
    private func handleJoinLink(_ url: URL) {
        guard let roomId = RoomCodeParser.roomId(from: url) else { return }
        // Владелец намерения — тот, кто в аккаунте ПРЯМО СЕЙЧАС (nil у гостя:
        // намерение достанется первому вошедшему, это и есть путь приглашения).
        // Записывается здесь, а не при входе: только в этот момент ещё
        // известно, чья ссылка. См. `PendingJoin.reconcileOwner`.
        PendingJoin.shared.set(roomId, ownerId: session.me?.id)
    }
}
