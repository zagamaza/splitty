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

    /// Поколение кеша. `removeAll()` его увеличивает, и загрузка, стартовавшая
    /// до разлогина, свой результат уже не запишет: иначе ответ, пришедший через
    /// секунду после logout, возвращал в кеш аватар ПРЕДЫДУЩЕГО аккаунта.
    private var generation = 0

    /// Загружает аватар пользователя, если он ещё не в кеше.
    func load(_ userId: Int, api: APIClient) async {
        guard images[userId] == nil, !missing.contains(userId), !inflight.contains(userId) else {
            return
        }
        let started = generation
        inflight.insert(userId)
        defer { inflight.remove(userId) }
        do {
            let data = try await api.userAvatar(id: userId)
            guard started == generation else { return }
            if let image = UIImage(data: data) {
                images[userId] = image
            } else {
                missing.insert(userId)
            }
        } catch let error as APIError {
            guard started == generation else { return }
            if case .server(let status, _, _) = error, status == 404 {
                missing.insert(userId)
            }
            // прочие ошибки (сеть) — не кешируем, попробуем ещё раз
        } catch {
            // отмена задачи и т.п. — молча
        }
    }

    /// Картинки из своего хранилища (`GET /files/{id}`) — фото групп. Ключ
    /// строковый, а не числовой: id файла и id пользователя живут в разных
    /// пространствах, и мешать их в одном словаре нельзя.
    private(set) var fileImages: [String: UIImage] = [:]
    private var missingFiles: Set<String> = []
    private var inflightFiles: Set<String> = []

    /// Загружает картинку по id файла, если её ещё нет в кеше. Ава неизменяема:
    /// замена даёт НОВЫЙ id, поэтому кеш можно держать до конца сессии, а список
    /// групп не качает одни и те же байты на каждом скролле.
    func loadFile(_ fileId: String, api: APIClient) async {
        guard fileImages[fileId] == nil,
              !missingFiles.contains(fileId),
              !inflightFiles.contains(fileId) else {
            return
        }
        let started = generation
        inflightFiles.insert(fileId)
        defer { inflightFiles.remove(fileId) }
        do {
            let data = try await api.fileData(id: fileId)
            guard started == generation else { return }
            if let image = UIImage(data: data) {
                fileImages[fileId] = image
            } else {
                missingFiles.insert(fileId)
            }
        } catch let error as APIError {
            guard started == generation else { return }
            // 403/404 — файла нет или он не наш: повторять незачем. Сетевые
            // ошибки не кешируем, попробуем ещё раз.
            if case .server(let status, _, _) = error, status == 404 || status == 403 {
                missingFiles.insert(fileId)
            }
        } catch {
            // отмена задачи и т.п. — молча
        }
    }

    /// Забыть картинку файла: после замены или снятия авы старый кадр не должен
    /// остаться на экране.
    func forgetFile(_ fileId: String) {
        fileImages[fileId] = nil
        missingFiles.remove(fileId)
    }

    /// Полная очистка (logout).
    func removeAll() {
        generation += 1
        images = [:]
        missing = []
        fileImages = [:]
        missingFiles = []
    }
}
