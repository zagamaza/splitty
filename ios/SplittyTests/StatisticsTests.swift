import Foundation
import XCTest
@testable import Splitty

/// Разбор нового контракта GET /rooms/{id}/statistics (дашборд «Итоги»)
/// и контракта валют: currency в комнатах, roomCurrency в активности,
/// totalsByCurrency у друзей, справочник GET /currencies.
final class StatisticsTests: XCTestCase {
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    // MARK: Statistics

    private let statisticsJSON = """
    {
      "currency": "USD",
      "totalSpent": 4200,
      "monthSpent": 1300,
      "byDay": [
        {"date": "2026-07-04", "sum": 500},
        {"date": "2026-07-05", "sum": 800}
      ],
      "byMonth": [
        {"month": "2026-02", "sum": 0},
        {"month": "2026-03", "sum": 700},
        {"month": "2026-04", "sum": 0},
        {"month": "2026-05", "sum": 1100},
        {"month": "2026-06", "sum": 1100},
        {"month": "2026-07", "sum": 1300}
      ],
      "operationCount": 12,
      "paidByMember": [
        {"user": {"id": 10, "username": "zagir", "displayName": "Загир"}, "sum": 3000},
        {"user": {"id": 20, "username": null, "displayName": "Алмаз"}, "sum": 1200}
      ],
      "shareByMember": [
        {"user": {"id": 10, "username": "zagir", "displayName": "Загир"}, "sum": 2100},
        {"user": {"id": 20, "username": null, "displayName": "Алмаз"}, "sum": 2100}
      ],
      "topOperations": [
        {
          "id": "65abc",
          "description": "Ужин",
          "sum": 1500,
          "donor": {"id": 10, "username": "zagir", "displayName": "Загир"},
          "createdAt": "2026-07-05T12:00:00Z"
        }
      ]
    }
    """

    func testDecodesStatistics() throws {
        let stats = try decoder.decode(Statistics.self, from: Data(statisticsJSON.utf8))
        XCTAssertEqual(stats.currency, "USD")
        XCTAssertEqual(stats.totalSpent, 4200)
        XCTAssertEqual(stats.monthSpent, 1300)

        XCTAssertEqual(stats.byDay.count, 2)
        XCTAssertEqual(stats.byDay[0].date, "2026-07-04")
        XCTAssertEqual(stats.byDay[0].sum, 500)

        // byMonth: 6 календарных месяцев включая текущий, ascending, с нулями.
        XCTAssertEqual(stats.byMonth.count, 6)
        XCTAssertEqual(stats.byMonth.first, MonthlySum(month: "2026-02", sum: 0))
        XCTAssertEqual(stats.byMonth.last, MonthlySum(month: "2026-07", sum: 1300))
        XCTAssertEqual(stats.operationCount, 12)

        XCTAssertEqual(stats.paidByMember.count, 2)
        XCTAssertEqual(stats.paidByMember[0].user.id, 10)
        XCTAssertEqual(stats.paidByMember[0].sum, 3000)
        XCTAssertEqual(stats.shareByMember.map(\.sum), [2100, 2100])

        XCTAssertEqual(stats.topOperations.count, 1)
        XCTAssertEqual(stats.topOperations[0].id, "65abc")
        XCTAssertEqual(stats.topOperations[0].description, "Ужин")
        XCTAssertEqual(stats.topOperations[0].sum, 1500)
        XCTAssertEqual(stats.topOperations[0].donor.displayName, "Загир")
    }

    func testDecodesStatisticsWithoutByMonthAndOperationCount() throws {
        // Старый офлайн-кеш / ещё не обновлённый бэкенд: byMonth и
        // operationCount отсутствуют — дефолты []/0, а не ошибка декодирования.
        let legacyJSON = """
        {
          "currency": "RUB",
          "totalSpent": 100,
          "monthSpent": 100,
          "byDay": [],
          "paidByMember": [],
          "shareByMember": [],
          "topOperations": []
        }
        """
        let stats = try decoder.decode(Statistics.self, from: Data(legacyJSON.utf8))
        XCTAssertEqual(stats.byMonth, [])
        XCTAssertEqual(stats.operationCount, 0)
        XCTAssertEqual(stats.totalSpent, 100)
    }

