import XCTest
@testable import Splitty

/// Сколько расходов держат человека в группе.
///
/// Раньше это знал только сервер: человек жал «Выйти» и получал отказ. Считаем
/// на клиенте — комната уже в памяти, — чтобы сказать заранее и назвать число.
final class LeaveBlockingOperationsTests: XCTestCase {

    private func user(_ id: Int) -> User {
        User(id: id, username: nil, displayName: "U\(id)")
    }

    private func operation(
        id: String,
        donorId: Int,
        recipientIds: [Int],
        isRepayment: Bool = false
    ) -> Splitty.Operation {
        Operation(
            id: id,
            description: "Ужин",
            sum: 100,
            isDebtRepayment: isRepayment,
            donor: user(donorId),
            recipients: recipientIds.map { OperationRecipient(user: user($0), sum: 50) },
            splitType: .equally,
            createdAt: Date(),
            files: nil
        )
    }

    private func room(operations: [Splitty.Operation]) -> RoomDetail {
        RoomDetail(
            id: "room1",
            name: "Поездка",
            createdAt: Date(),
            isArchived: false,
            members: [user(1), user(2)],
            currency: "RUB",
            totalSpent: 100,
            mySpent: 50,
            myBalance: 0,
            debts: [],
            operations: operations
        )
    }

    func testCountsExpensesWhereUserIsRecipient() {
        let detail = room(operations: [operation(id: "1", donorId: 2, recipientIds: [1, 2])])
        XCTAssertEqual(detail.operationsBlockingLeave(for: 1).count, 1)
    }

    func testCountsExpensesWhereUserPaid() {
        let detail = room(operations: [operation(id: "1", donorId: 1, recipientIds: [2])])
        XCTAssertEqual(detail.operationsBlockingLeave(for: 1).count, 1)
    }

    /// Погашения не держат: они не перестают быть верными после ухода.
    func testRepaymentsDoNotBlock() {
        let detail = room(operations: [operation(id: "1", donorId: 1, recipientIds: [2], isRepayment: true)])
        XCTAssertTrue(detail.operationsBlockingLeave(for: 1).isEmpty)
    }

    func testStrangerExpensesDoNotBlock() {
        let detail = room(operations: [operation(id: "1", donorId: 2, recipientIds: [2])])
        XCTAssertTrue(detail.operationsBlockingLeave(for: 1).isEmpty)
    }
}
