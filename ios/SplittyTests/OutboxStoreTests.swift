import XCTest
@testable import Splitty

/// Тесты outbox офлайн-операций: add/update/remove, FIFO-порядок,
/// статус failed и персистентность в outbox.json.
@MainActor
final class OutboxStoreTests: XCTestCase {
    private var fileURL: URL!

    override func setUp() {
        super.setUp()
        fileURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("outbox-tests-\(UUID().uuidString)", isDirectory: true)
            .appendingPathComponent("outbox.json")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: fileURL.deletingLastPathComponent())
        super.tearDown()
    }

    private func makeStore() -> OutboxStore {
        OutboxStore(fileURL: fileURL)
    }

    private func payload(_ description: String = "Кофе", sum: Int = 300) -> OutboxPayload {
        OutboxPayload(
            description: description,
            sum: sum,
            donorId: 100,
            recipientIds: [100, 200],
            recipientSums: nil
        )
    }

    // MARK: add

    func testAddAppendsPendingCreateEntry() {
        let store = makeStore()
        let entry = store.add(roomId: "room1", payload: payload())

        XCTAssertEqual(store.entries.count, 1)
        XCTAssertEqual(entry.kind, .create)
        XCTAssertEqual(entry.status, .pending)
        XCTAssertEqual(entry.roomId, "room1")
        XCTAssertEqual(entry.payload, payload())
        XCTAssertNil(entry.targetOperationId)
    }

    func testAddPersistsToDisk() {
        let store = makeStore()
        let entry = store.add(roomId: "room1", payload: payload())
        // Запись файла — фоновая: дожидаемся её перед перечиткой.
        store.waitForPendingWrites()

        // Новый store с тем же файлом видит запись (даты — с точностью до секунды).
        let reloaded = makeStore()
        XCTAssertEqual(reloaded.entries.map(\.localId), [entry.localId])
        XCTAssertEqual(reloaded.entries.first?.payload, payload())
        XCTAssertEqual(reloaded.entries.first?.status, .pending)
    }

    // MARK: update

    func testUpdateReplacesPayloadAndResetsFailedToPending() {
        let store = makeStore()
        let entry = store.add(roomId: "room1", payload: payload())
        store.markFailed(localId: entry.localId, message: "донор должен быть участником комнаты")

        let fixed = OutboxPayload(
            description: "Кофе (испр.)",
            sum: 500,
            donorId: 200,
            recipientIds: nil,
            recipientSums: [RecipientSum(userId: 100, sum: 200), RecipientSum(userId: 200, sum: 300)]
        )
        store.update(localId: entry.localId, payload: fixed)

        let updated = store.entries.first
        XCTAssertEqual(updated?.payload, fixed)
        // Исправленная failed-запись снова pending — уйдёт при следующем синке.
        XCTAssertEqual(updated?.status, .pending)
        // localId (идемпотентный ключ) и createdAt не меняются.
        XCTAssertEqual(updated?.localId, entry.localId)
        XCTAssertEqual(updated?.createdAt, entry.createdAt)
    }

    func testUpdateUnknownIdIsNoOp() {
        let store = makeStore()
        store.add(roomId: "room1", payload: payload())
        store.update(localId: UUID(), payload: payload("Другое"))
        XCTAssertEqual(store.entries.first?.payload?.description, "Кофе")
    }

    // MARK: remove

    func testRemoveDeletesEntryAndPersists() {
        let store = makeStore()
        let first = store.add(roomId: "room1", payload: payload("Первый"))
        let second = store.add(roomId: "room1", payload: payload("Второй"))

        store.remove(localId: first.localId)

        XCTAssertEqual(store.entries.map(\.localId), [second.localId])
        store.waitForPendingWrites()
        XCTAssertEqual(makeStore().entries.map(\.localId), [second.localId])
    }

    // MARK: FIFO

    func testEntriesKeepFifoOrder() {
        let store = makeStore()
        let a = store.add(roomId: "room1", payload: payload("А"))
        let b = store.add(roomId: "room2", payload: payload("Б"))
        let c = store.add(roomId: "room1", payload: payload("В"))

        XCTAssertEqual(store.entries.map(\.localId), [a.localId, b.localId, c.localId])
        // Порядок сохраняется после перезагрузки с диска.
        store.waitForPendingWrites()
        XCTAssertEqual(makeStore().entries.map(\.localId), [a.localId, b.localId, c.localId])
        // Правка НЕ двигает запись в очереди (FIFO по createdAt-порядку).
        store.update(localId: a.localId, payload: payload("А2"))
        XCTAssertEqual(store.entries.map(\.localId), [a.localId, b.localId, c.localId])
    }

    func testEntriesByRoomFiltersAndKeepsOrder() {
        let store = makeStore()
        let a = store.add(roomId: "room1", payload: payload("А"))
        store.add(roomId: "room2", payload: payload("Б"))
        let c = store.add(roomId: "room1", payload: payload("В"))

        XCTAssertEqual(store.entries(roomId: "room1").map(\.localId), [a.localId, c.localId])
        XCTAssertTrue(store.entries(roomId: "room3").isEmpty)
    }

    // MARK: failed

    func testMarkFailedStoresMessageAndPersists() {
        let store = makeStore()
        let entry = store.add(roomId: "room1", payload: payload())

        store.markFailed(localId: entry.localId, message: "сумма должна быть не меньше 1")

        XCTAssertEqual(
            store.entries.first?.status,
            .failed(message: "сумма должна быть не меньше 1")
        )
        XCTAssertTrue(store.entries.first?.isFailed ?? false)
        XCTAssertEqual(
            store.entries.first?.status.failureMessage,
            "сумма должна быть не меньше 1"
        )
        // Статус переживает перезапуск.
        store.waitForPendingWrites()
        XCTAssertEqual(
            makeStore().entries.first?.status,
            .failed(message: "сумма должна быть не меньше 1")
        )
    }

    // MARK: clear

    func testClearRemovesEverythingAndPersists() {
        let store = makeStore()
        store.add(roomId: "room1", payload: payload())
        store.add(roomId: "room2", payload: payload())

        store.clear()

        XCTAssertTrue(store.entries.isEmpty)
        store.waitForPendingWrites()
        XCTAssertTrue(makeStore().entries.isEmpty)
    }

    // MARK: загрузка с битым файлом

    func testCorruptedFileLoadsAsEmptyOutbox() throws {
        try FileManager.default.createDirectory(
            at: fileURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        try Data("не json".utf8).write(to: fileURL)
        XCTAssertTrue(makeStore().entries.isEmpty)
    }

    /// Битый файл не должен навсегда отключать запись: раньше didLoad оставался
    /// false, persist молчал, и офлайн-очередь жила только в памяти — пропадая
    /// при закрытии приложения.
    func testCorruptedFileDoesNotDisablePersistence() throws {
        try FileManager.default.createDirectory(
            at: fileURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        try Data("не json".utf8).write(to: fileURL)

        let store = makeStore()
        store.add(roomId: "room1", payload: payload("Такси"))
        store.waitForPendingWrites()

        XCTAssertEqual(makeStore().entries.count, 1, "запись после битого файла не сохранилась")
    }

    /// Неразобранный файл не стирается: в нём могут лежать неотправленные
    /// расходы, а причина сбоя — от обрыва записи до смены модели. Файл уносится
    /// в сторону, откуда его содержимое ещё можно достать.
    func testUnreadableFileIsSetAsideNotErased() throws {
        let dir = fileURL.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try Data("не json".utf8).write(to: fileURL)

        _ = makeStore()

        let leftovers = try FileManager.default.contentsOfDirectory(atPath: dir.path)
            .filter { $0.hasPrefix("outbox-unreadable-") }
        XCTAssertEqual(leftovers.count, 1, "неразобранный файл исчез вместе с содержимым")
        let saved = try String(contentsOf: dir.appendingPathComponent(leftovers[0]), encoding: .utf8)
        XCTAssertEqual(saved, "не json")
    }

    /// Файл со старым форматом (голый массив, без версии схемы) обязан
    /// читаться: он лежит на устройствах у всех, кто ставил прошлые сборки.
    func testLegacyFileWithoutSchemaVersionStillLoads() throws {
        let seeded = makeStore()
        seeded.add(roomId: "room1", payload: payload("Такси"))
        seeded.waitForPendingWrites()

        // Разворачиваем файл обратно в формат прошлых сборок: голый массив.
        let data = try Data(contentsOf: fileURL)
        let object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let entries = try XCTUnwrap(object["entries"])
        try JSONSerialization.data(withJSONObject: entries).write(to: fileURL)

        let store = makeStore()
        XCTAssertEqual(store.entries.count, 1, "очередь прошлой сборки не прочиталась")
        XCTAssertEqual(store.entries.first?.payload?.description, "Такси")
    }

    /// Незнакомое поле в файле не должно терять записи: так выглядит откат на
    /// прошлую версию приложения после того, как новая дописала своё поле.
    func testUnknownFieldDoesNotDropEntries() throws {
        let seeded = makeStore()
        seeded.add(roomId: "room1", payload: payload("Ужин", sum: 900))
        seeded.waitForPendingWrites()

        // Файл «из будущего»: версия схемы выше и в записи есть лишнее поле.
        let data = try Data(contentsOf: fileURL)
        var object = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        var entries = try XCTUnwrap(object["entries"] as? [[String: Any]])
        entries[0]["somethingNew"] = true
        object["entries"] = entries
        object["schemaVersion"] = 99
        try JSONSerialization.data(withJSONObject: object).write(to: fileURL)

        let store = makeStore()
        XCTAssertEqual(store.entries.count, 1, "запись потерялась из-за незнакомого поля")
        XCTAssertEqual(store.entries.first?.payload?.description, "Ужин")
    }

    /// Логаут не должен вернуть на диск очередь ПРЕДЫДУЩЕГО аккаунта: если
    /// первое чтение провалилось (залоченное устройство), retryLoadIfNeeded
    /// внутри persist поднимал её обратно, и следующий вошедший отправлял чужие
    /// расходы в свои комнаты.
    func testClearWipesFileEvenWhenFirstLoadFailed() throws {
        let seeded = makeStore()
        seeded.add(roomId: "room1", payload: payload("Чужой расход"))
        seeded.waitForPendingWrites()

        // Файл на месте, но нечитаем — didLoad остаётся false, как под
        // completeFileProtection на залоченном устройстве.
        let fm = FileManager.default
        try fm.setAttributes([.posixPermissions: 0], ofItemAtPath: fileURL.path)
        let store = makeStore()
        try fm.setAttributes([.posixPermissions: 0o644], ofItemAtPath: fileURL.path)

        store.clear()
        store.waitForPendingWrites()

        XCTAssertTrue(
            makeStore().entries.isEmpty,
            "логаут оставил очередь прошлого аккаунта"
        )
    }
}

// MARK: - 403 не должен запирать очередь

/// Фейк, отвечающий заданным статусом на конкретную комнату.
private final class RejectingAPI: OperationAPI {
    let failingRoomId: String
    let status: Int
    private(set) var attemptedRooms: [String] = []

    init(failingRoomId: String, status: Int) {
        self.failingRoomId = failingRoomId
        self.status = status
    }

    func addOperation(
        roomId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]?,
        clientOpId: String?
    ) async throws -> Splitty.Operation {
        attemptedRooms.append(roomId)
        if roomId == failingRoomId {
            throw APIError.server(status: status, code: "forbidden", message: "вы не участник этой комнаты")
        }
        return Splitty.Operation(
            id: clientOpId ?? "op",
            description: description,
            sum: sum,
            isDebtRepayment: false,
            donor: User(id: donorId, username: nil, displayName: "u\(donorId)"),
            recipients: [],
            splitType: .byExactAmount,
            createdAt: Date(timeIntervalSince1970: 1_780_000_000),
            files: nil
        )
    }

    func updateOperation(
        roomId: String, operationId: String, description: String, sum: Int,
        donorId: Int, split: ExpenseSplit, items: [OperationItem]?, version: Int?
    ) async throws -> Splitty.Operation {
        throw APIError.server(status: 500, code: "internal", message: "")
    }

    func deleteOperation(roomId: String, operationId: String) async throws {}
}

