import AuthenticationServices
import Foundation

/// Вход через Telegram Login Widget без ухода в приложение Telegram.
/// Серверная половина — `internal/rest/tg_callback.go`.
///
/// Подпись `hash` проверяет сервер: ключа бота у приложения нет.
enum TelegramWebAuth {

    /// Совпадает с `appScheme` на сервере и `CFBundleURLSchemes` в project.yml.
    static let callbackScheme = "splitty"

    /// Поля виджета. Ключи — как в JSON Telegram, на сервер уходят в camelCase.
    struct Payload: Decodable {
        let id: Int
        let firstName: String?
        let lastName: String?
        let username: String?
        let photoUrl: String?
        let authDate: Int64
        let hash: String

        enum CodingKeys: String, CodingKey {
            case id
            case firstName = "first_name"
            case lastName = "last_name"
            case username
            case photoUrl = "photo_url"
            case authDate = "auth_date"
            case hash
        }
    }

    enum Failure: Error {
        case cancelled
        case badResponse
        case session(Error)
    }

    @MainActor
    static func authenticate(baseURL: String, presenter: ASPresentationAnchor?) async throws -> Payload {
        guard let start = URL(string: baseURL.trimmedTrailingSlash + "/tg-auth") else {
            throw Failure.badResponse
        }

        let callbackURL: URL = try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(
                url: start,
                callbackURLScheme: callbackScheme
            ) { url, error in
                if let error {
                    let code = (error as NSError).code
                    if code == ASWebAuthenticationSessionError.canceledLogin.rawValue {
                        continuation.resume(throwing: Failure.cancelled)
                    } else {
                        continuation.resume(throwing: Failure.session(error))
                    }
                    return
                }
                guard let url else {
                    continuation.resume(throwing: Failure.badResponse)
                    return
                }
                continuation.resume(returning: url)
            }
            // Общая с Safari сессия: уже вошедшему в Telegram не логиниться заново
            session.prefersEphemeralWebBrowserSession = false
            session.presentationContextProvider = PresentationContext.shared(anchor: presenter)
            if !session.start() {
                continuation.resume(throwing: Failure.badResponse)
            }
        }

        return try decode(callbackURL: callbackURL)
    }

    /// Отдельно от `authenticate`, чтобы разбор проверялся тестами без живой сессии.
    static func decode(callbackURL: URL) throws -> Payload {
        guard let components = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false) else {
            throw Failure.badResponse
        }
        // Telegram кладёт результат и в query, и во fragment
        let fromQuery = components.queryItems?.first { $0.name == "tgAuthResult" }?.value
        let fromFragment = components.fragment
            .flatMap { URLComponents(string: "?" + $0)?.queryItems }?
            .first { $0.name == "tgAuthResult" }?.value

        guard let encoded = fromQuery ?? fromFragment, !encoded.isEmpty,
              let data = Data(base64URLEncoded: encoded),
              let payload = try? JSONDecoder().decode(Payload.self, from: data) else {
            throw Failure.badResponse
        }
        return payload
    }

    /// ASWebAuthenticationSession требует anchor, а SwiftUI его не даёт.
    private final class PresentationContext: NSObject, ASWebAuthenticationPresentationContextProviding {
        private static var instance: PresentationContext?
        private var anchor: ASPresentationAnchor?

        static func shared(anchor: ASPresentationAnchor?) -> PresentationContext {
            let ctx = instance ?? PresentationContext()
            ctx.anchor = anchor
            instance = ctx
            return ctx
        }

        func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
            anchor ?? ASPresentationAnchor()
        }
    }
}

extension Data {
    /// base64url: `-`/`_` вместо `+`/`/`, хвостовые `=` опущены.
    init?(base64URLEncoded string: String) {
        var s = string
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let remainder = s.count % 4
        if remainder > 0 {
            s += String(repeating: "=", count: 4 - remainder)
        }
        guard let data = Data(base64Encoded: s) else { return nil }
        self = data
    }
}

private extension String {
    var trimmedTrailingSlash: String {
        hasSuffix("/") ? String(dropLast()) : self
    }
}
