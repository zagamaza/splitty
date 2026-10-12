import XCTest

/// Снимки главных экранов редизайна «Общий чек» для сверки с макетами
/// `docs/design/receipt/receipt/*.png`. Ничего не утверждает про вид — его
/// смотрит человек; падает только если экран не открылся.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true и `make seed`.
/// Картинки — в `RECEIPT_SHOTS_DIR` (по умолчанию /tmp/receipt-shots);
/// тёмная тема — `RECEIPT_SHOTS_DARK=1`.
final class ReceiptShotsUITests: XCTestCase {
    private var app: XCUIApplication!
    private let dir = ProcessInfo.processInfo.environment["RECEIPT_SHOTS_DIR"] ?? "/tmp/receipt-shots"
    private var suffix: String { ProcessInfo.processInfo.environment["RECEIPT_SHOTS_DARK"] == "1" ? "-dark" : "" }

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = makeApp()
        app.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        if ProcessInfo.processInfo.environment["RECEIPT_SHOTS_DARK"] == "1" {
            app.launchArguments += ["-AppleInterfaceStyle", "Dark"]
        }
        app.launch()
    }

    private func shot(_ name: String) {
        sleep(1)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let url = URL(fileURLWithPath: dir).appendingPathComponent("ios-\(name)\(suffix).png")
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: url)
    }

    func testMainScreens() throws {
        loginIfNeeded(app)
        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        tab.tap()
        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10), "группа из seed-данных не появилась")
        shot("02-groups")

        room.tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "экран группы не открылся")
        shot("03-group")
        app.swipeUp()
        shot("03-group-scrolled")
        app.swipeDown()

        app.buttons["Балансы"].tap()
        shot("06-balances")
        app.buttons["Итоги"].tap()
        shot("07-totals")
        app.buttons["Настройки"].tap()
        shot("08-settings")
        app.buttons["Операции"].tap()

        let settle = app.buttons["settleUpLink"]
        if settle.waitForExistence(timeout: 5) {
            settle.tap()
            shot("05-settle")
            app.swipeDown(velocity: .fast)
        }

        let add = app.buttons["Добавить расход"].firstMatch
        if add.waitForExistence(timeout: 5) {
            add.tap()
            shot("04-add")
        }
    }
}
