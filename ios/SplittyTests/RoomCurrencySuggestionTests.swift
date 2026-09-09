import XCTest
@testable import Splitty

/// Подстановка валюты на экране создания тусы.
///
/// Валюта — не вопрос, а подтверждение: чаще всего следующая туса заводится в
/// той же валюте, что и предыдущая, а у первой лучший ориентир — регион.
final class RoomCurrencySuggestionTests: XCTestCase {
    private let available = ["RUB", "USD", "EUR", "JPY", "CNY", "KRW", "BRL", "IDR", "KZT", "UZS"]

    func testRecentRoomWins() {
        XCTAssertEqual(
            suggestedRoomCurrency(recent: "EUR", region: "USD", available: available),
            "EUR"
        )
    }

    func testRegionUsedWithoutRecent() {
        XCTAssertEqual(
            suggestedRoomCurrency(recent: nil, region: "USD", available: available),
            "USD"
        )
    }

    func testUnsupportedRegionFallsBackToDefault() {
        // Регион может дать валюту, которой нет в справочнике: молча падаем на
        // умолчание, а не показываем пустую строку.
        XCTAssertEqual(
            suggestedRoomCurrency(recent: nil, region: "CHF", available: available),
            "RUB"
        )
    }

    func testUnsupportedRecentFallsThroughToRegion() {
        XCTAssertEqual(
            suggestedRoomCurrency(recent: "CHF", region: "EUR", available: available),
            "EUR"
        )
    }

    func testNoHintsAtAll() {
        XCTAssertEqual(
            suggestedRoomCurrency(recent: nil, region: nil, available: available),
            "RUB"
        )
    }

    func testCodeCaseDoesNotMatter() {
        XCTAssertEqual(
            suggestedRoomCurrency(recent: "usd", region: nil, available: available),
            "USD"
        )
    }
}
