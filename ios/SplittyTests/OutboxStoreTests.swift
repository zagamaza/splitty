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
}
