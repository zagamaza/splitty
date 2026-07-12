import Foundation
import Observation

// MARK: - Модель outbox

/// Параметры расхода для тела POST/PUT операции (локальная копия формы):
/// описание, сумма, донор и способ деления — ровно ОДНО из полей
/// `recipientIds` (поровну) / `recipientSums` (точными суммами), как в `OperationBody`.
struct OutboxPayload: Codable, Equatable {
    var description: String
    var sum: Int
    var donorId: Int
    var recipientIds: [Int]?
    var recipientSums: [RecipientSum]?

    /// Способ деления для методов APIClient.
    var split: ExpenseSplit {
        if let recipientSums {
            return .byExactAmount(recipientSums: recipientSums)
        }
        return .equally(recipientIds: recipientIds ?? [])
    }
}

/// Запись outbox: локальная операция, ожидающая отправки на сервер.
struct OutboxEntry: Codable, Identifiable, Equatable {
    /// Тип отложенной мутации. Приложение сейчас создаёт только `.create`:
    /// правка/удаление ещё НЕ отправленной операции меняет саму запись outbox,
    /// а редактирование синхронизированных операций офлайн запрещено.
    /// `.update`/`.delete` (с `targetOperationId`) — зафиксированная схема v1
    /// на будущее, синк их тоже умеет.
    enum Kind: String, Codable {
        case create, update, delete
    }

    /// Статус: ждёт отправки или отвергнута сервером (400/403/404).
    enum Status: Codable, Equatable {
        case pending
        case failed(message: String)

        /// Текст ошибки для failed; nil — pending.
        var failureMessage: String? {
            if case .failed(let message) = self { return message }
            return nil
        }
    }

    /// Локальный id записи; он же идемпотентный ключ `clientOpId` при POST —
    /// повторная отправка после потерянного ответа не создаёт дубль.
    let localId: UUID
    let roomId: String
    var kind: Kind
    var payload: OutboxPayload?
    /// id серверной операции для `.update`/`.delete`.
    var targetOperationId: String?
    let createdAt: Date
    var status: Status

    var id: UUID { localId }

    var isFailed: Bool { status.failureMessage != nil }
}

// MARK: - Хранилище

/// Outbox расходов: FIFO-очередь локальных операций в файле
/// Application Support/outbox.json (атомарная перезапись при каждой мутации).
/// Мутации и синк — только с главного актёра (SwiftUI наблюдает `entries`).
@Observable
final class OutboxStore {
    /// Записи в порядке создания (FIFO: старые первыми — так же и отправляются).
    private(set) var entries: [OutboxEntry] = []

    /// true, пока идёт отправка outbox (баннер «Отправка…»).
    private(set) var isSyncing = false

    private let fileURL: URL
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder
    /// Серийная фоновая очередь записи файла: encode + запись при каждой
    /// мутации шли на главном потоке и дёргали анимации. Серийность
    /// гарантирует порядок перезаписей (последний снимок побеждает).
    private let io = DispatchQueue(label: "splitty.outbox.io", qos: .utility)

