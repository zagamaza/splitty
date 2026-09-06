import Foundation
import XCTest
@testable import Splitty

/// Разбор полей копеек: справочник валют и группа.
///
/// Главное тут не «новые поля читаются», а «их ОТСУТСТВИЕ не ломает клиент».
/// Сервер могут откатить на прежнюю версию, и тогда ни `fractional`, ни
/// `fractionalInput` в ответе не будет вовсе. Единственное честное поведение —
/// считать, что копейки выключены и дроби запрещены.
final class MoneyFractionalDecodingTests: XCTestCase {
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private func decodeCurrency(_ json: String) throws -> CurrencyInfo {
        try decoder.decode(CurrencyInfo.self, from: Data(json.utf8))
    }

    func testDecodesCurrencyWithScaleFields() throws {
        let currency = try decodeCurrency("""
        {"code": "USD", "symbol": "$", "flag": "🇺🇸",
         "fractional": true, "supportsFraction": true, "fractionalInput": true}
        """)
        XCTAssertTrue(currency.fractional)
        XCTAssertTrue(currency.supportsFraction)
        XCTAssertTrue(currency.fractionalInput)
    }

    /// Иена: дробной части нет — переключатель копеек не показывается вовсе.
    func testDecodesCurrencyWithoutMinorUnit() throws {
        let currency = try decodeCurrency("""
        {"code": "JPY", "symbol": "¥", "flag": "🇯🇵",
         "fractional": false, "supportsFraction": false, "fractionalInput": false}
        """)
        XCTAssertFalse(currency.supportsFraction)
    }

    /// Ответ сервера, откатанного на прежнюю версию: новых полей нет вовсе.
    func testDecodesCurrencyFromOldServer() throws {
        let currency = try decodeCurrency("""
        {"code": "RUB", "symbol": "₽", "flag": "🇷🇺"}
        """)
        XCTAssertFalse(currency.fractional)
        XCTAssertFalse(currency.supportsFraction, "без признака переключатель обязан быть скрыт")
        XCTAssertFalse(currency.fractionalInput, "отсутствие признака читается как запрет, а не как разрешение")
    }

    private func roomJSON(extra: String) -> String {
        """
        {
          "id": "65abc",
          "name": "Стамбул",
          "createdAt": "2026-09-06T03:00:00Z",
          "isArchived": false,
          "members": [],
          "currency": "USD",
          \(extra)
          "totalSpent": 2080,
          "mySpent": 1040,
          "myBalance": 0,
          "debts": [],
          "operations": [],
          "seenThrough": "2026-09-06T03:00:00Z"
        }
        """
    }

    func testDecodesRoomFractional() throws {
        let room = try decoder.decode(
            RoomDetail.self,
            from: Data(roomJSON(extra: "\"fractional\": true,").utf8)
        )
        XCTAssertTrue(room.fractional)
    }

    /// Группа от прежнего сервера: признака нет — считаем, что копеек нет.
    func testDecodesRoomFromOldServer() throws {
        let room = try decoder.decode(RoomDetail.self, from: Data(roomJSON(extra: "").utf8))
        XCTAssertFalse(room.fractional)
    }

    /// Точная доля приходит отдельным полем; старое `sum` — округлённая
    /// проекция, и клиент обязан предпочитать минорное.
    func testDecodesOperationMinorSums() throws {
        let operation = try decoder.decode(Splitty.Operation.self, from: Data("""
        {
          "id": "65def",
          "description": "Ужин",
          "sum": 21,
          "sumMinor": 2080,
          "isDebtRepayment": false,
          "donor": {"id": 10, "displayName": "Загир"},
          "recipients": [
            {"user": {"id": 10, "displayName": "Загир"}, "sum": 11, "sumMinor": 1040},
            {"user": {"id": 11, "displayName": "Алмаз"}, "sum": 10, "sumMinor": 1040}
          ],
          "createdAt": "2026-09-06T03:00:00Z"
        }
        """.utf8))
        XCTAssertEqual(operation.sumMinor, 2080)
        XCTAssertEqual(operation.sum, 21, "старое поле остаётся проекцией")
        XCTAssertEqual(operation.recipients.first?.sumMinor, 1040)
    }

    /// Операция от прежнего сервера: минорных полей нет, и это не ошибка.
    func testDecodesOperationWithoutMinorSums() throws {
        let operation = try decoder.decode(Splitty.Operation.self, from: Data("""
        {
          "id": "65def",
          "description": "Ужин",
          "sum": 100,
          "isDebtRepayment": false,
          "donor": {"id": 10, "displayName": "Загир"},
          "recipients": [{"user": {"id": 10, "displayName": "Загир"}, "sum": 100}],
          "createdAt": "2026-09-06T03:00:00Z"
        }
        """.utf8))
        XCTAssertNil(operation.sumMinor)
        XCTAssertEqual(operation.sum, 100)
    }
}
