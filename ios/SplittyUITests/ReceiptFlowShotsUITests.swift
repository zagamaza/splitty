import XCTest

/// Кадры сценариев «Общего чека», которых нет в `ReceiptShotsUITests`:
/// приветствие и вход, ИИ-черновик из нескольких позиций с ручной правкой
/// долей, частичный и крупный платёж в расчёте. Как и там, вид не утверждается —
/// его смотрит человек; тест падает, только если экран не открылся.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true, AI_FAKE_PARSER=true,
/// AI_FREE_DAILY_QUOTA=-1 и `make seed`. Приветствие снимается только на чистой
/// установке: перед `testWelcomeAndLogin` удалить приложение и сбросить
/// связку ключей симулятора. Переменные — как у `ReceiptShotsUITests`.
final class ReceiptFlowShotsUITests: XCTestCase {
    private var app: XCUIApplication!
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var dir: String { env["RECEIPT_SHOTS_DIR"] ?? "/tmp/receipt-shots" }
    private var suffix: String {
        (env["RECEIPT_SHOTS_DARK"] == "1" ? "-dark" : "") + (env["RECEIPT_SHOTS_XL"] == "1" ? "-xl" : "")
            + (env["RECEIPT_SHOTS_DEVICE"].map { "-\($0)" } ?? "")
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = makeApp()
        app.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        if env["RECEIPT_SHOTS_DARK"] == "1" {
            app.launchArguments += ["-splitty.theme", "dark"]
        }
        if env["RECEIPT_SHOTS_XL"] == "1" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityL"]
        }
    }

    private func shot(_ name: String) {
        sleep(1)
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let url = URL(fileURLWithPath: dir).appendingPathComponent("ios-\(name)\(suffix).png")
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: url)
    }

    func testWelcomeAndLogin() throws {
        app.launch()
        let skip = app.buttons["welcomeSkip"]
        XCTAssertTrue(skip.waitForExistence(timeout: 10), "приветствия нет — установка не чистая")
        shot("t10-01-welcome")
        skip.tap()
        let disclosure = app.buttons["emailLoginDisclosure"]
        XCTAssertTrue(disclosure.waitForExistence(timeout: 5), "экран входа не открылся")
        shot("t10-01-login")
        disclosure.tap()
        XCTAssertTrue(app.textFields["Email"].waitForExistence(timeout: 5), "шторка входа по email не открылась")
        shot("t10-01-login-email")
    }

    func testAIDraftAndManualShares() throws {
        app.launchEnvironment["SPLITTY_FAKE_DICTATION"] = "Ужин: хачапури 1200, вино 2400, чай 300"
        app.launch()
        openRoom()
        let fab = try XCTUnwrap(rightmostHittableButton(app, labeled: "Добавить расход"), "не нашли добавление расхода")
        fab.tap()
        let mic = app.descendants(matching: .any)["Записать голосом"].firstMatch
        XCTAssertTrue(mic.waitForExistence(timeout: 10), "микрофона в пустой форме нет")
        mic.press(forDuration: 0.3)

        let edit = app.buttons["editParticipantAmounts"]
        XCTAssertTrue(edit.waitForExistence(timeout: 15), "черновик не пришёл")
        shot("t10-04-ai-draft")
        app.swipeUp()
        shot("t10-04-ai-draft-scrolled")

        edit.tap()
        let fields = app.textFields.matching(NSPredicate(format: "identifier BEGINSWITH 'amountField.'"))
        XCTAssertTrue(fields.firstMatch.waitForExistence(timeout: 5), "полей сумм участников нет")
        let first = fields.element(boundBy: 0)
        first.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "клавиатура не появилась")
        first.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).doubleTap()
        first.typeText("2000")
        // У цифровой клавиатуры нет «Готово» — прокрутка формы убирает её и
        // заодно показывает поля сумм с правкой.
        app.swipeUp()
        shot("t10-04-ai-manual")
    }

    func testSettlePartialAndLargeAmount() throws {
        app.launch()
        openRoom()
        let settle = app.buttons["settleUpLink"]
        XCTAssertTrue(settle.waitForExistence(timeout: 10), "ссылки расчёта нет")
        bringIntoView(settle)
        settle.tap()
        XCTAssertTrue(app.navigationBars["Записать платёж"].waitForExistence(timeout: 5), "расчёт не открылся")

        let field = app.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        clear(field, app: app, maxLength: 16)
        field.typeText("4000")
        shot("t10-05-settle-partial-keyboard")
        dismissKeyboard(app)
        shot("t10-05-settle-partial")

        clear(field, app: app, maxLength: 16)
        field.typeText("123456789")
        shot("t10-05-settle-large-keyboard")
        XCTAssertEqual(field.value as? String, "123456789", "поле приняло не всю сумму")
        dismissKeyboard(app)
        shot("t10-05-settle-large")
    }

    /// Экраны, которые основной сценарий не задевает: вход по коду, архив,
    /// приглашение, Plus, пароль, настройки уведомлений. Каждый — если открылся.
    func testSecondaryScreens() throws {
        app.launch()
        loginIfNeeded(app)
        let groupsTab = app.buttons["tab.groups"]
        XCTAssertTrue(groupsTab.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        groupsTab.tap()

        let plus = app.buttons["Создать группу или присоединиться по коду"]
        if plus.waitForExistence(timeout: 5) {
            plus.tap()
            let join = app.buttons["Присоединиться по коду"]
            if join.waitForExistence(timeout: 3) {
                join.tap()
                shot("m-11-join")
                closeSheet()
            }
        }
        let archive = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Архив'")).firstMatch
        if archive.waitForExistence(timeout: 5) {
            bringIntoView(archive)
            archive.tap()
            shot("m-04-archive")
            app.navigationBars.buttons.firstMatch.tap()
        }

        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10))
        room.tap()
        let settings = app.buttons["Настройки"]
        if settings.waitForExistence(timeout: 10) {
            settings.tap()
            let add = app.buttons["Добавить"].firstMatch
            if add.waitForExistence(timeout: 5) {
                add.tap()
                shot("m-11-invite-friend")
                let link = app.buttons["Отправить ссылку"]
                if link.waitForExistence(timeout: 3) {
                    link.tap()
                    shot("m-11-invite-link")
                    closeSheet()
                }
                closeSheet()
            }
        }
        for _ in 0..<4 where !app.otherElements["tabBar"].isHittable {
            if app.navigationBars.buttons.firstMatch.isHittable {
                app.navigationBars.buttons.firstMatch.tap()
            } else {
                app.swipeDown(velocity: .fast)
            }
            sleep(1)
        }

        app.buttons["tab.account"].tap()
        let plusRow = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Splitor Plus'")).firstMatch
        if plusRow.waitForExistence(timeout: 5) {
            plusRow.tap()
            shot("m-16-plus")
            closeSheet()
        }
        let notifications = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Уведомления'"))
            .allElementsBoundByIndex.first { $0.frame.minY < app.otherElements["tabBar"].frame.minY - 1 }
        if let notifications {
            bringIntoView(notifications)
            notifications.tap()
            shot("m-15-notif-settings")
            app.navigationBars.buttons.firstMatch.tap()
        }
        let password = app.buttons["Изменить"].firstMatch
        if password.waitForExistence(timeout: 5) {
            bringIntoView(password)
            password.tap()
            shot("m-15-password")
            closeSheet()
        }
    }

    /// Без сервера: данные из кеша с пометкой. Бэкенд останавливают снаружи,
    /// сессия должна остаться от прошлого прогона.
    func testOfflineCachedState() throws {
        app.launch()
        let groupsTab = app.buttons["tab.groups"]
        XCTAssertTrue(groupsTab.waitForExistence(timeout: 10), "нет таб-бара — сессии нет")
        groupsTab.tap()
        shot("m-17-offline-groups")
        app.buttons["tab.activity"].tap()
        shot("m-17-offline-activity")
    }

    private func closeSheet() {
        let cancel = app.buttons["Отмена"].firstMatch
        if cancel.waitForExistence(timeout: 2) && cancel.isHittable {
            cancel.tap()
        } else {
            app.swipeDown(velocity: .fast)
        }
        sleep(1)
    }

    private func openRoom() {
        loginIfNeeded(app)
        let tab = app.buttons["tab.groups"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        tab.tap()
        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10), "группа из seed-данных не появилась")
        room.tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "экран группы не открылся")
    }

    /// Нижняя панель с кнопкой перекрывает ссылку, а isHittable этого не
    /// видит — доводим элемент малыми шагами до верхней половины экрана.
    private func bringIntoView(_ element: XCUIElement) {
        let height = app.windows.firstMatch.frame.height
        let center = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        for _ in 0..<10 {
            let frame = element.exists ? element.frame : .zero
            if element.exists && frame.minY > 120 && frame.maxY < height * 0.6 { break }
            let up = element.exists && frame.maxY >= height * 0.6
            center.press(forDuration: 0.05, thenDragTo: center.withOffset(CGVector(dx: 0, dy: up ? -200 : 200)))
        }
    }
}
