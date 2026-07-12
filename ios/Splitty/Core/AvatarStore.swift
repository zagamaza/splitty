import Observation
import UIKit

/// In-memory кеш аватаров из Telegram (GET /users/{id}/avatar через бэкенд).
/// 404 («нет фото») кешируется в `missing` — без повторных походов на каждый
/// список; сетевые ошибки НЕ кешируются, попробуем при следующем показе.
/// Чистится при logout вместе с остальными пользовательскими данными.
@MainActor
@Observable
final class AvatarStore {
    private(set) var images: [Int: UIImage] = [:]
    private var missing: Set<Int> = []
    private var inflight: Set<Int> = []

    /// Загружает аватар пользователя, если он ещё не в кеше.
    func load(_ userId: Int, api: APIClient) async {
        guard images[userId] == nil, !missing.contains(userId), !inflight.contains(userId) else {
            return
        }
        inflight.insert(userId)
        defer { inflight.remove(userId) }
        do {
            let data = try await api.userAvatar(id: userId)
            if let image = UIImage(data: data) {
                images[userId] = image
            } else {
                missing.insert(userId)
            }
        } catch let error as APIError {
            if case .server(let status, _, _) = error, status == 404 {
                missing.insert(userId)
            }
            // прочие ошибки (сеть) — не кешируем, попробуем ещё раз
        } catch {
            // отмена задачи и т.п. — молча
        }
    }

    /// Полная очистка (logout).
    func removeAll() {
        images = [:]
        missing = []
    }
}
