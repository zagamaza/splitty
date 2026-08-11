import XCTest

@testable import Splitty

/// Счётчик непрочитанного на карточке группы: разбор `unreadCount`, текст
/// бейджа и отметка группы прочитанной при её открытии.
///
/// Бейдж вкладки сообщает, ЧТО что-то случилось, счётчик карточки — ГДЕ.
/// Гаснет он открытием ИМЕННО этой группы: гаси его заход в «Уведомления»,
/// счётчиков почти никто не увидел бы — туда человека и ведёт бейдж.
private let roomJSON = #"""
{
  "id": "68a1b2c3d4e5f60718293a4b", "name": "Квартира",
  "createdAt": "2026-07-05T12:00:00Z", "isArchived": false,
  "members": [], "currency": "RUB", "totalSpent": 1000, "mySpent": 500,
  "myBalance": 0, "debts": [], "operations": [],
  "seenThrough": "2026-07-30T12:00:00Z"
}
"""#

/// «2026-07-30T12:00:00Z» — тот же момент, что в `seenThrough` ответа.
private let roomSeenThrough = Date(timeIntervalSince1970: 1_785_412_800)

/// Разбирает и с долями секунды, и без них — клиент вправе прислать любой из них.
private func parseSeenThrough(_ string: String) -> Date? {
    let plain = ISO8601DateFormatter()
    plain.formatOptions = [.withInternetDateTime]
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return plain.date(from: string) ?? fractional.date(from: string)
}

final class GroupUnreadModelTests: XCTestCase {
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private func summary(_ json: String) throws -> RoomSummary {
        try decoder.decode(RoomSummary.self, from: Data(json.utf8))
    }

    func testSummaryDecodesUnreadCount() throws {
        let room = try summary("""
        {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,"myBalance":0,
         "unreadCount":3}
        """)
        XCTAssertEqual(room.unreadCount, 3)
    }

    /// Ключа нет у прочитанной группы (omitempty на сервере) и у списков из
    /// офлайн-кеша прошлой версии: keyNotFound уронил бы весь список групп.
    func testSummaryDefaultsUnreadToZeroWhenKeyAbsent() throws {
        let room = try summary("""
        {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,"myBalance":0}
        """)
        XCTAssertEqual(room.unreadCount, 0)
    }

    /// Комната из кеша прошлой версии приходит без `seenThrough` — отмечать
    /// тогда нечем, но разбор экрана падать не должен.
    func testDetailSeenThroughIsOptional() throws {
        let withMark = try decoder.decode(RoomDetail.self, from: Data(roomJSON.utf8))
        XCTAssertEqual(withMark.seenThrough, roomSeenThrough)

        let withoutMark = try decoder.decode(RoomDetail.self, from: Data("""
        {"id":"r1","name":"Квартира","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"currency":"RUB","totalSpent":0,"mySpent":0,"myBalance":0,
         "debts":[],"operations":[]}
        """.utf8))
        XCTAssertNil(withoutMark.seenThrough)
    }

    /// Правило «99+» одно на бейдж вкладки и на счётчик карточки: сервер шлёт
    /// 100 как маркер «больше 99», и рисовать его числом нельзя.
    func testBadgeLabelMatchesTabRule() {
        XCTAssertNil(MainTabView.badgeLabel(for: 0))
        XCTAssertEqual(MainTabView.badgeLabel(for: 7), "7")
        XCTAssertEqual(MainTabView.badgeLabel(for: 99), "99")
        XCTAssertEqual(MainTabView.badgeLabel(for: 100), "99+")
    }
}

/// Отметка группы прочитанной: контракт запроса и поведение экрана группы.
final class GroupSeenMarkTests: XCTestCase {
    private var directory: URL!
    private var stubSession: URLSession!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil

        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("group-unread-tests-\(UUID().uuidString)", isDirectory: true)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        stubSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: directory)
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        super.tearDown()
    }

    @MainActor
    private func makeRepo() -> DataRepo {
        DataRepo(
            api: APIClient(
                baseURL: URL(string: "https://api.example.test"),
                token: "jwt",
                urlSession: stubSession
            ),
            cache: OfflineStore(directory: directory),
            scope: "test"
        )
    }

    /// Дата уходит СТРОКОЙ RFC3339: числу (`.deferredToDate`) сервер отвечает
    /// 400, и счётчик группы не гаснет ни разу.
    func testMarkRoomSeenSendsRFC3339String() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }
        let client = await APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "jwt",
            urlSession: stubSession
        )

        try await client.markRoomSeen(roomId: "room-1", through: roomSeenThrough)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/notifications-seen")
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: try XCTUnwrap(StubURLProtocol.lastBody)) as? [String: Any]
        )
        XCTAssertNil(json["seenThrough"] as? NSNumber)
        let raw = try XCTUnwrap(json["seenThrough"] as? String)
        XCTAssertEqual(parseSeenThrough(raw), roomSeenThrough)
    }

    /// Открыли группу — она прочитана, и отмечается серверным `seenThrough`
    /// из ответа, а не локальным «сейчас»: иначе погас бы и расход, пришедший
    /// между ответом и отметкой.
    @MainActor
    func testOpeningRoomMarksItSeenWithServerTime() async throws {
        StubURLProtocol.handler = { request in
            if request.httpMethod == "GET" { return (200, Data(roomJSON.utf8)) }
            return (204, Data())
        }

        let model = GroupDetailViewModel()
        await model.load(repo: makeRepo(), roomId: "68a1b2c3d4e5f60718293a4b")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(
            request.url?.path, "/api/v1/rooms/68a1b2c3d4e5f60718293a4b/notifications-seen")
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: try XCTUnwrap(StubURLProtocol.lastBody)) as? [String: Any]
        )
        XCTAssertEqual(parseSeenThrough(try XCTUnwrap(json["seenThrough"] as? String)), roomSeenThrough)
    }

    /// Офлайн-показ из кеша — не «человек прочитал»: `seenThrough` там от
    /// прошлого визита, а запрос всё равно не уйдёт.
    @MainActor
    func testCachedRoomIsNotMarkedSeen() async {
        StubURLProtocol.handler = { request in
            if request.httpMethod == "GET" { return (200, Data(roomJSON.utf8)) }
            return (204, Data())
        }
        let repo = makeRepo()
        let model = GroupDetailViewModel()
        await model.load(repo: repo, roomId: "68a1b2c3d4e5f60718293a4b")

        // сеть пропала — комната приезжает из кеша
        StubURLProtocol.handler = { _ in (503, Data(#"{"error":{"code":"internal","message":"нет"}}"#.utf8)) }
        StubURLProtocol.lastRequest = nil
        await model.load(repo: repo, roomId: "68a1b2c3d4e5f60718293a4b")

        XCTAssertNotEqual(
            StubURLProtocol.lastRequest?.url?.path,
            "/api/v1/rooms/68a1b2c3d4e5f60718293a4b/notifications-seen")
    }
}
