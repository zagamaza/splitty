import XCTest
@testable import Splitty

/// Даты следуют языку приложения.
///
/// Форматтеры были прибиты к русской локали: человек, выбравший английский,
/// видел «5 июл» вместо «Jul 5». Хуже того, колонка даты разбирала эту строку
/// по пробелу и брала первую часть как число — на английском там месяц.
final class DateFmtLocaleTests: XCTestCase {

    /// 5 июля 2026 года по местному времени устройства (число сравниваем с ним
    /// же — иначе тест зависел бы от часового пояса машины).
    private let date: Date = {
        var components = DateComponents()
        components.year = 2026
        components.month = 7
        components.day = 5
        components.hour = 12
        return Calendar.current.date(from: components) ?? Date()
    }()

    private var expectedDay: String {
        String(Calendar.current.component(.day, from: date))
    }

    override func tearDown() {
        DateFmt.localeOverride = nil
        super.tearDown()
    }

    func testDayAndMonthDoNotDependOnWordOrder() {
        DateFmt.localeOverride = Locale(identifier: "en_US")
        XCTAssertEqual(DateFmt.day(date), expectedDay, "в колонке даты вместо числа оказался месяц")
        XCTAssertFalse(DateFmt.month(date).isEmpty)

        DateFmt.localeOverride = Locale(identifier: "ru_RU")
        XCTAssertEqual(DateFmt.day(date), expectedDay)
    }

    func testMonthFollowsLocale() {
        DateFmt.localeOverride = Locale(identifier: "en_US")
        let english = DateFmt.month(date)
        DateFmt.localeOverride = Locale(identifier: "ru_RU")
        let russian = DateFmt.month(date)

        XCTAssertNotEqual(english, russian, "месяц не меняется вместе с языком приложения")
    }

    func testMonthYearFollowsLocale() {
        DateFmt.localeOverride = Locale(identifier: "en_US")
        let english = DateFmt.monthYear(date)
        DateFmt.localeOverride = Locale(identifier: "ru_RU")
        let russian = DateFmt.monthYear(date)

        XCTAssertNotEqual(english, russian, "заголовок месяца остался на одном языке")
    }

    func testDayMonthKeepsLocaleOrder() {
        DateFmt.localeOverride = Locale(identifier: "en_US")
        let english = DateFmt.dayMonth(date)
        XCTAssertTrue(english.hasPrefix("Jul"),
                      "английский порядок «месяц число» не соблюдён: \(english)")
    }
}
