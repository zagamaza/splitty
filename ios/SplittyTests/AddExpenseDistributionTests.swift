import Foundation
import XCTest
@testable import Splitty

/// Валидация распределения в режиме «По суммам» (AddExpenseViewModel)
/// и формы тела запроса операции (OperationBody, контракт v2).
@MainActor
final class AddExpenseDistributionTests: XCTestCase {
    private func makeModel(
        sum: String,
        recipientIds: Set<Int>,
        amounts: [Int: String]
    ) -> AddExpenseViewModel {
        let model = AddExpenseViewModel()
        model.sumText = sum
        model.recipientIds = recipientIds
        model.amountTexts = amounts
        model.splitType = .byExactAmount
        return model
    }

    // MARK: Остаток и валидность

    func testRemainingToDistribute() {
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "700", 2: "200"])
        XCTAssertEqual(model.distributedTotal, 900)
        XCTAssertEqual(model.remainingToDistribute, 100)
        XCTAssertFalse(model.isDistributionBalanced)
        XCTAssertFalse(model.canSave)
    }

    func testExactDistributionEnablesSave() {
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "700", 2: "300"])
        XCTAssertEqual(model.remainingToDistribute, 0)
        XCTAssertTrue(model.isDistributionBalanced)
        XCTAssertTrue(model.canSave)
    }

    func testOverDistributionIsNegativeAndBlocksSave() {
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "800", 2: "300"])
        XCTAssertEqual(model.remainingToDistribute, -100)
        XCTAssertFalse(model.canSave)
    }

    func testUnselectedMemberAmountsAreIgnored() {
        // Сумма снятого с выбора участника (id 3) не считается в Σ.
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "700", 2: "300", 3: "999"])
        XCTAssertEqual(model.distributedTotal, 1000)
        XCTAssertTrue(model.isDistributionBalanced)
    }

    func testEmptyAmountFieldCountsAsZero() {
        let model = makeModel(sum: "500", recipientIds: [1, 2], amounts: [1: "500"])
        XCTAssertEqual(model.enteredAmount(of: 2), 0)
        XCTAssertEqual(model.remainingToDistribute, 0)
        XCTAssertTrue(model.isDistributionBalanced)
    }

    func testZeroShareParticipantsAreDroppedFromRecipientSums() {
        // Участник 2 выбран, но его доля 0 (пустое поле): Σ == sum, сохранить
        // можно, но в recipientSums нулевая доля не отправляется — сервер
        // отклоняет суммы < 1, а получатель с долей 0 не участвует в делении.
        let model = makeModel(sum: "500", recipientIds: [1, 2], amounts: [1: "500"])
        XCTAssertTrue(model.canSave)
        XCTAssertEqual(
            model.exactRecipientSums(orderedIds: [1, 2]),
            [RecipientSum(userId: 1, sum: 500)]
        )
    }

    func testExactRecipientSumsKeepStableOrder() {
        let model = makeModel(
            sum: "600",
            recipientIds: [1, 2, 3],
            amounts: [1: "100", 2: "200", 3: "300"]
        )
        XCTAssertEqual(
            model.exactRecipientSums(orderedIds: [3, 1, 2]),
            [
                RecipientSum(userId: 3, sum: 300),
                RecipientSum(userId: 1, sum: 100),
                RecipientSum(userId: 2, sum: 200),
            ]
        )
    }

    func testZeroSumIsNeverBalanced() {
        let model = makeModel(sum: "", recipientIds: [1], amounts: [:])
        XCTAssertFalse(model.isDistributionBalanced)
        XCTAssertFalse(model.canSave)
    }

    func testEquallyModeDoesNotBlockSave() {
        // Дефолтный режим «Поровну»: кнопка активна, валидация — алертами.
        let model = AddExpenseViewModel()
        XCTAssertEqual(model.splitType, .equally)
        XCTAssertTrue(model.canSave)
    }

    func testDistributionHintShowsRemainder() {
        let model = makeModel(sum: "1000", recipientIds: [1], amounts: [1: "400"])
        XCTAssertEqual(model.distributionHint, "Осталось распределить: 600 ₽")

        model.amountTexts = [1: "1000"]
        XCTAssertEqual(model.distributionHint, "Сумма распределена полностью")

        model.amountTexts = [1: "1100"]
        XCTAssertEqual(model.distributionHint, "Перерасход: 100 ₽")
    }

    // MARK: Формы тела запроса (контракт v2)

    private func encodeBody(_ body: OperationBody) throws -> [String: Any] {
        let data = try JSONEncoder().encode(body)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    func testEquallyBodySendsRecipientIdsOnly() throws {
        let json = try encodeBody(OperationBody(
            description: "Ужин", sum: 1200, donorId: 10,
            split: .equally(recipientIds: [10, 20])
        ))
        XCTAssertEqual(json["recipientIds"] as? [Int], [10, 20])
        XCTAssertNil(json["recipientSums"])
        XCTAssertEqual(json["sum"] as? Int, 1200)
        XCTAssertEqual(json["donorId"] as? Int, 10)
    }

    func testByExactAmountBodySendsRecipientSumsOnly() throws {
        let json = try encodeBody(OperationBody(
            description: "Ужин", sum: 1000, donorId: 10,
            split: .byExactAmount(recipientSums: [
                RecipientSum(userId: 10, sum: 700),
                RecipientSum(userId: 20, sum: 300),
            ])
        ))
        XCTAssertNil(json["recipientIds"])
        let sums = try XCTUnwrap(json["recipientSums"] as? [[String: Any]])
        XCTAssertEqual(sums.count, 2)
        XCTAssertEqual(sums[0]["userId"] as? Int, 10)
        XCTAssertEqual(sums[0]["sum"] as? Int, 700)
        XCTAssertEqual(sums[1]["userId"] as? Int, 20)
        XCTAssertEqual(sums[1]["sum"] as? Int, 300)
    }
}
