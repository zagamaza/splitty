import CryptoKit
import Foundation
import Security

// MARK: - Nonce для Sign in with Apple

/// Одноразовое значение, связывающее конкретный запрос входа с выданным
/// Apple id-токеном.
///
/// Протокол ровно один и нарушать его половинками нельзя:
/// 1. клиент генерирует СЫРОЙ nonce (`random()`);
/// 2. в `ASAuthorizationAppleIDRequest.nonce` кладётся его SHA256 в hex
///    (`sha256Hex(_:)`) — именно хеш Apple подпишет внутри токена;
/// 3. в теле `POST /api/v1/auth/apple` на сервер уходит СЫРОЕ значение.
///
/// Сервер (`internal/rest/auth.go`, `handleAuthApple`) сверяет
/// `hex(sha256(rawNonce))` с claim `nonce` подписанного токена constant-time.
/// Отправить хеш вместо сырого значения — значит превратить проверку в
/// самоподтверждение: перехваченный у другого клиента токен прошёл бы вход.
enum AppleNonce {
    /// Алфавит сырого nonce: буквы, цифры и `-._`. Всё это безопасно
    /// одновременно в JSON-теле, в JWT-claim и в URL — экранирование по пути
    /// от клиента до Apple и обратно ничего не поменяет.
    static let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._")

    /// Длина по умолчанию: 32 символа алфавита из 65 — около 192 бит энтропии.
    static let defaultLength = 32

    /// Криптостойкий сырой nonce из `alphabet`.
    ///
    /// Байты берутся у `SecRandomCopyBytes`; «хвост» диапазона отбрасывается,
    /// чтобы взятие остатка не перекосило распределение в пользу первых
    /// символов алфавита (256 на 65 нацело не делится).
    static func random(length: Int = defaultLength) -> String {
        precondition(length > 0, "длина nonce должна быть положительной")

        let size = alphabet.count
        let limit = 256 - (256 % size)

        var result = ""
        result.reserveCapacity(length)
        var buffer = [UInt8](repeating: 0, count: length)

        while result.count < length {
            let status = SecRandomCopyBytes(kSecRandomDefault, buffer.count, &buffer)
            guard status == errSecSuccess else {
                // Единственный документированный отказ — недоступность
                // системного источника случайности. Отдать предсказуемое
                // значение нельзя: непредсказуемость nonce и есть вся защита
                // входа от повторного использования чужого токена.
                fatalError("SecRandomCopyBytes завершился с ошибкой \(status)")
            }
            for byte in buffer where result.count < length {
                guard Int(byte) < limit else { continue }
                result.append(alphabet[Int(byte) % size])
            }
        }

        return result
    }

    /// SHA256 от UTF-8 представления строки, в нижнем регистре hex —
    /// значение для `ASAuthorizationAppleIDRequest.nonce`.
    static func sha256Hex(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8))
            .map { String(format: "%02x", $0) }
            .joined()
    }
}
