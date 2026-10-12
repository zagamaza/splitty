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

    func testOtherScreens() throws {
        loginIfNeeded(app)
        XCTAssertTrue(app.tabBars.firstMatch.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")

        app.tabBars.buttons["Друзья"].tap()
        shot("10-friends")
        let friend = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Алмаз'")).firstMatch
        if friend.waitForExistence(timeout: 5) {
            friend.tap()
            shot("11-friend")
            app.navigationBars.buttons.firstMatch.tap()
        }

        app.tabBars.buttons["Уведомления"].tap()
        shot("12-activity")

        app.tabBars.buttons["Профиль"].tap()
        shot("13-profile")
        app.swipeUp()
        shot("13-profile-scrolled")

        app.tabBars.buttons["Группы"].tap()
        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10))
        room.tap()
        let op = app.staticTexts["Отель на три ночи"]
        if op.waitForExistence(timeout: 5) {
            op.tap()
            shot("09-operation")
            app.navigationBars.buttons.firstMatch.tap()
        }
        app.navigationBars.buttons.firstMatch.tap()

        let plus = app.buttons["Создать группу или присоединиться по коду"]
        if plus.waitForExistence(timeout: 5) {
            plus.tap()
            let create = app.buttons["Создать группу"]
            if create.waitForExistence(timeout: 3) {
                create.tap()
                shot("14-create-group")
            }
        }
    }
}
