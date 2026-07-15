import Foundation
import XCTest
@testable import Splitty

/// Модель черновика itemized-расхода (`OperationItem`/`ItemShare`/`ParseDraft`/
/// `ParseResponse`), клиентское превью долей (зеркало серверного `DeriveShares`)
/// и офлайн-раундтрип outbox (позиции переживают enqueue → flush → вызов API).
@MainActor
final class ItemDraftTests: XCTestCase {

    // MARK: - Codable round-trip

    /// Полный черновик (позиции с весами, фикс-сумма, надбавка, unknown) переживает
    /// encode → decode без потерь.
    func testParseResponseCodableRoundTrip() throws {
        let response = ParseResponse(
            draft: ParseDraft(
                description: "Ужин",
                sum: 4700,
                donorId: 1,
                items: [
                    OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [
                        ItemShare(userId: 1, weight: 1),
                        ItemShare(userId: 2, weight: 1),
                    ]),
                    OperationItem(name: "Вино", price: 3000, qty: 2, shares: [
                        ItemShare(userId: 3, weight: 1, amount: 500),
                        ItemShare(userId: 1, weight: 1),
                    ]),
                    OperationItem(
                        name: "Сервисный сбор", price: 470, qty: 1, shares: nil,
                        kind: OperationItem.kindSurcharge,
                        split: OperationItem.splitProportional, percent: 10
                    ),
                    OperationItem(name: "Баурсаки", price: 500, qty: 10, shares: [
                        ItemShare(userId: 1, weight: 1),
                    ], unknown: ["Саня"]),
                ]
            ),
            questions: ["Кто платил?"]
        )

