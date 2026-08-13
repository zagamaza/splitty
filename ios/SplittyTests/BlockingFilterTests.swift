import XCTest
@testable import Splitty

/// «Показать эти расходы» из отказа в выходе.
///
/// Отказ советует убрать себя из расходов, но не говорит, из каких: в группе их
/// бывают сотни, и совет оставался невыполнимым. Фильтр показывает ровно те,
/// что держат, — поэтому он обязан не показывать лишнего.
final class BlockingFilterTests: XCTestCase {

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
            description: "Ужин \(id)",
            sum: 100,
            isDebtRepayment: isRepayment,
            donor: user(donorId),
            recipients: recipientIds.map { OperationRecipient(user: user($0), sum: 50) },
            splitType: .equally,
            createdAt: Date(),
            files: nil
        )
    }

    private func section(_ id: Date, _ operations: [Splitty.Operation]) -> GroupDetailViewModel.MonthSection {
        GroupDetailViewModel.MonthSection(id: id, title: "Август", operations: operations)
    }

    func testFilterKeepsOnlyTheListedOperations() {
        let august = Date(timeIntervalSince1970: 1_754_000_000)
        let july = Date(timeIntervalSince1970: 1_751_000_000)
        let sections = [
            section(august, [operation(id: "1", donorId: 1, recipientIds: [2]),
                             operation(id: "2", donorId: 2, recipientIds: [2])]),
            section(july, [operation(id: "3", donorId: 1, recipientIds: [2])]),
        ]

        let kept = GroupDetailViewModel.sectionsKeepingOnly(sections, ids: ["1", "3"])

        XCTAssertEqual(kept.map { $0.operations.map(\.id) }, [["1"], ["3"]])
    }

    /// Месяц без единого мешающего расхода не должен показывать пустой заголовок.
    func testMonthsWithoutMatchesDisappear() {
        let august = Date(timeIntervalSince1970: 1_754_000_000)
        let july = Date(timeIntervalSince1970: 1_751_000_000)
        let sections = [
            section(august, [operation(id: "1", donorId: 1, recipientIds: [2])]),
            section(july, [operation(id: "2", donorId: 2, recipientIds: [2])]),
        ]

        let kept = GroupDetailViewModel.sectionsKeepingOnly(sections, ids: ["1"])

        XCTAssertEqual(kept.map(\.id), [august])
    }

    /// Фильтр и подпись под кнопкой выхода обязаны считать одно и то же.
    func testFilterShowsExactlyWhatTheRefusalCounts() {
        let operations = [
            operation(id: "1", donorId: 1, recipientIds: [2]),
            operation(id: "2", donorId: 2, recipientIds: [1, 2]),
            operation(id: "3", donorId: 2, recipientIds: [2]),
            operation(id: "4", donorId: 1, recipientIds: [2], isRepayment: true),
        ]
        let room = RoomDetail(
            id: "room1",
            name: "Поездка",
            createdAt: Date(),
            isArchived: false,
            members: [user(1), user(2)],
            currency: "RUB",
            totalSpent: 400,
            mySpent: 100,
            myBalance: 0,
            debts: [],
            operations: operations
        )
        let blocking = room.operationsBlockingLeave(for: 1)

        let kept = GroupDetailViewModel.sectionsKeepingOnly(
            [section(Date(timeIntervalSince1970: 1_754_000_000), operations)],
            ids: Set(blocking.map(\.id))
        )

        XCTAssertEqual(kept.reduce(0) { $0 + $1.operations.count }, blocking.count)
        XCTAssertEqual(kept.first?.operations.map(\.id), ["1", "2"])
    }
}
