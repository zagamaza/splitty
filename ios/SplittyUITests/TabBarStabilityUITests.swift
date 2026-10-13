import XCTest

/// «+» в нижней строке не должен ездить при смене вкладки: точка выбранной,
/// жирное начертание и гаснущий на «Уведомлениях» бейдж меняли ширины пунктов,
/// гибкие зазоры перераспределялись, и кнопка сдвигалась на каждом переходе.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true и `make seed`.
final class TabBarStabilityUITests: XCTestCase {
    func testAddButtonStaysPutAcrossTabs() throws {
        continueAfterFailure = false
        let app = makeApp()
        app.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        app.launch()
        loginIfNeeded(app)

        let add = app.otherElements["tabBar"].buttons["Добавить расход"]
        XCTAssertTrue(add.waitForExistence(timeout: 10), "нет «+» в нижней строке")
        let origin = add.frame.origin

        for tab in ["tab.activity", "tab.friends", "tab.account", "tab.groups"] {
            app.buttons[tab].tap()
            sleep(1)
            let moved = add.frame.origin
            XCTAssertEqual(moved.x, origin.x, accuracy: 0.5, "«+» сдвинулся на вкладке \(tab)")
            XCTAssertEqual(moved.y, origin.y, accuracy: 0.5, "«+» сдвинулся на вкладке \(tab)")
        }
    }
}