    func testDailySumDayParsing() {
        // «2026-07-05» — локальная дата (не RFC3339): парсится в начало суток.
        let daily = DailySum(date: "2026-07-05", sum: 100)
        let day = daily.day
        XCTAssertNotNil(day)
        if let day {
            let parts = Calendar.current.dateComponents([.year, .month, .day], from: day)
            XCTAssertEqual(parts.year, 2026)
            XCTAssertEqual(parts.month, 7)
            XCTAssertEqual(parts.day, 5)
        }
        XCTAssertNil(DailySum(date: "битая строка", sum: 1).day)
    }

    // MARK: Валюта в комнатах, активности, друзьях

    func testDecodesRoomSummaryWithCurrency() throws {
        let json = """
        {
          "id": "65fff",
          "name": "Бали",
          "createdAt": "2026-07-01T10:00:00Z",
          "isArchived": false,
          "members": [{"id": 10, "username": null, "displayName": "Загир"}],
          "memberCount": 1,
          "currency": "IDR",
          "totalSpent": 2500000,
          "myBalance": -100000
        }
        """
        let room = try decoder.decode(RoomSummary.self, from: Data(json.utf8))
        XCTAssertEqual(room.currency, "IDR")
        XCTAssertEqual(room.myBalance, -100000)
    }

    func testDecodesActivityItemWithRoomCurrency() throws {
        let json = """
        {
          "roomId": "65fff",
          "roomName": "Бали",
          "roomCurrency": "EUR",
          "operation": {
            "id": "65abc",
            "description": "Завтрак",
            "sum": 30,
            "isDebtRepayment": false,
            "donor": {"id": 10, "username": null, "displayName": "Загир"},
            "recipients": [
              {"user": {"id": 10, "username": null, "displayName": "Загир"}, "sum": 30}
            ],
            "splitType": "equally",
            "createdAt": "2026-07-05T12:00:00Z"
          }
        }
        """
        let item = try decoder.decode(ActivityItem.self, from: Data(json.utf8))
        XCTAssertEqual(item.roomCurrency, "EUR")
        XCTAssertEqual(item.operation.sum, 30)
    }

    func testDecodesFriendBalanceWithTotalsByCurrency() throws {
        // Поля total больше нет — нетто по валютам в totalsByCurrency,
        // у комнатной разбивки — валюта комнаты.
        let json = """
        {
          "user": {"id": 20, "username": "almaz", "displayName": "Алмаз"},
          "totalsByCurrency": [
            {"currency": "RUB", "sum": 500},
            {"currency": "USD", "sum": -1200}
          ],
          "rooms": [
            {"roomId": "1", "roomName": "Стамбул", "currency": "RUB", "balance": 500},
            {"roomId": "2", "roomName": "Бали", "currency": "USD", "balance": -1200}
          ]
        }
        """
        let friend = try decoder.decode(FriendBalance.self, from: Data(json.utf8))
        XCTAssertEqual(friend.totalsByCurrency, [
            CurrencySum(currency: "RUB", sum: 500),
            CurrencySum(currency: "USD", sum: -1200),
        ])
        XCTAssertEqual(friend.rooms.map(\.currency), ["RUB", "USD"])
        // Основная валюта — наибольший |суммы|.
        XCTAssertEqual(friend.totals.first, CurrencySum(currency: "USD", sum: -1200))
    }

    func testDecodesCurrencyDirectory() throws {
        let json = """
        [
          {"code": "RUB", "symbol": "₽", "flag": "🇷🇺"},
          {"code": "USD", "symbol": "$", "flag": "🇺🇸"}
        ]
        """
        let currencies = try decoder.decode([CurrencyInfo].self, from: Data(json.utf8))
        XCTAssertEqual(currencies.count, 2)
        XCTAssertEqual(currencies[0].code, "RUB")
        XCTAssertEqual(currencies[0].symbol, "₽")
        XCTAssertEqual(currencies[1].flag, "🇺🇸")
    }
}
