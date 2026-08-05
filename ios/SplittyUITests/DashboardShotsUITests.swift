import XCTest

/// Скриншоты дашборда «Итоги» с прокруткой (для визуальной проверки).
final class DashboardShotsUITests: XCTestCase {
    func testDashboardScroll() throws {
        let app = makeApp()
        app.launch()
        // Свой логин обязателен: класс запускается на чистой установке и по
        // алфавиту раньше DemoFlowUITests — сессии от него тут не бывает.
        loginIfNeeded(app)

        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10))
        app.staticTexts["Поездка в Стамбул"].tap()
        let totals = app.buttons["Итоги"]
        XCTAssertTrue(totals.waitForExistence(timeout: 10))
        totals.tap()
        XCTAssertTrue(app.staticTexts["Траты по дням"].waitForExistence(timeout: 10))

        for step in 1...4 {
            app.swipeUp()
            Thread.sleep(forTimeInterval: 0.6)
            let attachment = XCTAttachment(screenshot: app.screenshot())
            attachment.name = "дашборд-скролл-\(step)"
            attachment.lifetime = .keepAlways
            add(attachment)
        }
    }
}
