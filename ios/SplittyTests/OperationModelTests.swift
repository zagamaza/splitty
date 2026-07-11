import Foundation
import XCTest
@testable import Splitty

/// Модель операции по контракту API v2: recipients — пары {user, sum}
/// (хранимые доли в целых рублях), поле splitType; позиции пользователя
/// считаются из хранимых сумм, а не пересчётом поровну.
final class OperationModelTests: XCTestCase {
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    // `Splitty.Operation` — полная квалификация: в тестовом таргете имя
    // конфликтует с `Foundation.Operation` (NSOperation).
    private func decodeOperation(_ json: String) throws -> Splitty.Operation {
        try decoder.decode(Splitty.Operation.self, from: Data(json.utf8))
    }

    // MARK: Декодирование

    func testDecodesByExactAmountOperation() throws {
        let operation = try decodeOperation("""
        {
          "id": "65abc",
          "description": "Ужин",
          "sum": 1000,
          "isDebtRepayment": false,
          "donor": {"id": 10, "username": "zagir", "displayName": "Загир"},
          "recipients": [
            {"user": {"id": 10, "username": "zagir", "displayName": "Загир"}, "sum": 700},
            {"user": {"id": 20, "username": null, "displayName": "Алмаз"}, "sum": 300}
          ],
          "splitType": "by_exact_amount",
          "createdAt": "2026-07-05T12:00:00Z",
          "files": []
        }
        """)
        XCTAssertEqual(operation.splitType, .byExactAmount)
        XCTAssertEqual(operation.recipients.count, 2)
        XCTAssertEqual(operation.recipients[0].user.id, 10)
        XCTAssertEqual(operation.recipients[0].sum, 700)
        XCTAssertEqual(operation.recipients[1].user.id, 20)
        XCTAssertEqual(operation.recipients[1].sum, 300)
        // Хранимые доли сходятся с суммой операции.
        XCTAssertEqual(operation.recipients.map(\.sum).reduce(0, +), operation.sum)
    }

    func testDecodesEquallyOperation() throws {
        let operation = try decodeOperation("""
        {
          "id": "65abd",
          "description": "Такси",
          "sum": 100,
          "isDebtRepayment": false,
          "donor": {"id": 10, "username": null, "displayName": "Загир"},
          "recipients": [
            {"user": {"id": 10, "username": null, "displayName": "Загир"}, "sum": 34},
            {"user": {"id": 20, "username": null, "displayName": "Алмаз"}, "sum": 33},
            {"user": {"id": 30, "username": null, "displayName": "Ильдар"}, "sum": 33}
          ],
          "splitType": "equally",
          "createdAt": "2026-07-05T12:00:00Z"
        }
        """)
        XCTAssertEqual(operation.splitType, .equally)
        // Сервер отдаёт канонические целые доли — клиент их НЕ пересчитывает.
        XCTAssertEqual(operation.recipients.map(\.sum), [34, 33, 33])
    }

    func testDecodesRepaymentWithoutSplitType() throws {
        // У погашений splitType отсутствует — поле опциональное.
        let operation = try decodeOperation("""
        {
          "id": "65abe",
          "description": "",
          "sum": 500,
          "isDebtRepayment": true,
          "donor": {"id": 20, "username": null, "displayName": "Алмаз"},
          "recipients": [
            {"user": {"id": 10, "username": null, "displayName": "Загир"}, "sum": 500}
          ],
          "createdAt": "2026-07-05T12:00:00Z"
        }
        """)
        XCTAssertNil(operation.splitType)
        XCTAssertEqual(operation.recipients.first?.sum, 500)
    }

    func testUnknownSplitTypeDecodesAsEquallyLeniently() throws {
        // Незнакомое значение не роняет декодирование списка операций.
        let operation = try decodeOperation("""
        {
          "id": "65abf",
          "description": "Легаси",
          "sum": 100,
          "isDebtRepayment": false,
          "donor": {"id": 10, "username": null, "displayName": "Загир"},
          "recipients": [
            {"user": {"id": 10, "username": null, "displayName": "Загир"}, "sum": 100}
          ],
          "splitType": "something_new",
          "createdAt": "2026-07-05T12:00:00Z"
        }
        """)
        XCTAssertEqual(operation.splitType, .equally)
    }

    // MARK: Позиции из ХРАНИМЫХ сумм

    private func makeOperation(
        sum: Int,
        donorId: Int,
        recipients: [(id: Int, sum: Int)],
        splitType: SplitType? = .byExactAmount
    ) -> Splitty.Operation {
        Splitty.Operation(
            id: "op1",
            description: "Тест",
            sum: sum,
            isDebtRepayment: false,
            donor: User(id: donorId, username: nil, displayName: "Донор"),
            recipients: recipients.map {
                OperationRecipient(
                    user: User(id: $0.id, username: nil, displayName: "У\($0.id)"),
                    sum: $0.sum
                )
            },
            splitType: splitType,
            createdAt: Date(timeIntervalSince1970: 1_780_000_000),
            files: nil
        )
    }

    func testRecipientSumUsesStoredValues() {
        // Неравное деление: доля берётся из хранимой суммы, НЕ из sum/n.
        let operation = makeOperation(sum: 1000, donorId: 10, recipients: [(10, 700), (20, 300)])
        XCTAssertEqual(operation.recipientSum(of: 10), 700)
        XCTAssertEqual(operation.recipientSum(of: 20), 300)
        XCTAssertNil(operation.recipientSum(of: 99))
    }

    func testNetPositionForDonorAmongRecipients() {
        // Донор одолжил всё, кроме СВОЕЙ хранимой доли: 1000 − 700 = 300.
        let operation = makeOperation(sum: 1000, donorId: 10, recipients: [(10, 700), (20, 300)])
        XCTAssertEqual(operation.netPosition(of: 10), 300)
    }

    func testNetPositionForDebtorIsMinusStoredShare() {
        let operation = makeOperation(sum: 1000, donorId: 10, recipients: [(10, 700), (20, 300)])
        XCTAssertEqual(operation.netPosition(of: 20), -300)
    }

    func testNetPositionForDonorNotAmongRecipients() {
        // Донор не делит расход — одолжил всю сумму.
        let operation = makeOperation(sum: 900, donorId: 10, recipients: [(20, 500), (30, 400)])
        XCTAssertEqual(operation.netPosition(of: 10), 900)
    }

    func testNetPositionForNonParticipantIsNil() {
        let operation = makeOperation(sum: 900, donorId: 10, recipients: [(20, 500), (30, 400)])
        XCTAssertNil(operation.netPosition(of: 99))
    }

    func testNetPositionZeroWhenDonorTookWholeSum() {
        // Донор — единственный получатель всей суммы: расчёт (0).
        let operation = makeOperation(sum: 500, donorId: 10, recipients: [(10, 500)])
        XCTAssertEqual(operation.netPosition(of: 10), 0)
    }
}
