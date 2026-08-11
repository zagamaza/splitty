import XCTest
@testable import Splitty

/// Разбор payload push-уведомления.
///
/// Тесты существуют потому, что до них «переход по пушу» был фикцией: делегат
/// читал ключ `room_id`, которого бэкенд не шлёт (там camelCase `roomId`), а
/// подписчиков на событие не было ни одного. Ошибка жила незаметно, потому что
/// проверять было нечего.
final class PushRouteTests: XCTestCase {

    /// Пуш про расход несёт и комнату, и операцию — ведёт в карточку.
    /// Раньше `operationId` из payload просто выбрасывался, и человек попадал
    /// в список операций искать тот расход, о котором ему только что сказали.
    func testOperationPushOpensOperationCard() {
        let route = PushRoute(userInfo: [
            "channel": "operations",
            "roomId": "68f2a1c4d9",
            "operationId": "6a7b339f63a4ee45a2aed6db",
            "type": "operation",
        ])
        XCTAssertEqual(route, .operation(roomId: "68f2a1c4d9", operationId: "6a7b339f63a4ee45a2aed6db"))
    }

    /// Пустая строка — не операция: payload FCM состоит только из строк, и
    /// «operationId»: "" уводило бы в карточку с пустым id (404 на ровном месте).
    func testEmptyOperationIdFallsBackToRoom() {
        let route = PushRoute(userInfo: [
            "channel": "operations",
            "roomId": "room-1",
            "operationId": "",
            "type": "operation",
        ])
        XCTAssertEqual(route, .room(id: "room-1"))
    }

    func testDebtPushOpensRoom() {
        let route = PushRoute(userInfo: [
            "channel": "debts",
            "roomId": "room-1",
            "type": "debt",
        ])
        XCTAssertEqual(route, .room(id: "room-1"))
    }

    /// Приглашение ведёт в раздел, а НЕ в комнату: у человека с ожидающим
    /// приглашением доступа к ней ещё нет, и переход упёрся бы в 403.
    func testInvitePushOpensNotificationsNotRoom() {
        let route = PushRoute(userInfo: [
            "channel": "invites",
            "roomId": "room-1",
            "type": "invite",
        ])
        XCTAssertEqual(route, .notifications)
    }

    /// Правило приглашения сильнее операции: даже приди в таком payload
    /// operationId, в комнату приглашённого пускать нельзя.
    func testInviteWinsOverOperationId() {
        let route = PushRoute(userInfo: [
            "channel": "invites",
            "roomId": "room-1",
            "operationId": "op-1",
            "type": "invite",
        ])
        XCTAssertEqual(route, .notifications)
    }

    /// Прежний (неверный) ключ не должен внезапно снова заработать: если
    /// бэкенд когда-нибудь начнёт слать snake_case, это обязано быть заметно.
    func testLegacySnakeCaseKeyIsNotAccepted() {
        let route = PushRoute(userInfo: ["room_id": "room-1"])
        XCTAssertNil(route)
    }

    func testEmptyRoomIdRejected() {
        XCTAssertNil(PushRoute(userInfo: ["roomId": ""]))
    }

    func testUnrelatedPayloadRejected() {
        XCTAssertNil(PushRoute(userInfo: ["foo": "bar"]))
        XCTAssertNil(PushRoute(userInfo: [:]))
    }
}