    /// Файл по умолчанию: Application Support/outbox.json
    /// (отдельно от каталога read-кеша SplittyCache).
    static var defaultFileURL: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("outbox.json")
    }

    /// `fileURL` переопределяется в тестах (временный файл).
    init(fileURL: URL = OutboxStore.defaultFileURL) {
        self.fileURL = fileURL
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
        if let data = try? Data(contentsOf: fileURL),
           let loaded = try? self.decoder.decode([OutboxEntry].self, from: data) {
            entries = loaded
        }
    }

    /// Записи комнаты (для списка операций группы и бейджей), FIFO.
    func entries(roomId: String) -> [OutboxEntry] {
        entries.filter { $0.roomId == roomId }
    }

    /// Ставит офлайн-создание расхода в очередь (kind=.create, status=.pending).
    /// `localId` можно передать заранее: он же используется как clientOpId
    /// прямого POST — если ответ потерялся, досылка не создаст дубль.
    @MainActor
    @discardableResult
    func add(roomId: String, payload: OutboxPayload, localId: UUID = UUID()) -> OutboxEntry {
        let entry = OutboxEntry(
            localId: localId,
            roomId: roomId,
            kind: .create,
            payload: payload,
            targetOperationId: nil,
            createdAt: Date(),
            status: .pending
        )
        entries.append(entry)
        persist()
        return entry
    }

    /// Правка ещё не отправленной записи: новый payload, статус сбрасывается
    /// в pending (исправленная failed-запись отправится при следующем синке).
    @MainActor
    func update(localId: UUID, payload: OutboxPayload) {
        guard let index = entries.firstIndex(where: { $0.localId == localId }) else { return }
        entries[index].payload = payload
        entries[index].status = .pending
        persist()
    }

    /// Удаление записи (отмена локальной операции или успешная отправка).
    @MainActor
    func remove(localId: UUID) {
        entries.removeAll { $0.localId == localId }
        persist()
    }

    /// Сервер отверг запись (400/403/404): остаётся в списке со статусом
    /// failed и текстом ошибки — открыть/исправить/удалить может пользователь.
    @MainActor
    func markFailed(localId: UUID, message: String) {
        guard let index = entries.firstIndex(where: { $0.localId == localId }) else { return }
        entries[index].status = .failed(message: message)
        persist()
    }

    /// Полная очистка (logout).
    @MainActor
    func clear() {
        entries = []
        persist()
    }

    // MARK: Синхронизация

    /// Отправляет pending-записи строго по одной в порядке FIFO (сериализовано:
    /// повторный вызов во время работы — no-op, параллельных отправок нет).
    /// Успех → запись удаляется; 4xx → failed(текст ошибки), идём дальше;
    /// сетевая ошибка/5xx → запись остаётся pending, синк прерывается до
    /// следующего триггера (возврат сети, активация приложения, pull-to-refresh).
    /// Возвращает true, если отправлена хотя бы одна запись (нужен noteDataChanged).
    @MainActor
    func sync(api: APIClient) async -> Bool {
        guard !isSyncing else { return false }
        let pending = entries.filter { $0.status == .pending }
        guard !pending.isEmpty else { return false }
        isSyncing = true
        defer { isSyncing = false }

        var syncedAny = false
        for queued in pending {
            // Запись могли изменить/удалить, пока отправлялась предыдущая.
            guard let entry = entries.first(where: { $0.localId == queued.localId }),
                  entry.status == .pending else { continue }
            do {
                try await send(entry, api: api)
                remove(localId: entry.localId)
                syncedAny = true
            } catch let error as APIError {
                if case .server(let status, _, _) = error, (400..<500).contains(status) {
                    // Сервер отверг операцию — правка данных не поможет сама собой:
                    // помечаем failed, пользователь исправит/удалит. Идём к следующей.
                    markFailed(localId: entry.localId, message: error.localizedDescription)
                } else {
                    // Сеть/5xx: остаёмся pending, повторим при следующем триггере.
                    break
                }
            } catch is OutboxError {
                markFailed(localId: entry.localId, message: "Некорректная локальная запись")
            } catch {
                // Отмена задачи и прочее — прерываемся, записи остаются pending.
                break
            }
        }
        return syncedAny
    }

    /// Битая запись outbox (нет payload/targetOperationId для своего kind).
    private struct OutboxError: Error {}

    private func send(_ entry: OutboxEntry, api: APIClient) async throws {
        switch entry.kind {
        case .create:
            guard let payload = entry.payload else { throw OutboxError() }
            // clientOpId = localId: бэкенд на повтор отвечает 200 существующей
            // операцией — повторная отправка после потерянного ответа безопасна.
            _ = try await api.addOperation(
                roomId: entry.roomId,
                description: payload.description,
                sum: payload.sum,
                donorId: payload.donorId,
                split: payload.split,
                clientOpId: entry.localId.uuidString
            )
        case .update:
            guard let payload = entry.payload, let operationId = entry.targetOperationId else {
                throw OutboxError()
            }
            _ = try await api.updateOperation(
                roomId: entry.roomId,
                operationId: operationId,
                description: payload.description,
                sum: payload.sum,
                donorId: payload.donorId,
                split: payload.split
            )
        case .delete:
            guard let operationId = entry.targetOperationId else { throw OutboxError() }
            try await api.deleteOperation(roomId: entry.roomId, operationId: operationId)
        }
    }

    private func persist() {
        // Снимок на вызывающем потоке, encode и запись — в фоне.
        let snapshot = entries
        io.async { [encoder, fileURL] in
            guard let data = try? encoder.encode(snapshot) else { return }
            try? FileManager.default.createDirectory(
                at: fileURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            try? data.write(to: fileURL, options: [.atomic])
        }
    }

    /// Барьер для тестов: дожидается завершения всех фоновых записей.
    func waitForPendingWrites() {
        io.sync {}
    }
}
