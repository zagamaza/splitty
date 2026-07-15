import Foundation
import XCTest
@testable import Splitty

/// AI-поток формы расхода (`AddExpenseViewModel`): заполнение из `parseResponse`,
/// сброс позиций при ручной правке, сопоставление нераспознанного имени и правила
/// `canSave` для itemized-черновика (блок при непустом `unknown`, независимость от
/// расхождения плоского `sum` с Σ позиций — суммы выводит сервер).
@MainActor
final class AddExpenseAIFlowTests: XCTestCase {

    // MARK: Заполнение формы из ответа распознавания

    func testApplyParseResponseFillsForm() {
        let model = AddExpenseViewModel()
        let items = [
            OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
        ]
        let response = ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 1200, donorId: 1, items: items),
            questions: ["Кто платил?"]
        )

        model.apply(parse: response)

        XCTAssertEqual(model.descriptionText, "Ужин")
        XCTAssertEqual(model.sumText, "1200")
        XCTAssertEqual(model.draftItems, items)
        XCTAssertTrue(model.hasDraftItems)
        // Получатели синхронизированы с участниками позиций.
        XCTAssertEqual(model.recipientIds, [1, 2])
        XCTAssertEqual(model.parseQuestions, ["Кто платил?"])
    }

    // MARK: Сброс позиций при ручной правке / «Поровну на всех»

    func testResetItemsClearsDraftAndSwitchesToEqually() {
        let model = AddExpenseViewModel()
        model.splitType = .byExactAmount
        model.draftItems = [
            OperationItem(name: "Кофе", price: 300, qty: 1, shares: [ItemShare(userId: 1, weight: 1)]),
        ]
        XCTAssertTrue(model.hasDraftItems)

        model.resetItems()

        XCTAssertNil(model.draftItems)
        XCTAssertFalse(model.hasDraftItems)
        XCTAssertEqual(model.splitType, .equally)
    }

    // MARK: Сопоставление нераспознанного имени

    func testResolveUnknownAppliesLocallyAndClearsUnknown() throws {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(
                name: "Пиво", price: 500, qty: 1,
                shares: [ItemShare(userId: 1, weight: 1)],
                unknown: ["Саня"]
            ),
        ]
        // nil-baseURL: дозапись алиаса (fire-and-forget) бросит invalidURL и будет
        // проглочена try? — локальное применение синхронно и от сети не зависит.
        model.resolveUnknown(itemIndex: 0, name: "Саня", to: 2, api: APIClient(baseURL: nil, token: nil))

        XCTAssertFalse(model.hasUnknownItems)
        let item = try XCTUnwrap(model.draftItems?.first)
        XCTAssertNil(item.unknown)
        XCTAssertTrue(item.shareList.contains { $0.userId == 2 })
        // Имя разрешено, доли выводятся → сохранение снова доступно.
        XCTAssertTrue(model.canSave)
    }

    // MARK: canSave для itemized-черновика

    func testCanSaveFalseWhileUnknownPresent() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(
                name: "Пиво", price: 500, qty: 1,
                shares: [ItemShare(userId: 1, weight: 1)],
                unknown: ["Саня"]
            ),
        ]
        XCTAssertTrue(model.hasUnknownItems)
        XCTAssertEqual(model.firstUnknownName, "Саня")
        XCTAssertFalse(model.canSave)
    }

    func testCanSaveIgnoresFlatSumMismatchForItemized() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 1000, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
        ]
        // Плоская сумма расходится с Σ позиций (1000) — серверу неважно, он выводит
        // суммы из позиций, поэтому сохранение доступно.
        model.sumText = "999"

        XCTAssertTrue(model.hasDraftItems)
        XCTAssertEqual(model.itemizedTotal, 1000)
        XCTAssertTrue(model.canSave)
    }

    func testCanSaveFalseWhenItemsOverAllocated() {
        let model = AddExpenseViewModel()
        // Фиксы (80 + 40) превышают цену позиции (100) → доли не выводятся.
        model.draftItems = [
            OperationItem(name: "Позиция", price: 100, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1, amount: 80),
                ItemShare(userId: 2, weight: 1, amount: 40),
            ]),
        ]
        XCTAssertNil(model.itemizedShares)
        XCTAssertFalse(model.canSave)
    }

    // MARK: Правка позиции (write-back шита)

    func testReplaceItemUpdatesDraft() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [ItemShare(userId: 1, weight: 1)]),
        ]
        let updated = OperationItem(name: "Пицца 2", price: 900, qty: 1, shares: [
            ItemShare(userId: 1, weight: 1),
            ItemShare(userId: 2, weight: 2),
        ])

        model.replaceItem(at: 0, with: updated)

        XCTAssertEqual(model.draftItems?.first, updated)
        // Новый участник (2) подхвачен в получатели.
        XCTAssertEqual(model.recipientIds, [1, 2])
    }

    // MARK: Подытог / сборы / итого

    func testSubtotalAndSurchargesSplit() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
            OperationItem(
                name: "Сервисный сбор", price: 120, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]
        XCTAssertEqual(model.itemizedSubtotal, 1200)
        XCTAssertEqual(model.itemizedSurcharges, 120)
        XCTAssertEqual(model.itemizedTotal, 1320)
    }
}