        let data = try JSONEncoder().encode(response)
        let decoded = try JSONDecoder().decode(ParseResponse.self, from: data)
        XCTAssertEqual(decoded, response)
    }

    /// Черновик декодируется из JSON, собранного по серверным ключам
    /// (`ai.ParseResult`/`ai.Draft` в internal/ai): userId/weight/amount, kind/split/percent, unknown.
    func testParseResponseDecodesServerShape() throws {
        let json = """
        {
          "draft": {
            "description": "Ужин",
            "sum": 1700,
            "donorId": 1,
            "items": [
              {
                "name": "Пицца",
                "price": 1200,
                "qty": 1,
                "shares": [
                  {"userId": 1, "weight": 1},
                  {"userId": 2, "weight": 1}
                ],
                "kind": "item"
              },
              {
                "name": "Сбор",
                "price": 500,
                "qty": 1,
                "kind": "surcharge",
                "split": "equally",
                "percent": 10
              },
              {
                "name": "Пиво",
                "price": 0,
                "qty": 1,
                "shares": [],
                "kind": "item",
                "unknown": ["Саня"]
              }
            ]
          },
          "questions": ["Кто платил?"]
        }
        """
        let response = try JSONDecoder().decode(ParseResponse.self, from: Data(json.utf8))

        XCTAssertEqual(response.draft.description, "Ужин")
        XCTAssertEqual(response.draft.donorId, 1)
        XCTAssertEqual(response.draft.itemList.count, 3)
        XCTAssertEqual(response.questionList, ["Кто платил?"])

        let pizza = response.draft.itemList[0]
        XCTAssertFalse(pizza.isSurcharge)
        XCTAssertEqual(pizza.shareList.map(\.userId), [1, 2])
        XCTAssertNil(pizza.shareList[0].amount)

        // Надбавка без `shares` → shareList пуст, isSurcharge истинно.
        let surcharge = response.draft.itemList[1]
        XCTAssertTrue(surcharge.isSurcharge)
        XCTAssertTrue(surcharge.shareList.isEmpty)
        XCTAssertEqual(surcharge.split, OperationItem.splitEqually)
        XCTAssertEqual(surcharge.percent, 10)

        // Нераспознанное имя проброшено в unknown → черновик нельзя сохранить.
        let beer = response.draft.itemList[2]
        XCTAssertTrue(beer.hasUnknown)
        XCTAssertEqual(beer.unknown, ["Саня"])
        XCTAssertTrue(response.draft.hasUnknown)
    }

    /// nil-опции (`split`/`percent`/`unknown`) не сериализуются, `shares` надбавки — тоже.
    func testOptionalFieldsOmittedWhenNil() throws {
        let item = OperationItem(name: "Кофе", price: 300, qty: 1, shares: nil, kind: OperationItem.kindItem)
        let data = try JSONEncoder().encode(item)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertNil(object["shares"])
        XCTAssertNil(object["split"])
        XCTAssertNil(object["percent"])
        XCTAssertNil(object["unknown"])
        XCTAssertEqual(object["name"] as? String, "Кофе")
        XCTAssertEqual(object["price"] as? Int, 300)
        XCTAssertEqual(object["kind"] as? String, "item")
    }

    // MARK: - Клиентское превью долей (зеркало серверного DeriveShares)

    /// Полный чек из Overview: пицца поровну + баурсаки веса 5/3/2 + вино (Маша
    /// фикс 500, остальное поровну) + сбор 10% proportional. Суммы сходятся до рубля.
    func testDerivedSharesFullReceipt() throws {
        // user 1 — «я», user 2 — Лёха, user 3 — Маша.
        let items: [OperationItem] = [
            OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
                ItemShare(userId: 3, weight: 1),
            ]),
            OperationItem(name: "Баурсаки", price: 500, qty: 10, shares: [
                ItemShare(userId: 2, weight: 5),
                ItemShare(userId: 1, weight: 3),
                ItemShare(userId: 3, weight: 2),
            ]),
            OperationItem(name: "Вино", price: 3000, qty: 1, shares: [
                ItemShare(userId: 3, weight: 1, amount: 500),
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
            OperationItem(
                name: "Сервисный сбор", price: 470, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]

        let result = try XCTUnwrap(items.derivedShares())
        XCTAssertEqual(result.total, 5170)
        XCTAssertEqual(result.shares, [1: 1980, 2: 2090, 3: 1100])
        XCTAssertEqual(result.shares.values.reduce(0, +), result.total)
    }

    /// Неровное деление: остаток по одному достаётся получателям с меньшим userId
    /// (детерминированный tie-break, как в серверном `splitByWeight`).
    func testDerivedSharesUnevenRemainder() throws {
        let items = [
            OperationItem(name: "Такси", price: 100, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
                ItemShare(userId: 3, weight: 1),
            ]),
        ]
        let result = try XCTUnwrap(items.derivedShares())
        XCTAssertEqual(result.shares, [1: 34, 2: 33, 3: 33])
        XCTAssertEqual(result.total, 100)
    }

    /// Микс фикс + вес: снимается фикс, остаток делится по весам.
    func testDerivedSharesFixedPlusWeighted() throws {
        let items = [
            OperationItem(name: "Вино", price: 3000, qty: 1, shares: [
                ItemShare(userId: 3, weight: 1, amount: 500),
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ]),
        ]
        let result = try XCTUnwrap(items.derivedShares())
        XCTAssertEqual(result.shares, [1: 1250, 2: 1250, 3: 500])
    }

    /// Перебор фиксов над ценой позиции → превью невалидно (nil).
    func testDerivedSharesOverAllocatedIsNil() {
        let items = [
            OperationItem(name: "Позиция", price: 100, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1, amount: 80),
                ItemShare(userId: 2, weight: 1, amount: 40),
            ]),
        ]
        XCTAssertNil(items.derivedShares())
    }

    /// Надбавка с нулевой ценой → превью невалидно (nil).
    func testDerivedSharesSurchargeWithoutPriceIsNil() {
        let items = [
            OperationItem(name: "Кофе", price: 300, qty: 1, shares: [ItemShare(userId: 1, weight: 1)]),
            OperationItem(
                name: "Сбор", price: 0, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge, split: OperationItem.splitEqually
            ),
        ]
        XCTAssertNil(items.derivedShares())
    }

    // MARK: - Офлайн-раундтрип outbox (позиции переживают enqueue → flush → API)

    func testOutboxSyncSendsItemsThroughSeam() async throws {
        let fileURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("outbox-items-\(UUID().uuidString)", isDirectory: true)
            .appendingPathComponent("outbox.json")
        defer { try? FileManager.default.removeItem(at: fileURL.deletingLastPathComponent()) }

        let store = OutboxStore(fileURL: fileURL)
        let items = [
            OperationItem(name: "Пицца", price: 1200, qty: 1, shares: [
                ItemShare(userId: 100, weight: 1),
                ItemShare(userId: 200, weight: 1),
            ]),
        ]
        let payload = OutboxPayload(
            description: "Ужин",
            sum: 1200,
            donorId: 100,
            recipientIds: nil,
            recipientSums: [
                RecipientSum(userId: 100, sum: 600),
                RecipientSum(userId: 200, sum: 600),
            ],
            items: items
        )
        let entry = store.add(roomId: "room1", payload: payload)

        let fake = FakeOperationAPI()
        let syncedAny = await store.sync(api: fake)

        XCTAssertTrue(syncedAny)
        // Успешная отправка удаляет запись из очереди.
        XCTAssertTrue(store.entries.isEmpty)
        // Позиции доехали до вызова API без потерь.
        XCTAssertEqual(fake.addCalls.count, 1)
        let call = try XCTUnwrap(fake.addCalls.first)
        XCTAssertEqual(call.roomId, "room1")
        XCTAssertEqual(call.items, items)
        // clientOpId = localId записи (идемпотентность досылки).
        XCTAssertEqual(call.clientOpId, entry.localId.uuidString)
    }

    /// Позиции переживают перезапись файла outbox (enqueue → flush → перечитка).
    func testOutboxItemsSurvivePersistence() throws {
        let fileURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("outbox-persist-\(UUID().uuidString)", isDirectory: true)
            .appendingPathComponent("outbox.json")
        defer { try? FileManager.default.removeItem(at: fileURL.deletingLastPathComponent()) }

        let items = [
            OperationItem(name: "Кофе", price: 300, qty: 1, shares: [ItemShare(userId: 1, weight: 1)]),
        ]
        let payload = OutboxPayload(
            description: "Кофе", sum: 300, donorId: 1,
            recipientIds: [1], recipientSums: nil, items: items
        )

        let store = OutboxStore(fileURL: fileURL)
        store.add(roomId: "room1", payload: payload)
        store.waitForPendingWrites()

        let reloaded = OutboxStore(fileURL: fileURL)
        XCTAssertEqual(reloaded.entries.first?.payload?.items, items)
    }
}

