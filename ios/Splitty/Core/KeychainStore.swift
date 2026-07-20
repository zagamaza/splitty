import Foundation
import Security

/// Простое хранилище строк в Keychain (kSecClassGenericPassword).
/// Используется для JWT-токена сессии.
enum KeychainStore {
    private static let service = "com.zagir.splitty"

    /// Сохраняет значение. `@discardableResult` — вызывающему обычно нечего
    /// делать с ошибкой, но молча терять токен нельзя: без успешной записи
    /// приложение выглядит залогиненным до перезапуска.
    @discardableResult
    static func save(_ value: String, key: String) -> Bool {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        // ThisDeviceOnly обязателен: по умолчанию (WhenUnlocked) элемент попадает
        // в зашифрованные бэкапы и iCloud Keychain и восстанавливается на ДРУГОМ
        // устройстве вместе с живой сессией. Android-клиент закрыл это же место
        // исключением session-хранилища из бэкапа.
        let attributes: [String: Any] = [
            kSecValueData as String: Data(value.utf8),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        // Сначала update, и только если элемента нет — add. Прежний вариант делал
        // безусловный SecItemDelete перед SecItemAdd: если add падал (устройство
        // ещё не разблокировано после перезагрузки — errSecInteractionNotAllowed),
        // старый рабочий токен был уже стёрт, и пользователя разлогинивало.
        var status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            var insert = query
            attributes.forEach { insert[$0.key] = $0.value }
            status = SecItemAdd(insert as CFDictionary, nil)
        }
        if status != errSecSuccess {
            print("KeychainStore.save failed for \(key): OSStatus \(status)")
            return false
        }
        return true
    }

    static func read(key: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data
        else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    static func delete(key: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(query as CFDictionary)
    }
}
