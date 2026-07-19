import XCTest

@testable import Splitty

/// `debtsUnavailable` — легаси-комнаты бота, где доли не сходятся. Сервер шлёт
/// флаг вместе с `debts: []` и `myBalance: 0` (json-тег с omitempty, поэтому у
/// здоровых комнат ключа в ответе НЕТ). Без флага нулевой баланс читается как
/// «все в расчёте» — ложное утверждение о деньгах. Android это уже различает
/// (GroupDetailScreen.kt), iOS раньше — нет.
final class RoomDebtsUnavailableTests: XCTestCase {

    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private func summary(_ json: String) throws -> RoomSummary {
        try decoder.decode(RoomSummary.self, from: Data(json.utf8))
    }

    private func detail(_ json: String) throws -> RoomDetail {
        try decoder.decode(RoomDetail.self, from: Data(json.utf8))
    }

    func testSummaryDecodesFlagWhenPresent() throws {
        let room = try summary("""
        {"id":"r1","name":"Тусa","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,
         "myBalance":0,"debtsUnavailable":true}
        """)
        XCTAssertTrue(room.debtsUnavailable)
        XCTAssertEqual(room.myBalance, 0, "сервер обнуляет баланс — без флага это читалось бы как «в расчёте»")
    }

    /// Ключ отсутствует у здоровых комнат (omitempty) — декодер обязан дать false,
    /// а не бросить keyNotFound: иначе список групп перестанет грузиться целиком.
    func testSummaryDefaultsToFalseWhenKeyAbsent() throws {
        let room = try summary("""
        {"id":"r1","name":"Тусa","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"memberCount":2,"currency":"RUB","totalSpent":1000,"myBalance":-500}
        """)
        XCTAssertFalse(room.debtsUnavailable)
        XCTAssertEqual(room.myBalance, -500)
    }

    func testDetailDecodesFlagWhenPresent() throws {
        let room = try detail("""
        {"id":"r1","name":"Тусa","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"currency":"RUB","totalSpent":1000,"mySpent":500,"myBalance":0,
         "debts":[],"operations":[],"debtsUnavailable":true}
        """)
        XCTAssertTrue(room.debtsUnavailable)
        XCTAssertTrue(room.debts.isEmpty)
    }

    func testDetailDefaultsToFalseWhenKeyAbsent() throws {
        let room = try detail("""
        {"id":"r1","name":"Тусa","createdAt":"2026-07-05T12:00:00Z","isArchived":false,
         "members":[],"currency":"RUB","totalSpent":1000,"mySpent":500,"myBalance":0,
         "debts":[],"operations":[]}
        """)
        XCTAssertFalse(room.debtsUnavailable)
    }
}
