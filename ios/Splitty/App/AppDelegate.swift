import UIKit
import FirebaseMessaging

/// UIKit-делегат приложения: нужен только для инициализации push (Firebase +
/// APNs). SwiftUI-жизненный цикл сохраняется, делегат цепляется через
/// `@UIApplicationDelegateAdaptor` в `SplittyApp`.
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        // FirebaseApp.configure + делегаты Messaging/UNUserNotificationCenter +
        // запрос разрешения и регистрация в APNs.
        PushManager.shared.configure()
        return true
    }

    /// APNs выдал device-token — отдаём его FCM. При включённом swizzling
    /// Firebase делает это сам, но явная передача надёжнее и рекомендована докой.
    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        Messaging.messaging().apnsToken = deviceToken
    }

    /// Не удалось зарегистрироваться в APNs (нет сети, симулятор без push и т.п.).
    /// Не критично: FCM-токен просто не появится, локально приложение работает.
    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        // Тихо: push недоступен на этом устройстве/сборке.
    }
}
