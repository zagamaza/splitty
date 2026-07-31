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
    /// Размер сырого nonce: 32 случайных байта = 256 бит. Строкой он
    /// становится в hex, то есть 64 символа `0…9a…f`.
    static let byteCount = 32

    /// Криптостойкий сырой nonce — hex от `byteCount` случайных байт.
    ///
    /// Hex, а не «алфавит из букв и цифр»: от строки здесь нужна ровно одна
    /// вещь — непредсказуемость, а любой алфавит, размер которого не делит
    /// 256, требует отбраковки байтов ради равномерности. Hex делит нацело
    /// по построению, безопасен в JSON, в JWT-claim и в URL, и совпадает
    /// с кодировкой `sha256Hex(_:)` — тот же формат по обе стороны протокола.
    static func random() -> String {
        var bytes = [UInt8](repeating: 0, count: byteCount)
        let status = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        guard status == errSecSuccess else {
            // Единственный документированный отказ — недоступность системного
            // источника случайности. Отдать предсказуемое значение нельзя:
            // непредсказуемость nonce и есть вся защита входа от повторного
            // использования чужого токена.
            fatalError("SecRandomCopyBytes завершился с ошибкой \(status)")
        }
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    /// SHA256 от UTF-8 представления строки, в нижнем регистре hex —
    /// значение для `ASAuthorizationAppleIDRequest.nonce`.
    static func sha256Hex(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8))
            .map { String(format: "%02x", $0) }
            .joined()
    }
}