// MARK: - Фейк write-path API (шов OperationAPI)

/// Фейк `OperationAPI` для теста офлайн-раундтрипа: записывает вызовы и возвращает
/// заглушку операции. Реальный `APIClient` подставить нельзя (final + private URLSession).
private final class FakeOperationAPI: OperationAPI {
    struct AddCall {
        let roomId: String
        let items: [OperationItem]?
        let clientOpId: String?
    }
    struct UpdateCall {
        let roomId: String
        let operationId: String
        let items: [OperationItem]?
    }

    private(set) var addCalls: [AddCall] = []
    private(set) var updateCalls: [UpdateCall] = []
    private(set) var deletedOperationIds: [String] = []

    func addOperation(
        roomId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]?,
        clientOpId: String?
    ) async throws -> Operation {
        addCalls.append(AddCall(roomId: roomId, items: items, clientOpId: clientOpId))
        return Self.stubOperation(id: clientOpId ?? "op", description: description, sum: sum, donorId: donorId)
    }

    func updateOperation(
        roomId: String,
        operationId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]?
    ) async throws -> Operation {
        updateCalls.append(UpdateCall(roomId: roomId, operationId: operationId, items: items))
        return Self.stubOperation(id: operationId, description: description, sum: sum, donorId: donorId)
    }

    func deleteOperation(roomId: String, operationId: String) async throws {
        deletedOperationIds.append(operationId)
    }

    private static func stubOperation(id: String, description: String, sum: Int, donorId: Int) -> Operation {
        Operation(
            id: id,
            description: description,
            sum: sum,
            isDebtRepayment: false,
            donor: User(id: donorId, username: nil, displayName: "Донор"),
            recipients: [],
            splitType: .byExactAmount,
            createdAt: Date(timeIntervalSince1970: 1_780_000_000),
            files: nil
        )
    }
}
