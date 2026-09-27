import Foundation
import XCTest
@testable import Splitty

/// «Изменить суммы участников»: чек сворачивается в плоский расход по суммам.
///
/// Раньше в режиме чека карточки деления не было вовсе, а единственный выход —
/// «Поровну на всех» — выбрасывал распределение по позициям. Пользователь после
/// ИИ-разбора не мог поправить долю одного человека.
@MainActor
final class AddExpenseManualSharesTests: XCTestCase {

    private let members = [
        User(id: 1, username: "a", displayName: "Аня"),
        User(id: 2, username: "b", displayName: "Боря"),
        User(id: 3, username: "c", displayName: "Вера"),
    ]

    private func model(fractional: Bool) -> AddExpenseViewModel {
        let model = AddExpenseViewModel()
        model.selectRoom(RoomSummary(
            id: "r1", name: "Тест", createdAt: Date(timeIntervalSince1970: 0),
            isArchived: false, members: members, memberCount: members.count,
            currency: "RUB", fractional: fractional, totalSpent: 0, myBalance: 0
        ))
        return model
    }

    private func item(_ name: String, _ price: Int, _ ids: [Int]) -> OperationItem {
        OperationItem(name: name, price: price, qty: 1,
                      shares: ids.map { ItemShare(userId: $0, weight: 1) })
    }

    // MARK: Сворачивание

