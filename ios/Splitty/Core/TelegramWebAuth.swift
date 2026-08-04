import AuthenticationServices
import Foundation

/// Вход через Telegram Login Widget без ухода в приложение Telegram.
///
/// Поток (серверная половина — `internal/rest/tg_callback.go`):
///
///   1. открываем `ASWebAuthenticationSession` на `<baseURL>/tg-auth`;
///      сервер редиректит на `oauth.telegram.org` со СВОИМ origin — клиент
///      его не собирает, иначе origin не совпал бы с доменом бота;
///   2. человек подтверждает вход, Telegram возвращает браузер на
///      `<domain>/tg-callback?tgAuthResult=<base64>`;
///   3. страница уводит в `splitty://tg-callback?tgAuthResult=…`, сессия
///      ловит схему и отдаёт нам URL;
///   4. декодируем payload и шлём поля в `POST /auth/telegram`, где подпись и
///      свежесть `auth_date` проверяются серверным `checkTelegramHash`.
///
/// ⚠️ Клиент НИЧЕГО не проверяет: `hash` подписан ключом бота, которого у
/// приложения нет и быть не должно. Задача клиента — донести поля до сервера
/// без искажений.
enum TelegramWebAuth {

    /// Схема возврата. Совпадает с `appScheme` на сервере и с
    /// `CFBundleURLSchemes` в project.yml — три места, менять только вместе.
    static let callbackScheme = "splitty"

    /// Поля Telegram Login Widget. Имена — как в JSON от Telegram (snake_case),
    /// на сервер уходят в его camelCase (см. `telegramAuthRequest`).
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
        /// Человек закрыл окно — молчим, алерт показывать нельзя
        case cancelled
        /// Telegram вернулся без результата либо payload не разбирается
        case badResponse
        case session(Error)
    }

    /// Проводит сессию до конца и возвращает разобранный payload.
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
            // Общая с Safari сессия: если человек уже вошёл в Telegram в
            // браузере, повторно логиниться не придётся
            session.prefersEphemeralWebBrowserSession = false
            session.presentationContextProvider = PresentationContext.shared(anchor: presenter)
            if !session.start() {
                continuation.resume(throwing: Failure.badResponse)
            }
        }

        return try decode(callbackURL: callbackURL)
    }

    /// Достаёт payload из `splitty://tg-callback?tgAuthResult=<base64>`.
    /// Вынесено отдельно, чтобы разбор проверялся тестами без живой сессии.
    static func decode(callbackURL: URL) throws -> Payload {
        guard let components = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false) else {
            throw Failure.badResponse
        }
        // Telegram кладёт результат и в query, и во fragment — смотрим оба
        let fromQuery = components.queryItems?.first { $0.name == "tgAuthResult" }?.value
        let fromFragment = components.fragment
            .flatMap { URLComponents(string: "?" + $0)?.queryItems }?
            .first { $0.name == "tgAuthResult" }?.value

        guard let encoded = fromQuery ?? fromFragment, !encoded.isEmpty else {
            throw Failure.badResponse
        }
        guard let data = Data(base64URLEncoded: encoded) else {
            throw Failure.badResponse
        }
        guard let payload = try? JSONDecoder().decode(Payload.self, from: data) else {
            throw Failure.badResponse
        }
        return payload
    }

    /// Держатель окна для сессии: ASWebAuthenticationSession требует anchor,
    /// а SwiftUI его не даёт — берём ключевое окно сцены.
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
    /// base64url (Telegram отдаёт именно его): `-`/`_` вместо `+`/`/`,
    /// хвостовые `=` опущены
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
