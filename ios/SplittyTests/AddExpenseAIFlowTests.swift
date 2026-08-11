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

    // MARK: Выбор экрана по исходу распознавания
    // (какой вид покажет форма: композер / чек / плоский AI-результат)

    func testItemizedResultShowsReceipt() {
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 1200, donorId: 1,
                              items: [OperationItem(name: "Пицца", price: 1200, shares: [ItemShare(userId: 1, weight: 1)])]),
            questions: nil))
        XCTAssertTrue(model.hasDraftItems)       // → receiptSection
        XCTAssertTrue(model.didRecognize)
        XCTAssertFalse(model.isEmptyForm)        // → не композер
    }

    /// Позиции чека решают, КАК делить, а плательщик — КТО дал деньги: строка
    /// «Заплатил(а)» обязана пережить распознавание чека. Раньше её уносило
    /// вместе со всей карточкой деления, и расход молча записывался на себя.
    func testPayerLineStaysVisibleWithReceiptItems() {
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 1200, donorId: 1,
                              items: [OperationItem(name: "Пицца", price: 1200,
                                                    shares: [ItemShare(userId: 1, weight: 1)])]),
            questions: nil))
        XCTAssertTrue(model.showsPayerLine)      // → payerLineCard над чеком
        XCTAssertFalse(model.showsSplitCard)     // способ деления задают позиции
    }

    /// Плоский расход: плательщик живёт внутри карточки деления, отдельной
    /// строки быть не должно — иначе он покажется дважды.
    func testPayerLineHiddenForFlatExpense() {
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Такси", sum: 400, donorId: nil, items: nil),
            questions: nil))
        XCTAssertFalse(model.showsPayerLine)
        XCTAssertTrue(model.showsSplitCard)
    }

    // MARK: Экран разбора: что останавливается, а что уходит сразу

    /// Одно правило для голоса и фото: первый ввод в пустую форму ждёт решения
    /// («Распознать / добавить второй источник / отмена»), всё остальное уходит
    /// в разбор без лишнего тапа.
    func testStopsAtReviewOnlyOnFirstInputIntoEmptyForm() {
        XCTAssertTrue(AddExpenseViewModel.stopsAtReview(isEmptyForm: true, hasOtherCapture: false))
        // второй источник к уже приложенному первому — уходят одним запросом
        XCTAssertFalse(AddExpenseViewModel.stopsAtReview(isEmptyForm: true, hasOtherCapture: true))
        // уточнение готового черновика — сразу в разбор
        XCTAssertFalse(AddExpenseViewModel.stopsAtReview(isEmptyForm: false, hasOtherCapture: false))
        XCTAssertFalse(AddExpenseViewModel.stopsAtReview(isEmptyForm: false, hasOtherCapture: true))
    }

    func testFlatResultMarkedRecognizedNotManual() {
        // модель вернула сумму без позиций — это НЕ ручной ввод, форма должна
        // показать плашку «Распознано голосом», а не выглядеть как мануал
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Такси", sum: 400, donorId: nil, items: nil),
            questions: nil))
        XCTAssertFalse(model.hasDraftItems)
        XCTAssertTrue(model.didRecognize)        // → recognizedBanner
        XCTAssertFalse(model.isEmptyForm)
    }

    func testEmptyResultKeepsComposer() {
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "", sum: 0, donorId: nil, items: nil),
            questions: ["не удалось распознать"]))
        XCTAssertFalse(model.didRecognize)       // ничего не распознали
        XCTAssertTrue(model.isEmptyForm)         // → композер остаётся
        // вопрос модели виден у композера, алерт не дублируется
        XCTAssertEqual(model.parseQuestions, ["не удалось распознать"])
        XCTAssertNil(model.alertMessage)
    }

    func testEmptyResultWithoutQuestionsShowsAlert() {
        // совсем пусто и без вопросов — молча вернуть композер нельзя,
        // пользователь должен понять, что произошло
        let model = AddExpenseViewModel()
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "", sum: 0, donorId: nil, items: nil),
            questions: nil))
        XCTAssertFalse(model.didRecognize)
        XCTAssertTrue(model.isEmptyForm)
        XCTAssertNotNil(model.alertMessage)
    }

    // MARK: Подсказки «что уточнить» на экране диктовки

    func testMissingInfoHintsCollectUnknownPricesAndQuestions() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 0, qty: 1,
                          shares: [ItemShare(userId: 1, weight: 1)], unknown: ["Саня"]),
            OperationItem(name: "Салат", price: 300, qty: 1,
                          shares: [ItemShare(userId: 1, weight: 1)]),
        ]
        // Вопрос про пиццу дублирует synthetic-подсказку цены — фильтруется;
        // вопрос про плательщика — нет.
        model.parseQuestions = ["Сколько стоила пицца?", "Кто платил?"]

        XCTAssertEqual(model.missingInfoHints, [
            "Кто это — «Саня»?",
            "Сколько стоит «Пицца»?",
            "Кто платил?",
        ])
    }

    func testMissingInfoHintsEmptyWhenDraftComplete() {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 600, qty: 1,
                          shares: [ItemShare(userId: 1, weight: 1)]),
        ]
        XCTAssertTrue(model.missingInfoHints.isEmpty)
    }

    // MARK: Голосовая правка: подсветка изменений и отмена

    func testCorrectionMarksChangedItemsAndAllowsUndo() {
        let model = AddExpenseViewModel()
        let pizza = OperationItem(name: "Пицца", price: 1200, qty: 1,
                                  shares: [ItemShare(userId: 1, weight: 1), ItemShare(userId: 2, weight: 1)])
        let beer = OperationItem(name: "Пиво", price: 600, qty: 1,
                                 shares: [ItemShare(userId: 1, weight: 1), ItemShare(userId: 2, weight: 1)])
        // Первое распознавание — не «правка»: без снапшота и подсветки.
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 1800, donorId: 1, items: [pizza, beer]),
            questions: nil))
        XCTAssertFalse(model.canUndoParse)
        XCTAssertTrue(model.changedItemIndices.isEmpty)

        // Правка: «пиво только Лёха» — изменилась вторая позиция.
        let beerFixed = OperationItem(name: "Пиво", price: 600, qty: 1,
                                      shares: [ItemShare(userId: 2, weight: 1)])
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 1800, donorId: 1, items: [pizza, beerFixed]),
            questions: nil))
        XCTAssertEqual(model.changedItemIndices, [1])
        XCTAssertTrue(model.canUndoParse)

        // Откат возвращает прежний черновик.
        model.undoParse()
        XCTAssertEqual(model.draftItems, [pizza, beer])
        XCTAssertFalse(model.canUndoParse)
        XCTAssertTrue(model.changedItemIndices.isEmpty)
    }

    func testChangedIndicesDetectsEditsAndAdditions() {
        let a = OperationItem(name: "A", price: 100, shares: [ItemShare(userId: 1, weight: 1)])
        let b = OperationItem(name: "B", price: 200, shares: [ItemShare(userId: 1, weight: 1)])
        let b2 = OperationItem(name: "B", price: 250, shares: [ItemShare(userId: 1, weight: 1)])
        let c = OperationItem(name: "C", price: 300, shares: [ItemShare(userId: 1, weight: 1)])
        // Изменение по месту + добавление в конец.
        XCTAssertEqual(AddExpenseViewModel.changedIndices(old: [a, b], new: [a, b2, c]), [1, 2])
        // Без изменений — пусто.
        XCTAssertEqual(AddExpenseViewModel.changedIndices(old: [a, b], new: [a, b]), [])
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

    func testCanSaveFalseWhilePricelessItemPresent() {
        // Позиция «цена не определена» (price=0): раскладка есть, но сохранять
        // получек нельзя — сервер тоже отклонит (price ≥ 1).
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 0, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
            OperationItem(name: "Салат", price: 300, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
            ]),
        ]
        XCTAssertTrue(model.hasPricelessItems)
        XCTAssertFalse(model.canSave)

        // Цена заполнена (шит позиции) — сохранение открывается.
        model.replaceItem(at: 0, with: OperationItem(name: "Пицца", price: 600, qty: 1, shares: [
            ItemShare(userId: 1, weight: 1),
            ItemShare(userId: 2, weight: 1),
        ]))
        XCTAssertFalse(model.hasPricelessItems)
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

    // MARK: Разбивка «С кого сколько»

    func testPersonSharesSplitsBaseAndSurcharge() throws {
        let model = AddExpenseViewModel()
        model.draftItems = [
            // Пицца 1000: юзер 1 и 2 поровну (по 500).
            OperationItem(name: "Пицца", price: 1000, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
            // Салат 200: только юзер 2.
            OperationItem(name: "Салат", price: 200, qty: 1, shares: [
                ItemShare(userId: 2, weight: 1),
            ]),
            // Сбор 120 пропорционально базе (500 против 700): 50 и 70.
            OperationItem(
                name: "Сбор", price: 120, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]

        let shares = try XCTUnwrap(model.personShares)
        XCTAssertEqual(shares.map(\.userId), [1, 2])
        XCTAssertEqual(shares[0].total, 550)
        XCTAssertEqual(shares[0].surchargePart, 50)
        XCTAssertEqual(shares[1].total, 770)
        XCTAssertEqual(shares[1].surchargePart, 70)
        // Σ итогов == итог чека до рубля.
        XCTAssertEqual(shares.reduce(0) { $0 + $1.total }, model.itemizedTotal)
    }

    func testPersonSharesNilWhenItemsInvalid() {
        let model = AddExpenseViewModel()
        // Фиксы превышают цену — доли невыводимы.
        model.draftItems = [
            OperationItem(name: "Позиция", price: 100, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1, amount: 80),
                ItemShare(userId: 2, weight: 1, amount: 40),
            ]),
        ]
        XCTAssertNil(model.personShares)
    }

    // MARK: Правило деления сбора

    func testToggleSurchargeRuleFlipsSplitAndRecalculates() throws {
        let model = AddExpenseViewModel()
        model.draftItems = [
            OperationItem(name: "Пицца", price: 1000, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
            ]),
            OperationItem(name: "Салат", price: 200, qty: 1, shares: [
                ItemShare(userId: 2, weight: 1),
            ]),
            OperationItem(
                name: "Сбор", price: 120, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]
        // Пропорционально: 100 против 20.
        var shares = try XCTUnwrap(model.personShares)
        XCTAssertEqual(shares.map(\.surchargePart), [100, 20])

        model.toggleSurchargeRule(at: 2)
        XCTAssertEqual(model.draftItems?[2].split, OperationItem.splitEqually)
        // Поровну: по 60.
        shares = try XCTUnwrap(model.personShares)
        XCTAssertEqual(shares.map(\.surchargePart), [60, 60])

        // Обратно — снова пропорционально.
        model.toggleSurchargeRule(at: 2)
        XCTAssertEqual(model.draftItems?[2].split, OperationItem.splitProportional)
    }

    func testToggleSurchargeRuleIgnoresRegularItem() {
        let model = AddExpenseViewModel()
        let item = OperationItem(name: "Пицца", price: 1000, qty: 1, shares: [
            ItemShare(userId: 1, weight: 1),
        ])
        model.draftItems = [item]
        model.toggleSurchargeRule(at: 0)
        XCTAssertEqual(model.draftItems?[0], item)
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
