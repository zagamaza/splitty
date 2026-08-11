import XCTest
@testable import Splitty

/// Приглашение друга, выход и удаление участника: запросы к API, тексты
/// отказов 409 и итог множественного приглашения.
final class GroupMembershipAPITests: XCTestCase {
    private var client: APIClient!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "jwt",
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

    func testAddMemberSendsUserIdAndParsesAddedStatus() async throws {
        StubURLProtocol.handler = { _ in (200, Data(#"{"status":"added"}"#.utf8)) }

        let status = try await client.addMember(roomId: "room-1", userId: 88)

        XCTAssertEqual(status, .added)
        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/members")
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: try XCTUnwrap(StubURLProtocol.lastBody)) as? [String: Any]
        )
        XCTAssertEqual(json["userId"] as? Int, 88)
    }

    /// 202 — человек уже выходил из группы, и вернуть его можно только с его
    /// согласия. Для экрана это НЕ то же самое, что «добавлен».
    func testAddMemberParsesPendingStatus() async throws {
        StubURLProtocol.handler = { _ in (202, Data(#"{"status":"pending"}"#.utf8)) }

        let status = try await client.addMember(roomId: "room-1", userId: 88)

        XCTAssertEqual(status, .pending)
    }

    func testLeaveRoomUsesDeleteMembersMe() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.leaveRoom(roomId: "room-1")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/members/me")
    }

    func testRemoveMemberUsesDeleteWithUserId() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.removeMember(roomId: "room-1", userId: 88)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/api/v1/rooms/room-1/members/88")
    }

    /// 409 не должен читаться как мёртвая сессия — иначе отказ выхода
    /// выкидывал бы человека на экран входа.
    func testLeaveConflictIsNotUnauthorized() async {
        StubURLProtocol.handler = { _ in
            (409, Data(#"{"error":{"code":"has_operations","message":"конфликт"}}"#.utf8))
        }

        do {
            try await client.leaveRoom(roomId: "room-1")
            XCTFail("ожидали APIError.server(409)")
        } catch let error as APIError {
            XCTAssertFalse(error.isUnauthorized)
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
    }
}

/// Тексты отказов 409. Свои, а не серверные: отказ обязан объяснять путь
/// наружу, иначе человек упирается в глухое «конфликт».
final class LeaveErrorTextTests: XCTestCase {
    private let hasOperations = APIError.server(
        status: 409, code: "has_operations", message: "любой серверный текст"
    )

    func testHasOperationsExplainsWayOutForSelf() {
        let text = leaveErrorText(hasOperations)
        XCTAssertTrue(text.contains("Уберите себя"))
        XCTAssertTrue(text.contains("смените плательщика"))
        // Ни кода, ни серверного текста в алерте быть не должно.
        XCTAssertFalse(text.contains("has_operations"))
        XCTAssertFalse(text.contains("любой серверный текст"))
    }

    func testHasOperationsForOtherMemberSpeaksAboutHim() {
        let text = leaveErrorText(hasOperations, isSelf: false)
        XCTAssertTrue(text.contains("На участнике"))
        XCTAssertFalse(text.contains("Уберите себя"))
    }

    func testLastMemberSuggestsArchive() {
        let text = leaveErrorText(APIError.server(status: 409, code: "last_member", message: ""))
        XCTAssertEqual(text, "Вы последний участник. Заархивируйте группу, если она больше не нужна")
    }

    /// Незнакомый сбой уходит в общий человеческий текст, а не в «Ошибка 500».
    func testOtherErrorsFallBackToCommonText() {
        let forbidden = APIError.server(status: 403, code: "forbidden", message: "нет доступа")
        XCTAssertEqual(leaveErrorText(forbidden), humanErrorText(forbidden))

        let transport = APIError.transport(URLError(.notConnectedToInternet))
        XCTAssertEqual(leaveErrorText(transport), humanErrorText(transport))
    }
}

/// Итог множественного приглашения. Раньше любой сбой считался общим
/// провалом: показывалась первая ошибка, а удавшиеся приглашения не доезжали
/// до списка групп.
final class InviteResultTextTests: XCTestCase {
    func testAllAddedNeedsNoExplanation() {
        XCTAssertNil(InviteFriendView.resultText(added: ["Аня", "Пётр"], pending: [], failed: []))
    }

    func testPendingIsReportedSeparatelyFromAdded() throws {
        let text = try XCTUnwrap(
            InviteFriendView.resultText(added: ["Аня"], pending: ["Ольга"], failed: [])
        )
        XCTAssertTrue(text.contains("Добавлен(а) в группу: Аня"))
        XCTAssertTrue(text.contains("ждём согласия: Ольга"))
    }

    func testPartialFailureNamesWhoWasNotInvited() throws {
        let text = try XCTUnwrap(
            InviteFriendView.resultText(added: ["Аня"], pending: [], failed: ["Иван"])
        )
        XCTAssertTrue(text.contains("Добавлен(а) в группу: Аня"))
        XCTAssertTrue(text.contains("Не удалось пригласить: Иван"))
    }
}

/// Удалённый аккаунт остаётся в снимках комнат (анонимизированным) и попадает
/// в `/friends`, но приглашение ему вернуло бы 404 — признак приходит полем
/// `deleted`, которого в большинстве ответов нет вовсе.
final class DeletedUserDecodingTests: XCTestCase {
    private func decode(_ json: String) throws -> User {
        try JSONDecoder().decode(User.self, from: Data(json.utf8))
    }

    func testMissingFlagDecodesAsAlive() throws {
        let user = try decode(#"{"id":77,"username":null,"displayName":"Аня"}"#)
        XCTAssertFalse(user.deleted)
    }

    func testFlagDecodes() throws {
        let user = try decode(#"{"id":88,"username":"","displayName":"Удалённый пользователь","deleted":true}"#)
        XCTAssertTrue(user.deleted)
    }

    /// Признак обязан переживать офлайн-кеш: он пишется тем же кодеком.
    func testRoundTripKeepsFlag() throws {
        let user = User(id: 88, username: nil, displayName: "Удалённый пользователь", deleted: true)
        let data = try JSONEncoder().encode(user)
        XCTAssertTrue(try JSONDecoder().decode(User.self, from: data).deleted)
    }
}
