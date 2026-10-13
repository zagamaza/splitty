import XCTest

/// Снимки главных экранов редизайна «Общий чек» для сверки с макетами
/// `docs/design/receipt/receipt/*.png`. Ничего не утверждает про вид — его
/// смотрит человек; падает только если экран не открылся.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true и `make seed`.
/// Картинки — в `RECEIPT_SHOTS_DIR` (по умолчанию /tmp/receipt-shots);
/// тёмная тема — `RECEIPT_SHOTS_DARK=1`, крупный текст — `RECEIPT_SHOTS_XL=1`
/// (через xcodebuild — с префиксом `TEST_RUNNER_`).
final class ReceiptShotsUITests: XCTestCase {
    private var app: XCUIApplication!
    private let dir = ProcessInfo.processInfo.environment["RECEIPT_SHOTS_DIR"] ?? "/tmp/receipt-shots"
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var suffix: String {
        (env["RECEIPT_SHOTS_DARK"] == "1" ? "-dark" : "") + (env["RECEIPT_SHOTS_XL"] == "1" ? "-xl" : "")
            + (env["RECEIPT_SHOTS_LANG"].map { "-\($0)" } ?? "")
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = makeApp()
        let lang = env["RECEIPT_SHOTS_LANG"] ?? "ru"
        app.launchArguments += ["-AppleLanguages", "(\(lang))", "-AppleLocale", lang]
        if env["RECEIPT_SHOTS_DARK"] == "1" {
            app.launchArguments += ["-splitty.theme", "dark"]
        }
        if env["RECEIPT_SHOTS_XL"] == "1" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityL"]
        }
        app.launch()
    }

    private func shot(_ name: String) {
        sleep(1)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let device = env["RECEIPT_SHOTS_DEVICE"].map { "-\($0)" } ?? ""
        let url = URL(fileURLWithPath: dir).appendingPathComponent("ios-\(name)\(suffix)\(device).png")
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

        // На крупном шрифте ссылка ниже края экрана: докручиваем до неё и
        // снимаем, только убедившись, что открылся именно расчёт.
        let settle = app.buttons["settleUpLink"]
        if settle.waitForExistence(timeout: 5) {
            // isHittable не видит нижнюю панель с кнопкой поверх ссылки —
            // доводим ссылку малыми шагами до верхней половины экрана.
            let screenHeight = app.windows.firstMatch.frame.height
            let center = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            for _ in 0..<10 {
                let frame = settle.exists ? settle.frame : .zero
                if settle.exists && frame.minY > 120 && frame.maxY < screenHeight * 0.6 { break }
                let up = settle.exists && frame.maxY >= screenHeight * 0.6
                center.press(forDuration: 0.05, thenDragTo: center.withOffset(CGVector(dx: 0, dy: up ? -200 : 200)))
            }
            settle.tap()
            XCTAssertTrue(app.navigationBars["Записать платёж"].waitForExistence(timeout: 5), "расчёт не открылся")
            shot("05-settle")
            app.swipeDown(velocity: .fast)
            for _ in 0..<4 { app.swipeDown() }
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

    /// Проход для других языков: вкладки и разделы — по позиции, а не по
    /// русским подписям. Вход по email — те же поля, у них нет перевода в тесте,
    /// поэтому язык меняется уже после входа (сессия переживает перезапуск).
    func testGroupInLanguage() throws {
        let lang = app.launchArguments
        app.terminate()
        let ru = makeApp()
        ru.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        ru.launch()
        loginIfNeeded(ru)
        ru.terminate()
        app = makeApp()
        app.launchArguments = lang
        app.launch()
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(tabs.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        tabs.buttons.element(boundBy: 1).tap()
        shot("02-groups")
        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10))
        room.tap()
        sleep(2)
        shot("03-group")
    }
}