final class OutboxForbiddenTests: XCTestCase {

    /// Человека убрали из группы, пока его расход лежал в очереди. Такой отказ
    /// сам собой не пройдёт, и раньше он останавливал ВЕСЬ синк: следом
    /// застревали расходы в других группах.
    @MainActor
    func testForbiddenEntryDoesNotBlockTheRestOfTheQueue() async throws {
        let file = FileManager.default.temporaryDirectory
            .appendingPathComponent("outbox-403-\(UUID().uuidString).json")
        defer { try? FileManager.default.removeItem(at: file) }

        let store = OutboxStore(fileURL: file)
        _ = store.add(
            roomId: "closed",
            payload: OutboxPayload(description: "Такси", sum: 100, donorId: 1, recipientIds: [1, 2])
        )
        _ = store.add(
            roomId: "open",
            payload: OutboxPayload(description: "Ужин", sum: 200, donorId: 1, recipientIds: [1, 2])
        )

        let api = RejectingAPI(failingRoomId: "closed", status: 403)
        _ = await store.sync(api: api)

        XCTAssertTrue(api.attemptedRooms.contains("open"), "синк остановился на 403 и не дошёл до других групп")
        XCTAssertEqual(store.entries.count, 1, "отвергнутая запись должна остаться помеченной, а прошедшая — уйти")
        XCTAssertEqual(store.entries.first?.roomId, "closed")
        if case .failed = store.entries.first?.status {} else {
            XCTFail("отвергнутая запись должна быть помечена failed, а не висеть pending")
        }
    }
}
