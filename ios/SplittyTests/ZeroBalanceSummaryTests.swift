import XCTest
@testable import Splitty

/// Нулевой общий итог по группам не должен выдаваться за «все долги погашены»,
/// когда долги на самом деле есть или не считаются.
final class ZeroBalanceSummaryTests: XCTestCase {
    private func room(_ id: String, balance: Int, unavailable: Bool = false) -> RoomSummary {
        var room = RoomSummary(
            id: id, name: id, createdAt: Date(timeIntervalSince1970: 1_750_000_000), isArchived: false,
            members: [], memberCount: 2, currency: "RUB", totalSpent: 1000, myBalance: balance
        )
        room.debtsUnavailable = unavailable
        return room
    }

    func testOppositeDebtsCancelOutAreNotSettled() {
        XCTAssertEqual(ZeroBalanceSummary.of([room("a", balance: 500), room("b", balance: -500)]), .cancelOut)
    }

    func testOnlyUnavailableRoomsAreNotSettled() {
        XCTAssertEqual(ZeroBalanceSummary.of([room("a", balance: 0, unavailable: true)]), .unavailable)
    }

    func testTrulySettled() {
        XCTAssertEqual(ZeroBalanceSummary.of([room("a", balance: 0), room("b", balance: 0, unavailable: true)]), .settled)
        XCTAssertEqual(ZeroBalanceSummary.of([]), .settled)
    }
}
