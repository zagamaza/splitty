import XCTest
@testable import Splitty

/// Архивация из списка тус (долгое зажатие на карточке) ходит теми же
/// маршрутами, что и переключатель на экране настроек группы. Тестов на них
/// не было вовсе: опечатка в пути или замена POST на PUT молча ломала бы
/// единственный способ убрать тусу из списка.
final class ArchiveRoomTests: XCTestCase {
    private var client: APIClient!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil

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
        super.tearDown()
    }

    func testArchiveRoomSendsPost() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.archiveRoom(id: "room-1")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/archive")
    }

    func testUnarchiveRoomSendsPost() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.unarchiveRoom(id: "room-1")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/unarchive")
    }

    /// Ошибку сервера метод обязан пробросить, а не проглотить: список тус
    /// иначе показал бы группу «убранной», хотя на сервере она осталась.
    func testArchiveRoomPropagatesServerError() async {
        StubURLProtocol.handler = { _ in (403, Data(#"{"error":"forbidden"}"#.utf8)) }

        do {
            try await client.archiveRoom(id: "room-1")
            XCTFail("403 обязан долететь до вызывающего")
        } catch {
            // ожидаемо
        }
    }
}
