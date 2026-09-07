import XCTest
@testable import Splitty

/// Дробные суммы: показ и разбор ввода.
///
/// Ошибка здесь молчит: сумма 20,80 превращается в 21 и расходится с сервером,
/// который хранит точное значение.
final class MoneyFractionTests: XCTestCase {

    override func setUp() {
        super.setUp()
        MoneyLocale.override = Locale(identifier: "ru_RU")
    }

    override func tearDown() {
        MoneyLocale.override = nil
        super.tearDown()
    }

    // MARK: Показ

    /// Форматтер разделяет число и символ НЕразрывным пробелом — в сравнении
    /// он неотличим от обычного, и тест падал бы на невидимой разнице.
    private func plain(_ text: String) -> String {
        text.replacingOccurrences(of: "\u{00a0}", with: " ")
            .replacingOccurrences(of: "\u{202f}", with: " ")
    }

    func testFractionShownOnlyWhenPresent() {
        XCTAssertEqual(plain(money(minor: 2080, currency: "USD")), "20,80 $", "копейки потерялись")
        XCTAssertEqual(plain(money(minor: 2100, currency: "USD")), "21 $", "ровная сумма показана с нулями")
    }

    func testWholeRoomUnaffected() {
        // В тусе без копеек нецелых сумм не возникает — показ прежний.
        XCTAssertEqual(money(minor: 123456700, currency: "RUB"), money(1234567, currency: "RUB"))
    }

    // MARK: Разбор ввода

    func testParsesBothSeparators() {
        XCTAssertEqual(minorFromInput("20,80"), 2080, "запятая с русской клавиатуры не принята")
        XCTAssertEqual(minorFromInput("20.80"), 2080, "точка с цифровой панели не принята")
    }

    func testSingleFractionDigitIsTenths() {
        XCTAssertEqual(minorFromInput("20,8"), 2080, "20,8 — это 80 копеек, а не 8")
    }

    func testWholeNumber() {
        XCTAssertEqual(minorFromInput("21"), 2100)
    }

    func testRejectsNonSums() {
        XCTAssertNil(minorFromInput(""), "пустая строка — не сумма")
        XCTAssertNil(minorFromInput("20,805"), "три знака после разделителя — не сумма")
        XCTAssertNil(minorFromInput("20,,8"), "два разделителя — не сумма")
        XCTAssertNil(minorFromInput("абв"), "буквы — не сумма")
        XCTAssertNil(minorFromInput("20,"), "разделитель без дробной части — не сумма")
    }

    // MARK: Подготовка поля ввода

    func testInputTextRoundTrips() {
        for minor in [2080, 2100, 1, 99, 100, 123456] {
            guard let parsed = minorFromInput(inputTextFromMinor(minor)) else {
                return XCTFail("не разобрали обратно \(minor)")
            }
            XCTAssertEqual(parsed, minor, "поле ввода потеряло точность на \(minor)")
        }
    }

    func testInputTextKeepsTrailingZero() {
        XCTAssertEqual(inputTextFromMinor(2080), "20,80")
        XCTAssertEqual(inputTextFromMinor(2008), "20,08", "ведущий ноль в копейках потерян")
        XCTAssertEqual(inputTextFromMinor(2100), "21", "ровная сумма показана с нулями")
    }
}
