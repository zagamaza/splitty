import AuthenticationServices
import Foundation

/// Исход системного листа Apple, который экрану нужно различать.
///
/// `cancelled` вынесена отдельно намеренно: человек, закрывший лист, не должен
/// получать алерт — это решение, а не сбой (та же логика, что в
/// `GoogleSignInError`). Текст остальных случаев задаёт ЭКРАН: у входа и у
/// привязки он разный («…для входа» / «…для привязки»).
enum AppleSignInError: Error {
    /// Человек закрыл системный лист. Обрабатывается ТИХО.
    case cancelled
    /// Не удалось сгенерировать nonce: системный источник случайности отказал.
    /// Продолжать нельзя — непредсказуемость nonce и есть вся защита от
    /// повторного использования чужого токена (см. `AppleNonce`).
    case nonceUnavailable
    /// Apple вернул ответ без id-токена — слать на бэкенд нечего.
    case missingCredential
}

/// Общая часть Sign in with Apple для ВХОДА (`LoginView`) и ПРИВЯЗКИ
/// (`AccountView`): настройка системного запроса и разбор его ответа.
///
/// Обе стороны листа обязаны совпадать (nonce, скоупы, извлечение
/// `authorizationCode`), а различаются экраны только тем, что делают с
/// результатом. Пока этот код был скопирован в оба экрана, он успел разъехаться:
/// вход подставлял `?? ""` вместо отсутствующего кода, привязка оставляла его
/// опциональным — то есть одно и то же поле уезжало на сервер двумя способами.
enum AppleSignInService {
    /// Данные листа Apple, нужные бэкенду.
    struct Credential {
        /// Подписанный Apple id-токен (`identityToken`).
        let idToken: String
        /// СЫРОЙ nonce этой попытки: в токене лежит его SHA256, сервер сверяет
        /// одно с другим (см. `AppleNonce`).
        let rawNonce: String
        /// Имя — Apple отдаёт его ТОЛЬКО при первом входе, дальше пусто.
        /// Пустая строка нормальна: сервер не затирает уже сохранённое имя.
        let displayName: String
        /// Одноразовый `authorizationCode`, живёт минуты. Сервер меняет его на
        /// refresh token, без которого при удалении аккаунта нечем звать
        /// `auth/revoke` (Apple Guideline 5.1.1(v)); «добрать» позже нельзя.
        /// nil — Apple кода не вернул, это не повод отменять вход.
        let authorizationCode: String?
    }

    /// Готовит системный запрос и возвращает СЫРОЙ nonce, который экран обязан
    /// сохранить до `onCompletion` и передать сюда же.
    ///
    /// В запрос уходит ХЕШ — именно он попадёт в подписанный Apple токен;
    /// сырое значение остаётся у клиента и уедет на сервер телом запроса,
    /// чтобы серверу было что с чем сверять.
    ///
    /// nil — системный генератор случайности отказал: nonce в запрос не
    /// проставлен, и `credential(from:rawNonce:)` бросит `.nonceUnavailable`.
    static func prepare(_ request: ASAuthorizationAppleIDRequest) -> String? {
        request.requestedScopes = [.fullName, .email]
        guard let rawNonce = try? AppleNonce.random() else {
            return nil
        }
        request.nonce = AppleNonce.sha256Hex(rawNonce)
        return rawNonce
    }

    /// Разбирает ответ листа. `rawNonce` — то, что вернул `prepare`.
    ///
    /// Бросает `AppleSignInError.cancelled` на отмену, `.nonceUnavailable`,
    /// если nonce этой попытки потерян, `.missingCredential`, если Apple не
    /// дал id-токена, и исходную ошибку системы во всех прочих случаях.
    static func credential(
        from result: Result<ASAuthorization, Error>,
        rawNonce: String?
    ) throws -> Credential {
        switch result {
        case .failure(let error):
            if let authError = error as? ASAuthorizationError, authError.code == .canceled {
                throw AppleSignInError.cancelled
            }
            throw error
        case .success(let authorization):
            guard let rawNonce else {
                throw AppleSignInError.nonceUnavailable
            }
            guard let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
                  let identityToken = credential.identityToken,
                  let idToken = String(data: identityToken, encoding: .utf8)
            else {
                throw AppleSignInError.missingCredential
            }
            return Credential(
                idToken: idToken,
                rawNonce: rawNonce,
                displayName: displayName(credential.fullName),
                authorizationCode: credential.authorizationCode
                    .flatMap { String(data: $0, encoding: .utf8) }
            )
        }
    }

    /// Имя из `ASAuthorizationAppleIDCredential.fullName`.
    private static func displayName(_ components: PersonNameComponents?) -> String {
        guard let components else { return "" }
        return PersonNameComponentsFormatter
            .localizedString(from: components, style: .default)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
