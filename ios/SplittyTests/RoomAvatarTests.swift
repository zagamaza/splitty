import XCTest
@testable import Splitty

/// Фото группы: комната знает про свою картинку, а загрузка уходит туда, куда
/// договорились с сервером.
final class RoomAvatarTests: XCTestCase {
    private var client: APIClient!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "t",
            urlSession: URLSession(configuration: configuration)
        )
    }

    override func tearDown() {
        client = nil
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        super.tearDown()
    }

    /// Ключа может не быть вовсе (omitempty на сервере, и старые ответы в
    /// офлайн-кеше) — разбор обязан это пережить, а не уронить весь список.
    func testRoomSummaryDecodesWithoutAvatar() throws {
        let json = """
        {"id":"1","name":"Стамбул","createdAt":"2026-08-17T10:00:00Z","isArchived":false,
         "members":[],"memberCount":0,"currency":"RUB","totalSpent":0,"myBalance":0}
        """
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let room = try decoder.decode(RoomSummary.self, from: Data(json.utf8))
        XCTAssertNil(room.avatarFileId)
    }

    func testRoomSummaryDecodesAvatar() throws {
        let json = """
        {"id":"1","name":"Стамбул","createdAt":"2026-08-17T10:00:00Z","isArchived":false,
         "members":[],"memberCount":0,"currency":"RUB","totalSpent":0,"myBalance":0,
         "avatarFileId":"65a0000000000000000000ff"}
        """
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let room = try decoder.decode(RoomSummary.self, from: Data(json.utf8))
        XCTAssertEqual(room.avatarFileId, "65a0000000000000000000ff")
    }

    /// RoomDetail разбирается РУЧНЫМ init(from:), где поля перечислены по
    /// одному. Строку про avatarFileId там забыли, и фото группы не
    /// показывалось на экране настроек — при том что в списке тус оно было
    /// (там RoomSummary со своим декодером), а сервер поле исправно отдавал.
    /// Компилятор промолчал: у поля есть дефолт nil.
    func testRoomDetailDecodesAvatar() throws {
        let room = try Self.decodeDetail(avatar: "\"avatarFileId\":\"65a0000000000000000000ff\",")
        XCTAssertEqual(room.avatarFileId, "65a0000000000000000000ff")
    }

    /// Ключа может не быть (omitempty) — разбор обязан это пережить.
    func testRoomDetailDecodesWithoutAvatar() throws {
        let room = try Self.decodeDetail(avatar: "")
        XCTAssertNil(room.avatarFileId)
    }

    private static func decodeDetail(avatar: String) throws -> RoomDetail {
        let json = """
        {"id":"1","name":"Стамбул","createdAt":"2026-08-17T10:00:00Z","isArchived":false,
         "members":[],"currency":"RUB",\(avatar)"totalSpent":0,"mySpent":0,"myBalance":0,
         "debts":[],"operations":[]}
        """
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(RoomDetail.self, from: Data(json.utf8))
    }

    /// Загрузка идёт PUT-ом на адрес комнаты, тело — multipart с полем `image`.
    /// Сервер отличает поле по имени, поэтому имя проверяем явно.
    func testSetRoomAvatarSendsMultipartPut() async throws {
        StubURLProtocol.handler = { _ in (200, Data(#"{"avatarFileId":"file-1"}"#.utf8)) }

        let fileId = try await client.setRoomAvatar(roomId: "room-1", image: Data([0xFF, 0xD8, 0xFF]))
        XCTAssertEqual(fileId, "file-1")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "PUT")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/avatar")
        let contentType = try XCTUnwrap(request.value(forHTTPHeaderField: "Content-Type"))
        XCTAssertTrue(contentType.hasPrefix("multipart/form-data; boundary="), contentType)

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let text = String(decoding: body, as: UTF8.self)
        XCTAssertTrue(text.contains(#"name="image""#), "сервер ищет часть с именем image")
        XCTAssertTrue(text.contains("Content-Type: image/jpeg"))
    }

    func testDeleteRoomAvatarSendsDelete() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.deleteRoomAvatar(roomId: "room-1")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/avatar")
    }

    /// Распознавание расхода собирает тело тем же кодом, что и ава: проверяем,
    /// что после объединения multipart-сборки части не потерялись.
    func testParseOperationStillSendsAllParts() async throws {
        StubURLProtocol.handler = { _ in (200, Data(#"{"items":[],"unknownNames":[]}"#.utf8)) }

        _ = try? await client.parseOperation(
            roomId: "room-1",
            audio: Data([1, 2, 3]),
            image: Data([4, 5, 6]),
            text: "пицца"
        )

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let text = String(decoding: body, as: UTF8.self)
        XCTAssertTrue(text.contains(#"name="audio""#))
        XCTAssertTrue(text.contains(#"name="image""#))
        XCTAssertTrue(text.contains(#"name="text""#))
        XCTAssertEqual(StubURLProtocol.lastRequest?.url?.path, "/api/v1/rooms/room-1/operations/parse")
    }
}
