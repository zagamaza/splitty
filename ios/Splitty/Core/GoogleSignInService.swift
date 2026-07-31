import Foundation
import GoogleSignIn
import UIKit

/// Исход входа через Google, который экрану нужно различать.
///
/// `cancelled` вынесена в отдельный случай намеренно: человек, закрывший
/// системный лист, не должен получать алерт — это не сбой, а решение.
/// Всё остальное — обычные ошибки с человеческим текстом.
enum GoogleSignInError: LocalizedError {
    /// Пользователь закрыл лист входа. Обрабатывается ТИХО.
    case cancelled
    /// Не нашли, из чего показать лист (сцена не активна). Практически
    /// недостижимо с экрана входа, но молчать об этом нельзя.
    case noPresenter
    /// Google вернул сессию без id-токена — слать на бэкенд нечего.
    case missingIdToken

    var errorDescription: String? {
        switch self {
        case .cancelled:
            return "Вход через Google отменён"
        case .noPresenter:
            return "Не удалось открыть окно входа Google. Попробуйте ещё раз"
        case .missingIdToken:
            return "Google не вернул данные для входа. Попробуйте ещё раз"
        }
    }
}

/// Тонкая обёртка над `GIDSignIn`: показывает системный лист и отдаёт
/// подписанный Google **id-токен** — единственное, что нужно бэкенду
/// (`POST /api/v1/auth/google`, см. `APIClient.loginWithGoogle`).
///
/// Client id намеренно НЕ задаётся здесь кодом: SDK читает `GIDClientID`
/// из Info.plist (`ios/project.yml` → `info.properties`), рядом с
/// reversed-схемой в `CFBundleURLTypes`. Два способа задать одно и то же
/// значение — это два места, где оно может разойтись.
enum GoogleSignInService {
    /// Показывает лист входа и возвращает id-токен.
    /// Бросает `GoogleSignInError.cancelled`, если человек закрыл лист.
    @MainActor
    static func signIn() async throws -> String {
        guard let presenter = topViewController() else {
            throw GoogleSignInError.noPresenter
        }

        let result: GIDSignInResult
        do {
            result = try await withCheckedThrowingContinuation { continuation in
                // Явная обёртка над completion-вариантом, а не автоматический
                // async-импорт: тот зависит от того, как ObjC-заголовок
                // импортировался в конкретной версии SDK, и молча меняется
                // при обновлении.
                GIDSignIn.sharedInstance.signIn(withPresenting: presenter) { signInResult, error in
                    if let error {
                        continuation.resume(throwing: error)
                        return
                    }
                    guard let signInResult else {
                        continuation.resume(throwing: GoogleSignInError.missingIdToken)
                        return
                    }
                    continuation.resume(returning: signInResult)
                }
            }
        } catch {
            throw isCancellation(error) ? GoogleSignInError.cancelled : error
        }

        guard let idToken = result.user.idToken?.tokenString, !idToken.isEmpty else {
            throw GoogleSignInError.missingIdToken
        }
        return idToken
    }

    /// true, если ошибка означает «человек закрыл лист».
    /// Сверяем и домен, и код: код -5 сам по себе встречается в других
    /// доменах и значит там совсем другое.
    static func isCancellation(_ error: Error) -> Bool {
        if error is CancellationError {
            return true
        }
        let nsError = error as NSError
        return nsError.domain == kGIDSignInErrorDomain
            && nsError.code == GIDSignInError.canceled.rawValue
    }

    /// Верхний показанный контроллер активной сцены — SDK нужен именно он,
    /// иначе лист попытается открыться поверх уже закрытого экрана.
    @MainActor
    private static func topViewController() -> UIViewController? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        guard let window = scene?.windows.first(where: \.isKeyWindow) ?? scene?.windows.first,
              var top = window.rootViewController
        else {
            return nil
        }
        while let presented = top.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        return top
    }
}
