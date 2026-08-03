import Foundation
import UIKit
import UserNotifications
import FirebaseCore
import FirebaseMessaging

/// Управление push-уведомлениями (FCM):
///  • конфигурирует Firebase и APNs на старте (см. `AppDelegate`);
///  • получает FCM-токен и регистрирует его на бэкенде (`POST /me/devices`);
///  • перерегистрирует токен при логине и при его ротации (`onNewToken`);
///  • отвязывает токен при выходе (`DELETE /me/devices`, пока JWT ещё жив);
///  • показывает баннер в foreground и обрабатывает тап.
///
/// Порт android/.../push/PushTokenRegistrar + SplittyMessagingService: та же
/// логика надёжной регистрации (login → register, refresh → re-register,
/// logout → unregister, ретрай при старте) с дедупом уже отправленного токена.
///
/// Привязка/отвязка push-токена устройства — шов для тестов экранов.
///
/// Нужен ровно затем, чтобы сценарий удаления аккаунта проверялся НА УРОВНЕ
/// ЭКРАНА: сам `PushManager` тащит за собой Firebase (делегат `Messaging`,
/// APNs), в юнит-тестах не поднимается и молча ничего не делает — а именно
/// его вызов и уничтожал токен повтора (`DELETE /me/devices` висит на `s.auth`
/// и на tombstone отвечает 401). Без шва тест «повтор не ходит в /me/devices»
/// проходил бы и на сломанном коде.
protocol PushTokenBinding: AnyObject {
    /// Отвязать токен на бэкенде (`DELETE /me/devices`), пока JWT валиден.
    func unregisterCurrentToken() async
    /// Зарегистрировать текущий токен заново (`POST /me/devices`).
    func registerCurrentToken()
}

/// Singleton, т.к. должен быть делегатом Messaging/UNUserNotificationCenter,
/// которые живут дольше любого SwiftUI-view. Сессию пробрасывает `SplittyApp`.
final class PushManager: NSObject, PushTokenBinding {
    static let shared = PushManager()

    /// Сессия для доступа к `api` (актуальные baseURL/token) и статусу входа.
    /// weak — владелец SessionStore это `SplittyApp`, не мы.
    private weak var session: SessionStore?

    /// Последний известный FCM-токен (из `didReceiveRegistrationToken`).
    private var fcmToken: String?

    /// Токен, который уже успешно зарегистрирован на бэкенде — чтобы не слать
    /// один и тот же POST повторно на каждом логине/refresh.
    private var lastRegisteredToken: String?

    private override init() {}

    // MARK: Конфигурация (из AppDelegate.didFinishLaunching)

    /// Настраивает Firebase, делегатов и запрашивает разрешение + APNs-токен.
    /// Идемпотентно относительно повторного вызова FirebaseApp.configure не будет —
    /// зовётся один раз из AppDelegate.
    func configure() {
        FirebaseApp.configure()
        Messaging.messaging().delegate = self
        UNUserNotificationCenter.current().delegate = self
        requestAuthorization()
    }

    /// Привязать сессию (зовёт `SplittyApp` при появлении окна). Если FCM-токен
    /// уже получен, а пользователь авторизован — регистрируем немедленно
    /// (кейс «токен пришёл раньше, чем прицепилась сессия»).
    func attach(session: SessionStore) {
        self.session = session
        registerIfPossible()
    }

    /// Логин/логаут сменился (зовётся из `.onChange(of: isAuthenticated)`).
    /// При входе — регистрируем текущий токен; выход обрабатывает `unregister`
    /// ЯВНО до `session.logout()` (там токен ещё валиден).
    func authStateChanged() {
        registerIfPossible()
    }

    // MARK: Регистрация токена

    /// Зарегистрировать текущий токен заново. Нужен там, где отвязка уже
    /// произошла, а действие, ради которого она делалась, не состоялось —
    /// сейчас это неудавшееся удаление аккаунта (`AccountView.deleteAccount`):
    /// человек остаётся в живом аккаунте, и без повторной регистрации пуши ему
    /// молча перестали бы приходить до следующего входа или запуска.
    /// Если сессии уже нет (аккаунт всё-таки удалён) — ничего не делает.
    func registerCurrentToken() {
        registerIfPossible()
    }

    /// Регистрирует текущий FCM-токен на бэкенде, если есть сессия, вход и токен,
    /// и он отличается от уже отправленного. Ошибки best-effort (ретрай — при
    /// следующем логине/старте/refresh токена).
    private func registerIfPossible() {
        guard let session, session.isAuthenticated,
              let token = fcmToken, token != lastRegisteredToken else {
            return
        }
        Task {
            do {
                try await session.api.registerDevice(token: token)
                lastRegisteredToken = token
            } catch {
                // Молча: токен зарегистрируется при следующем триггере
                // (повторный логин, ротация токена, перезапуск приложения).
            }
        }
    }

    /// Отвязать текущий токен на бэкенде. Звать ДО `session.logout()` — там
    /// токен ещё валиден. Ждём результат (best-effort), чтобы запрос ушёл до
    /// сброса JWT. Сбрасывает дедуп, чтобы следующий вход зарегистрировал заново.
    func unregisterCurrentToken() async {
        defer { lastRegisteredToken = nil }
        guard let session, session.isAuthenticated, let token = fcmToken else {
            return
        }
        do {
            try await session.api.unregisterDevice(token: token)
        } catch {
            // Не критично: протухший на сервере токен всё равно чистится при
            // первой неудачной доставке (UNREGISTERED в outbox-воркере бэкенда).
        }
    }

    // MARK: Разрешение и APNs

    private func requestAuthorization() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { _, _ in
            // Отказ не критичен: токен всё равно регистрируется, просто система
            // не покажет баннер. Регистрацию в APNs делаем в любом случае —
            // на главном потоке (требование UIApplication).
            Task { @MainActor in
                UIApplication.shared.registerForRemoteNotifications()
            }
        }
    }
}

// MARK: - MessagingDelegate (FCM-токен)

extension PushManager: MessagingDelegate {
    /// Новый/обновлённый FCM-токен: сохраняем и (пере)регистрируем на бэкенде.
    /// Покрывает и первый выпуск токена, и ротацию (аналог Android onNewToken).
    func messaging(_ messaging: Messaging, didReceiveRegistrationToken fcmToken: String?) {
        guard let fcmToken else { return }
        self.fcmToken = fcmToken
        // Токен сменился — прошлая регистрация невалидна, дедуп сбрасываем.
        if lastRegisteredToken != fcmToken {
            lastRegisteredToken = nil
        }
        registerIfPossible()
    }
}

// MARK: - UNUserNotificationCenterDelegate (показ и тап)

extension PushManager: UNUserNotificationCenterDelegate {
    /// Пуш пришёл, когда приложение на переднем плане — показываем баннер+звук
    /// (по умолчанию iOS foreground-уведомления не показывает).
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }

    /// Тап по уведомлению: пока просто выводит приложение на передний план.
    /// Данные (`room_id`/`operation_id`) прокидываем в NotificationCenter —
    /// задел под deep-link к конкретной комнате (реализуется отдельно).
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let data = response.notification.request.content.userInfo
        if let roomId = data["room_id"] as? String {
            NotificationCenter.default.post(
                name: .splittyPushTapped,
                object: nil,
                userInfo: ["room_id": roomId, "operation_id": data["operation_id"] as? String ?? ""]
            )
        }
        completionHandler()
    }
}

extension Notification.Name {
    /// Тап по push-уведомлению (userInfo: room_id, operation_id) — задел под deep-link.
    static let splittyPushTapped = Notification.Name("splitty.push.tapped")
}
