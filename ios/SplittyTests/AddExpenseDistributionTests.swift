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
        XCTAssertEqual(model.distributedTotalMinor, 90000)
        XCTAssertEqual(model.remainingToDistributeMinor, 10000)
        XCTAssertFalse(model.isDistributionBalanced)
        XCTAssertFalse(model.canSave)
    }

    func testExactDistributionEnablesSave() {
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "700", 2: "300"])
        XCTAssertEqual(model.remainingToDistributeMinor, 0)
        XCTAssertTrue(model.isDistributionBalanced)
        XCTAssertTrue(model.canSave)
    }

    func testOverDistributionIsNegativeAndBlocksSave() {
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "800", 2: "300"])
        XCTAssertEqual(model.remainingToDistributeMinor, -10000)
        XCTAssertFalse(model.canSave)
    }

    func testUnselectedMemberAmountsAreIgnored() {
        // Сумма снятого с выбора участника (id 3) не считается в Σ.
        let model = makeModel(sum: "1000", recipientIds: [1, 2], amounts: [1: "700", 2: "300", 3: "999"])
        XCTAssertEqual(model.distributedTotalMinor, 100000)
        XCTAssertTrue(model.isDistributionBalanced)
    }

    func testEmptyAmountFieldCountsAsZero() {
        let model = makeModel(sum: "500", recipientIds: [1, 2], amounts: [1: "500"])
        XCTAssertEqual(model.enteredAmountMinor(of: 2), 0)
        XCTAssertEqual(model.remainingToDistributeMinor, 0)
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
            [RecipientSum(userId: 1, minor: 50000)]
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
                RecipientSum(userId: 3, minor: 30000),
                RecipientSum(userId: 1, minor: 10000),
                RecipientSum(userId: 2, minor: 20000),
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
        XCTAssertEqual(model.distributionHint, "Осталось распределить: 600 ₽")

        model.amountTexts = [1: "1000"]
        XCTAssertEqual(model.distributionHint, "Сумма распределена полностью")

        model.amountTexts = [1: "1100"]
        XCTAssertEqual(model.distributionHint, "Перерасход: 100 ₽")
    }

    // MARK: Дробные доли

    func testFractionalSharesBalanceAgainstFractionalSum() {
        // 20,80 на двоих: целыми это не набирается вовсе — 10 + 11 не сходится
        // ни с 20,80, ни с округлённым 21, и расход не сохранялся.
        let model = makeModel(sum: "20,80", recipientIds: [1, 2], amounts: [1: "10,40", 2: "10,40"])
        XCTAssertEqual(model.distributedTotalMinor, 2080)
        XCTAssertEqual(model.remainingToDistributeMinor, 0)
        XCTAssertTrue(model.isDistributionBalanced)
        XCTAssertTrue(model.canSave)
    }

    func testWholeSharesDoNotBalanceFractionalSum() {
        let model = makeModel(sum: "20,80", recipientIds: [1, 2], amounts: [1: "10", 2: "11"])
        XCTAssertEqual(model.remainingToDistributeMinor, -20)
        XCTAssertFalse(model.isDistributionBalanced)
        XCTAssertFalse(model.canSave)
    }

    func testFractionalRecipientSumsCarryExactValue() {
        let model = makeModel(sum: "20,80", recipientIds: [1, 2], amounts: [1: "10,40", 2: "10,40"])
        let sums = model.exactRecipientSums(orderedIds: [1, 2])
        XCTAssertEqual(sums.map(\.sumMinor), [1040, 1040])
        // Целое поле — округление точного ровно по правилу сервера: иначе пара
        // полей разошлась бы и запрос вернул 400.
        XCTAssertEqual(sums.map(\.sum), [10, 10])
    }

    func testFractionalRemainderHint() {
        let model = makeModel(sum: "20,80", recipientIds: [1, 2], amounts: [1: "10,40", 2: "10"])
        XCTAssertEqual(model.distributionHint, "Осталось распределить: 0,40 ₽")
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

    func testFractionalRecipientSumsSendMinorField() throws {
        let json = try encodeBody(OperationBody(
            description: "Ужин", sum: 21, sumMinor: 2080, donorId: 10,
            split: .byExactAmount(recipientSums: [
                RecipientSum(userId: 10, minor: 1040),
                RecipientSum(userId: 20, minor: 1040),
            ])
        ))
        let sums = try XCTUnwrap(json["recipientSums"] as? [[String: Any]])
        XCTAssertEqual(sums[0]["sumMinor"] as? Int, 1040)
        XCTAssertEqual(sums[0]["sum"] as? Int, 10)
    }
}
