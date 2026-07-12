import XCTest
@testable import Splitty

/// Тесты файлового read-кеша офлайн-режима: запись/чтение/перезапись,
/// промахи, очистка и безопасные имена файлов.
/// `OfflineStore` — актор (I/O вне main), поэтому тесты async/await.
final class OfflineStoreTests: XCTestCase {
    private var directory: URL!
    private var store: OfflineStore!

    override func setUp() {
        super.setUp()
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("offline-store-tests-\(UUID().uuidString)", isDirectory: true)
        store = OfflineStore(directory: directory)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: directory)
        super.tearDown()
    }

    private func room(_ name: String, balance: Int) -> RoomSummary {
        RoomSummary(
            id: "65a0000000000000000000ff",
            name: name,
            createdAt: Date(timeIntervalSince1970: 1_750_000_000),
            isArchived: false,
            members: [User(id: 100, username: "zagir", displayName: "Загир")],
            memberCount: 1,
            currency: "RUB",
            totalSpent: 1200,
            myBalance: balance
        )
    }

    // MARK: запись/чтение

    func testWriteThenReadRoundTrip() async {
        let rooms = [room("Стамбул", balance: 500)]
        await store.write(rooms, key: "rooms-archived-false")

        let read: [RoomSummary]? = await store.read(key: "rooms-archived-false")
        XCTAssertEqual(read, rooms)
        // Дата переживает round-trip (ISO 8601, секундная точность).
        XCTAssertEqual(read?.first?.createdAt, Date(timeIntervalSince1970: 1_750_000_000))
    }

    func testReadMissingKeyReturnsNil() async {
        let read: [RoomSummary]? = await store.read(key: "нет-такого-ключа")
        XCTAssertNil(read)
    }

    func testReadWrongTypeReturnsNil() async {
        await store.write(["строка"], key: "key")
        let read: [RoomSummary]? = await store.read(key: "key")
        XCTAssertNil(read)
    }

    func testCorruptedFileReturnsNil() async throws {
        await store.write([room("Стамбул", balance: 0)], key: "key")
        let file = directory.appendingPathComponent(OfflineStore.fileName(for: "key"))
        try Data("{битый json".utf8).write(to: file)

        let read: [RoomSummary]? = await store.read(key: "key")
        XCTAssertNil(read)
    }

    // MARK: перезапись

    func testWriteOverwritesPreviousValue() async {
        await store.write([room("Стамбул", balance: 500)], key: "rooms")
        await store.write([room("Бали", balance: -200)], key: "rooms")

        let read: [RoomSummary]? = await store.read(key: "rooms")
        XCTAssertEqual(read?.count, 1)
        XCTAssertEqual(read?.first?.name, "Бали")
        XCTAssertEqual(read?.first?.myBalance, -200)
    }

    func testKeysAreIndependent() async {
        await store.write([room("Стамбул", balance: 1)], key: "rooms-archived-false")
        await store.write([room("Архивная", balance: 2)], key: "rooms-archived-true")

        let active: [RoomSummary]? = await store.read(key: "rooms-archived-false")
        let archived: [RoomSummary]? = await store.read(key: "rooms-archived-true")
        XCTAssertEqual(active?.first?.name, "Стамбул")
        XCTAssertEqual(archived?.first?.name, "Архивная")
    }

    // MARK: очистка

    func testRemoveAllDropsEverything() async {
        await store.write([room("Стамбул", balance: 500)], key: "rooms")
        await store.write([room("Бали", balance: 1)], key: "room-65a")

        await store.removeAll()

        let rooms: [RoomSummary]? = await store.read(key: "rooms")
        let detail: [RoomSummary]? = await store.read(key: "room-65a")
        XCTAssertNil(rooms)
        XCTAssertNil(detail)
        // После очистки кеш снова работоспособен.
        await store.write([room("Новая", balance: 0)], key: "rooms")
        let again: [RoomSummary]? = await store.read(key: "rooms")
        XCTAssertEqual(again?.first?.name, "Новая")
    }

    // MARK: имена файлов

    func testFileNameKeepsSafeCharactersAndJsonExtension() {
        XCTAssertEqual(
            OfflineStore.fileName(for: "rooms-archived-false"),
            "rooms-archived-false.json"
        )
        XCTAssertEqual(
            OfflineStore.fileName(for: "room-65a0f"),
            "room-65a0f.json"
        )
    }

    func testFileNameSanitizesUnsafeCharacters() {
        let name = OfflineStore.fileName(for: "a/b?c=d&e тест")
        XCTAssertFalse(name.contains("/"))
        XCTAssertFalse(name.contains("?"))
        XCTAssertFalse(name.contains("&"))
        XCTAssertFalse(name.contains(" "))
        XCTAssertTrue(name.hasSuffix(".json"))
    }
}
