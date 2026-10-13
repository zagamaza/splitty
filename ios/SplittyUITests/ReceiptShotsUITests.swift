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
        let tab = app.buttons["tab.groups"]
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
        XCTAssertTrue(app.otherElements["tabBar"].waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")

        app.buttons["tab.friends"].tap()
        shot("10-friends")
        let friend = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Алмаз'")).firstMatch
        if friend.waitForExistence(timeout: 5) {
            friend.tap()
            shot("11-friend")
            app.navigationBars.buttons.firstMatch.tap()
        }

        app.buttons["tab.activity"].tap()
        shot("12-activity")

        app.buttons["tab.account"].tap()
        shot("13-profile")
        app.swipeUp()
        shot("13-profile-scrolled")

        app.buttons["tab.groups"].tap()
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
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 5))
        shot("02-groups-return")

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

    /// Блок остатка на экране тусы: один кредитор («Кому: Алмаз») и несколько
    /// («По группе»), вкладка балансов и список долгов расчёта. Вторая туса
    /// заводится руками через API (два кредитора), без неё кадр пропускается.
    func testGroupMoney() throws {
        loginIfNeeded(app)
        XCTAssertTrue(app.otherElements["tabBar"].waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        app.buttons["tab.groups"].tap()
        for (room, tag) in [("Поездка в Стамбул", "one"), ("Выходные на даче", "many")] {
            let row = app.staticTexts[room]
            guard row.waitForExistence(timeout: 5) else { continue }
            // На крупном шрифте строка может оказаться под нижней панелью.
            for _ in 0..<4 where !row.isHittable || row.frame.maxY > app.windows.firstMatch.frame.height * 0.75 {
                app.swipeUp()
            }
            row.tap()
            if !app.buttons["Балансы"].waitForExistence(timeout: 10) { shot("15-money-\(tag)-fail") }
            XCTAssertTrue(app.buttons["Балансы"].exists, "экран группы не открылся")
            shot("15-money-\(tag)")
            app.buttons["Балансы"].tap()
            shot("15-money-\(tag)-balances")
            app.buttons["Операции"].tap()
            let settle = app.buttons["settleUpLink"]
            if settle.waitForExistence(timeout: 5) {
                if !settle.isHittable { app.swipeUp() }
                settle.tap()
                // Один долг сразу открывает форму платежа, несколько — список.
                sleep(1)
                shot("15-money-\(tag)-settle")
                app.swipeDown(velocity: .fast)
                sleep(1)
            }
            app.navigationBars.buttons.firstMatch.tap()
        }
    }

    /// «Итоги» одним листом: верх, середина и низ отчёта на тусе с данными и
    /// пустая туса («Пустая туса» заводится через API; без неё кадр пропускается).
    func testTotalsReport() throws {
        loginIfNeeded(app)
        XCTAssertTrue(app.otherElements["tabBar"].waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        app.buttons["tab.groups"].tap()
        for (room, tag) in [("Поездка в Стамбул", "full"), ("Пустая туса", "empty")] {
            let row = app.staticTexts[room]
            guard row.waitForExistence(timeout: 5) else { continue }
            for _ in 0..<4 where !row.isHittable || row.frame.maxY > app.windows.firstMatch.frame.height * 0.75 {
                app.swipeUp()
            }
            row.tap()
            XCTAssertTrue(app.buttons["Итоги"].waitForExistence(timeout: 10), "экран группы не открылся")
            app.buttons["Итоги"].tap()
            sleep(2)
            shot("16-totals-\(tag)-top")
            if tag == "full" {
                app.swipeUp()
                shot("16-totals-\(tag)-mid")
                for _ in 0..<6 { app.swipeUp() }
                shot("16-totals-\(tag)-bottom")
            }
            app.navigationBars.buttons.firstMatch.tap()
            // Из глубины листа «назад» может не сразу попасть в кнопку.
            if !app.otherElements["tabBar"].waitForExistence(timeout: 3) {
                app.navigationBars.buttons.firstMatch.tap()
            }
        }
    }

    /// Карточка операции одним листом: обычный расход, расход с позициями и
    /// длинными именами, погашение. Данные «Выходных на даче» заводятся через
    /// API; без тусы тест пропускается.
    func testOperationDocuments() throws {
        loginIfNeeded(app)
        XCTAssertTrue(app.otherElements["tabBar"].waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        app.buttons["tab.groups"].tap()
        let room = app.staticTexts["Выходные на даче"]
        guard room.waitForExistence(timeout: 5) else { throw XCTSkip("нет тусы «Выходные на даче»") }
        room.tap()
        XCTAssertTrue(app.buttons["Итоги"].waitForExistence(timeout: 10), "экран группы не открылся")
        let screen = app.windows.firstMatch.frame.height
        for (prefix, tag) in [("Продукты на выходные", "plain"), ("Ужин в грузинском", "items"), ("Банкет", "long"), ("Вы заплатили", "repayment")] {
            let row = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
            for _ in 0..<6 where !row.exists || !row.isHittable || row.frame.maxY > screen * 0.75 {
                app.swipeUp()
            }
            guard row.exists else { continue }
            row.tap()
            sleep(2)
            shot("17-op-\(tag)-top")
            for _ in 0..<5 { app.swipeUp() }
            shot("17-op-\(tag)-bottom")
            app.navigationBars.buttons.firstMatch.tap()
            sleep(1)
            for _ in 0..<4 { app.swipeDown() }
        }
    }

    /// Витрина чека без сервера (`SPLITTY_RECEIPT_SHOWCASE`, только Debug):
    /// дробная цена шире листа, длинное название, сбор — отдельным листом и
    /// встроенным разделом.
    func testReceiptShowcase() throws {
        app.terminate()
        app = makeApp()
        app.launchEnvironment["SPLITTY_RECEIPT_SHOWCASE"] = "1"
        if env["RECEIPT_SHOTS_XL"] == "1" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityL"]
        }
        app.launch()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Хачапури'")).firstMatch
            .waitForExistence(timeout: 10), "витрина не открылась")
        shot("18-showcase-top")
        app.swipeUp()
        shot("18-showcase-mid")
        for _ in 0..<4 { app.swipeUp() }
        shot("18-showcase-bottom")
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
        let tabs = app.otherElements["tabBar"]
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
