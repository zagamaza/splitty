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
    /// Позиции чека itemized-операции: переживают enqueue → flush, чтобы офлайн-
    /// сохранение не теряло разбивку. `default = nil` — старый memberwise-
    /// инициализатор (тесты/вызовы без items) остаётся валиден.
    var items: [OperationItem]? = nil

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
    /// Владелец записи — id пользователя, создавшего её. nil у записей старых
    /// версий. Нужен, чтобы после смены аккаунта очередь пользователя A не
    /// улетела на сервер под токеном пользователя B (см. `keepOwned`).
    var ownerUserId: Int?

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
        // Различаем «файла нет» и «прочитать не удалось». Под
        // completeFileProtection фоновый запуск на залоченном устройстве даёт
        // ошибку чтения: раньше оба случая давали пустой entries, и первая же
        // запись затирала очередь неотправленных расходов начисто.
        if FileManager.default.fileExists(atPath: fileURL.path) {
            if let data = try? Data(contentsOf: fileURL) {
                // Файл прочитан — писать безопасно. Битый JSON (обрыв записи,
                // смена схемы) восстановлению не подлежит: если оставить
                // didLoad = false, persist навсегда замолкает и офлайн-очередь
                // живёт только в памяти, пропадая при закрытии приложения.
                didLoad = true
                if let loaded = try? self.decoder.decode([OutboxEntry].self, from: data) {
                    entries = loaded
                }
            }
        } else {
            didLoad = true // очереди ещё не было — писать безопасно
        }
    }

    /// Очередь успешно прочитана (или её ещё не существовало). Пока false —
    /// сохранять нельзя: перезапись затрёт непрочитанные записи.
    private var didLoad = false

    /// Записи комнаты (для списка операций группы и бейджей), FIFO.
    func entries(roomId: String) -> [OutboxEntry] {
        entries.filter { $0.roomId == roomId }
    }

    /// Ставит офлайн-создание расхода в очередь (kind=.create, status=.pending).
    /// `localId` можно передать заранее: он же используется как clientOpId
    /// прямого POST — если ответ потерялся, досылка не создаст дубль.
    @MainActor
    @discardableResult
    func add(
        roomId: String,
        payload: OutboxPayload,
        localId: UUID = UUID(),
        ownerUserId: Int? = nil
    ) -> OutboxEntry {
        let entry = OutboxEntry(
            localId: localId,
            roomId: roomId,
            kind: .create,
            payload: payload,
            targetOperationId: nil,
            createdAt: Date(),
            status: .pending,
            ownerUserId: ownerUserId
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

    /// Значит ли 4xx, что запись больше никогда не пройдёт и её надо пометить
    /// failed (пользователь исправит руками).
    ///
    /// 401 — НЕ про данные, а про протухший токен: пометить им всю очередь
    /// значило бы обнулить офлайн-ввод при каждой переавторизации, ровно то,
    /// что `expireSession()` специально пытается сохранить. 408/429 —
    /// временные, повторим позже. Android держит тот же инвариант
    /// (`OutboxSyncer`: 401 → break).
    ///
    /// 403 — «вы не участник этой комнаты» (`roomForMember` на сервере), а не
    /// состояние токена: человека убрали из группы, пока расход лежал в
    /// очереди, и сам собой этот отказ не пройдёт. Раньше 403 считался
    /// временным и упирался в `break` — застревала не только эта запись, но и
    /// ВСЯ очередь целиком, включая расходы в других группах.
    private func isPermanentReject(_ status: Int) -> Bool {
        switch status {
        case 401, 408, 429: return false
        default: return (400..<500).contains(status)
        }
    }

    /// Оставляет в очереди только записи вошедшего пользователя (вызывается
    /// при входе): очередь пользователя A не должна уйти на сервер под токеном
    /// пользователя B. Записи без владельца (созданы прошлой версией
    /// приложения) достаются `userId` только если аккаунт не менялся.
    @MainActor
    func keepOwned(by userId: Int, inheritingOrphans: Bool) {
        let kept = entries.filter { entry in
            if let owner = entry.ownerUserId { return owner == userId }
            return inheritingOrphans
        }
        guard kept.count != entries.count else { return }
        entries = kept
        didLoad = true
        persist()
    }

    /// Полная очистка (logout).
    @MainActor
    func clear() {
        entries = []
        // Без didLoad = true логаут ушёл бы в retryLoadIfNeeded и вернул на диск
        // очередь ПРЕДЫДУЩЕГО аккаунта (первое чтение могло не пройти на
        // залоченном устройстве) — следующий вошедший отправил бы чужие расходы
        // в свои комнаты.
        didLoad = true
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
    func sync(api: OperationAPI) async -> Bool {
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
                if case .server(let status, _, _) = error, isPermanentReject(status) {
                    // Сервер отверг операцию — правка данных не поможет сама собой:
                    // помечаем failed, пользователь исправит/удалит. Идём к следующей.
                    markFailed(localId: entry.localId, message: error.localizedDescription)
                } else {
                    // Сеть/5xx: остаёмся pending, повторим при следующем триггере.
                    break
                }
            } catch is OutboxError {
                markFailed(localId: entry.localId, message: String(localized: "Некорректная локальная запись"))
            } catch {
                // Отмена задачи и прочее — прерываемся, записи остаются pending.
                break
            }
        }
        return syncedAny
    }

    /// Битая запись outbox (нет payload/targetOperationId для своего kind).
    private struct OutboxError: Error {}

    private func send(_ entry: OutboxEntry, api: OperationAPI) async throws {
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
                items: payload.items,
                clientOpId: entry.localId.uuidString
            )
        case .update:
            guard let payload = entry.payload, let operationId = entry.targetOperationId else {
                throw OutboxError()
            }
            // Версия НЕ отправляется намеренно: правку сделали офлайн, и
            // пересобрать её по свежим данным человек не может — он давно ушёл
            // с экрана. Отказ по конфликту здесь означал бы потерянную работу,
            // поэтому очередь пишет безусловно, как раньше.
            _ = try await api.updateOperation(
                roomId: entry.roomId,
                operationId: operationId,
                description: payload.description,
                sum: payload.sum,
                donorId: payload.donorId,
                split: payload.split,
                items: payload.items,
                version: nil
            )
        case .delete:
            guard let operationId = entry.targetOperationId else { throw OutboxError() }
            try await api.deleteOperation(roomId: entry.roomId, operationId: operationId)
        }
    }

    /// Повторная попытка прочитать очередь, если первая (на залоченном
    /// устройстве) провалилась. Записи с диска, которых нет в памяти,
    /// возвращаются в очередь — иначе они пропали бы при первой же перезаписи.
    private func retryLoadIfNeeded() {
        guard !didLoad else { return }
        guard let data = try? Data(contentsOf: fileURL) else { return }
        didLoad = true
        guard let loaded = try? decoder.decode([OutboxEntry].self, from: data) else { return }
        let known = Set(entries.map(\.localId))
        entries = loaded.filter { !known.contains($0.localId) } + entries
    }

    private func persist() {
        retryLoadIfNeeded()
        // Пока очередь не прочитана, перезаписывать файл нельзя: на диске могут
        // лежать неотправленные расходы, которых нет в памяти.
        guard didLoad else { return }
        // Снимок на вызывающем потоке, encode и запись — в фоне.
        let snapshot = entries
        io.async { [encoder, fileURL] in
            guard let data = try? encoder.encode(snapshot) else { return }
            let directory = fileURL.deletingLastPathComponent()
            try? FileManager.default.createDirectory(
                at: directory,
                withIntermediateDirectories: true
            )
            var directoryURL = directory
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try? directoryURL.setResourceValues(values)
            // Вне бэкапа: очередь содержит суммы и участников неотправленных
            // расходов. Защита — UnlessOpen, а не completeFileProtection:
            // очередь пишется и из фоновых синков на залоченном устройстве, где
            // строгий режим отдаёт ошибку записи, и запись молча терялась.
            try? data.write(to: fileURL, options: [.atomic, .completeFileProtectionUnlessOpen])
        }
    }

    /// Барьер для тестов: дожидается завершения всех фоновых записей.
    func waitForPendingWrites() {
        io.sync {}
    }
}