    /// Неравные доли со сбором: у каждого ровно то, что насчитали позиции.
    func testConvertKeepsEachPersonsShareIncludingSurcharge() throws {
        let model = model(fractional: false)
        model.draftItems = [
            item("Пицца", 1000, [1, 2]),
            item("Салат", 200, [2]),
            OperationItem(
                name: "Сбор", price: 120, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]

        let preview = try XCTUnwrap(model.manualSharesPreview)
        XCTAssertFalse(preview.wasRounded, "550 и 770 — целые, округлять нечего")
        model.convertItemsToManualShares()

        XCTAssertNil(model.draftItems, "позиции обязаны уйти — иначе сервер проигнорирует суммы")
        XCTAssertEqual(model.splitType, .byExactAmount)
        XCTAssertEqual(model.recipientIds, [1, 2])
        XCTAssertEqual(model.amountTexts, [1: "550", 2: "770"])
        XCTAssertEqual(model.sumText, "1320")
        XCTAssertTrue(model.showsSplitCard, "карточка деления обязана появиться")
    }

    /// 100 на троих в целой тусе: 34 / 33 / 33, итог 100 — и об округлении
    /// говорим прямо, потому что оно меняет долги.
    func testWholeRoomRoundsByLargestRemainderAndSaysSo() throws {
        let model = model(fractional: false)
        model.draftItems = [item("Такси", 100, [1, 2, 3])]

        let preview = try XCTUnwrap(model.manualSharesPreview)
        XCTAssertTrue(preview.wasRounded)
        model.convertItemsToManualShares()

        XCTAssertEqual(model.amountTexts, [1: "34", 2: "33", 3: "33"])
        XCTAssertEqual(model.sumText, "100", "итог обязан сохраниться")
    }

    /// Та же сумма в тусе с копейками: доли точные, округления нет.
    func testFractionalRoomKeepsExactMinor() throws {
        let model = model(fractional: true)
        model.draftItems = [item("Такси", 100, [1, 2, 3])]

        let preview = try XCTUnwrap(model.manualSharesPreview)
        XCTAssertFalse(preview.wasRounded)
        XCTAssertEqual(preview.shares.map(\.minor), [3334, 3333, 3333])
    }

    /// 1 ₽ на троих: после округления двое должны ноль — их не отправляем.
    func testZeroSharesAfterRoundingAreDropped() throws {
        let model = model(fractional: false)
        model.draftItems = [item("Жвачка", 1, [1, 2, 3])]

        model.convertItemsToManualShares()

        XCTAssertEqual(model.recipientIds, [1])
        XCTAssertEqual(model.amountTexts, [1: "1"])
        XCTAssertEqual(model.sumText, "1")
    }

    // MARK: Когда сворачивать нельзя

    /// Нераспознанное имя: потеря позиций разблокировала бы сохранение
    /// неверного расхода.
    func testNotOfferedWhileNameUnknown() {
        let model = model(fractional: false)
        model.draftItems = [OperationItem(
            name: "Пицца", price: 1200, qty: 1,
            shares: [ItemShare(userId: 1, weight: 1)], unknown: ["Саня"]
        )]

        XCTAssertNil(model.manualSharesPreview)
    }

    /// Позиция без цены — то же самое.
    func testNotOfferedWhilePriceMissing() {
        let model = model(fractional: false)
        model.draftItems = [item("Пицца", 0, [1, 2])]

        XCTAssertNil(model.manualSharesPreview)
    }

    /// Кнопка не прячется, а объясняет, почему недоступна.
    func testUnavailableReasonIsGivenInsteadOfHidingTheButton() {
        let model = model(fractional: false)
        model.draftItems = [OperationItem(
            name: "Пицца", price: 1200, qty: 1,
            shares: [ItemShare(userId: 1, weight: 1)], unknown: ["Саня"]
        )]
        XCTAssertNil(model.manualSharesPreview)
        XCTAssertNotNil(model.manualSharesUnavailableReason)

        model.draftItems = [item("Пицца", 1200, [1, 2])]
        XCTAssertNil(model.manualSharesUnavailableReason, "доступна — причины нет")
    }

    // MARK: Отмена

    /// Отмена возвращает не только позиции, но и деление, которое было ДО.
    ///
    /// Снимок раньше хранил позиции, описание, сумму и плательщика — после
    /// отмены позиции возвращались, а деление оставалось плоским от
    /// отменённого шага.
    func testUndoRestoresItemsAndSplit() {
        let model = model(fractional: false)
        let items = [item("Пицца", 1000, [1, 2])]
        model.draftItems = items
        model.recipientIds = [1, 2]
        model.sumText = "1000"

        model.convertItemsToManualShares()
        XCTAssertTrue(model.canUndoParse)
        model.undoParse()

        XCTAssertEqual(model.draftItems, items)
        XCTAssertEqual(model.splitType, .equally)
        XCTAssertEqual(model.recipientIds, [1, 2])
        XCTAssertEqual(model.amountTexts, [:])
        XCTAssertEqual(model.sumText, "1000")
    }

    /// «Поровну на всех» отменяется так же полно.
    func testUndoAfterEqualSplitRestoresItemsAndRecipients() {
        let model = model(fractional: false)
        let items = [item("Пицца", 1000, [1, 2])]
        model.draftItems = items
        model.recipientIds = [1, 2]

        model.collapseToEqualSplit()
        XCTAssertEqual(model.recipientIds, [1, 2, 3])
        model.undoParse()

        XCTAssertEqual(model.draftItems, items)
        XCTAssertEqual(model.recipientIds, [1, 2])
    }

    // MARK: Удаление последней позиции

    /// Удалили все позиции — форма пустая по сумме, а не расход на весь чек.
    ///
    /// Раньше в поле оставался распознанный итог, и удалённый чек сохранялся
    /// плоским расходом на всю сумму. Android это уже делал правильно.
    func testDeletingLastItemClearsSum() {
        let model = model(fractional: false)
        model.draftItems = [item("Пицца", 1000, [1, 2])]
        model.sumText = "1000"
        model.splitType = .byExactAmount

        model.deleteItem(at: 0)

        XCTAssertNil(model.draftItems)
        XCTAssertEqual(model.sumText, "")
        XCTAssertEqual(model.splitType, .equally)
        XCTAssertEqual(model.recipientIds, [1, 2, 3])
    }

    /// Отмена после ПЛОСКИХ ручных сумм возвращает их, а не только позиции.
    ///
    /// Ровно ради этого снимок и расширен: человек разложил 70/30, потом
    /// голосом поправил чек с другими участниками и нажал «Отменить». Прежний
    /// снимок возвращал позиции и сумму, а получатели и суммы оставались от
    /// отменённой правки.
    func testUndoAfterVoiceCorrectionRestoresManualAmounts() {
        let model = model(fractional: false)
        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 100, donorId: 1, items: nil),
            questions: []
        ))
        model.splitType = .byExactAmount
        model.recipientIds = [1, 2]
        model.amountTexts = [1: "70", 2: "30"]

        model.apply(parse: ParseResponse(
            draft: ParseDraft(description: "Ужин", sum: 100, donorId: 1,
                              items: [item("Ужин", 100, [2, 3])]),
            questions: ["Кто такой Саня?"]
        ))
        XCTAssertTrue(model.canUndoParse)
        model.undoParse()

        XCTAssertNil(model.draftItems)
        XCTAssertEqual(model.splitType, .byExactAmount)
        XCTAssertEqual(model.recipientIds, [1, 2])
        XCTAssertEqual(model.amountTexts, [1: "70", 2: "30"])
        XCTAssertEqual(model.parseQuestions, [])
    }

    // MARK: Правка сохранённого чека

    /// Сохранённый чек 100 на троих в ЦЕЛОЙ тусе: его доли уже дробные
    /// (3334/3333/3333), и сервер принимает правку, не меняющую их.
    ///
    /// Проверяется настоящий путь — `load(editOperation:)` и исходящий PUT, а не
    /// готовая форма с поднятым `fractional`: именно так ловится неверная
    /// инициализация точности и сериализация. Сворачивание обязано дать
    /// прежние точные доли без округления и уйти без позиций.
    func testSavedWholeRoomReceiptCollapsesIntoItsExactShares() async throws {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        let session = URLSession(configuration: configuration)
        let cacheDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("manual-shares-\(UUID().uuidString)", isDirectory: true)
        defer {
            try? FileManager.default.removeItem(at: cacheDir)
            StubURLProtocol.handler = nil
            StubURLProtocol.lastBody = nil
        }
        let roomJSON = #"""
        {"id":"room-1","name":"Дача","createdAt":"2026-09-01T10:00:00Z","isArchived":false,
         "currency":"RUB","members":[{"id":1,"username":"a","displayName":"Аня"},
         {"id":2,"username":"b","displayName":"Боря"},{"id":3,"username":"c","displayName":"Вера"}],
         "totalSpent":0,"mySpent":0,"myBalance":0,"debts":[],"operations":[],
         "seenThrough":"2026-09-01T10:00:00Z"}
        """#
        StubURLProtocol.handler = { request in
            request.httpMethod == "GET" ? (200, Data(roomJSON.utf8)) : (200, Data("{}".utf8))
        }
        let api = APIClient(baseURL: URL(string: "https://api.example.test"), token: "t", urlSession: session)
        let repo = DataRepo(api: api, cache: OfflineStore(directory: cacheDir))

        func recipient(_ id: Int, _ minor: Int) -> OperationRecipient {
            OperationRecipient(user: User(id: id, username: nil, displayName: "\(id)"),
                               sum: minor / 100, sumMinor: minor)
        }
        let operation = Operation(
            id: "op-1",
            description: "Такси",
            sum: 100,
            isDebtRepayment: false,
            donor: User(id: 1, username: nil, displayName: "1"),
            recipients: [recipient(1, 3334), recipient(2, 3333), recipient(3, 3333)],
            splitType: .byExactAmount,
            createdAt: Date(timeIntervalSince1970: 1_780_000_000),
            files: nil,
            items: [item("Такси", 100, [1, 2, 3])]
        )

        let model = AddExpenseViewModel()
        await model.load(repo: repo, fixedRoomId: "room-1", editOperation: operation, me: nil)

        let preview = try XCTUnwrap(model.manualSharesPreview)
        XCTAssertFalse(preview.wasRounded, "у правки дробной операции округлять нельзя")
        XCTAssertEqual(preview.shares.map(\.minor), [3334, 3333, 3333])

        model.convertItemsToManualShares()
        XCTAssertNil(model.draftItems)
        _ = await model.save(api: api, outbox: OutboxStore(), isOnline: true)

        let body = try XCTUnwrap(StubURLProtocol.lastBody, "PUT не ушёл")
        let json = try XCTUnwrap(try JSONSerialization.jsonObject(with: body) as? [String: Any])
        XCTAssertNil(json["items"], "позиции обязаны уйти — иначе сервер проигнорирует суммы")
        let sums = try XCTUnwrap(json["recipientSums"] as? [[String: Any]])
        XCTAssertEqual(sums.compactMap { $0["sumMinor"] as? Int }, [3334, 3333, 3333],
                       "доли в PUT изменились — сервер ответит 409 на правку дробных обязательств")
    }

    // MARK: Приведение к шагу

    func testDistributePreservesTotal() {
        let shares = AddExpenseViewModel.distribute(
            [(userId: 1, minor: 3334), (userId: 2, minor: 3333), (userId: 3, minor: 3333)], step: 100)
        XCTAssertEqual(shares.map(\.minor), [3400, 3300, 3300])
        XCTAssertEqual(shares.reduce(0) { $0 + $1.minor }, 10_000)
    }

    /// Ничья по остатку — в порядке появления в чеке.
    func testDistributeBreaksTiesByOrder() {
        let shares = AddExpenseViewModel.distribute(
            [(userId: 1, minor: 50), (userId: 2, minor: 50)], step: 100)
        XCTAssertEqual(shares.map(\.minor), [100, 0])
    }
}
