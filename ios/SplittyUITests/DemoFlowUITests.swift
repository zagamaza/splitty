import XCTest

/// Сквозной демо-прогон по основным сценариям против локального бэкенда
/// (http://127.0.0.1:7171, API_DEV_AUTH=true, данные из seed-скрипта).
/// Каждый шаг снимает скриншот-attachment (lifetime .keepAlways).
final class DemoFlowUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = true
        app = XCUIApplication()
        app.launch()
    }

    func testDemoFlow() throws {
        shot("00-запуск")
        loginIfNeeded()

        // --- Группы ---
        tapTab("Группы")
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10),
                      "Группа из seed-данных не появилась")
        shot("01-группы")

        app.staticTexts["Поездка в Стамбул"].tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "Экран группы не открылся")
        sleepShort()
        shot("02-группа-стамбул")

        // --- Добавление расхода (FAB внутри группы) ---
        if let fab = rightmostHittableButton(labeled: "Добавить расход") {
            fab.tap()
            let descField = app.textFields["Описание"]
            if descField.waitForExistence(timeout: 5) {
                descField.tap()
                descField.typeText("Кофе на двоих")
                let sumField = app.textFields["0"].firstMatch
                if sumField.exists {
                    sumField.tap()
                    sumField.typeText("600")
                }
                dismissKeyboard()
                shot("03-новый-расход")
                let save = app.buttons["Сохранить"]
                if save.waitForExistence(timeout: 3), save.isEnabled {
                    save.tap()
                    sleepShort()
                }
            }
            shot("04-группа-после-расхода")
        } else {
            XCTFail("Не нашли кнопку добавления расхода в группе")
        }

        // --- Карточка операции ---
        if app.staticTexts["Ужин в ресторане"].waitForExistence(timeout: 5) {
            app.staticTexts["Ужин в ресторане"].tap()
            sleepShort()
            shot("05-карточка-операции")
            dismissModalOrBack()
        }

        // --- Балансы ---
        if app.buttons["Балансы"].waitForExistence(timeout: 5) {
            app.buttons["Балансы"].tap()
            sleepShort()
            shot("06-балансы")
            dismissModalOrBack()
        }

        // --- Итоги ---
        if app.buttons["Итоги"].waitForExistence(timeout: 5) {
            app.buttons["Итоги"].tap()
            sleepShort()
            shot("07-итоги")
            dismissModalOrBack()
        }

        // --- Погашение долга ---
        if app.buttons["Погасить долг"].waitForExistence(timeout: 5), app.buttons["Погасить долг"].isEnabled {
            app.buttons["Погасить долг"].tap()
            sleepShort()
            shot("08-выбор-долга")
            // Если открылся список «Ваши долги» — выбираем долг Алмаза
            let debtRow = app.buttons.matching(
                NSPredicate(format: "label CONTAINS %@", "должен")
            ).firstMatch
            if debtRow.waitForExistence(timeout: 3) {
                debtRow.tap()
                sleepShort()
            }
            shot("09-записать-платёж")
            let record = app.buttons["Записать платёж"]
            if record.waitForExistence(timeout: 3), record.isEnabled {
                record.tap()
                sleepShort()
                shot("10-после-платежа")
            } else {
                dismissModalOrBack()
            }
        }

        // --- Друзья ---
        goBackToRoot()
        tapTab("Друзья")
        sleepShort()
        shot("11-друзья")
        let friendRow = app.buttons.matching(
            NSPredicate(format: "label BEGINSWITH %@", "Алмаз")
        ).firstMatch
        if friendRow.waitForExistence(timeout: 5) {
            friendRow.tap()
            sleepShort()
            shot("12-друг-алмаз")
            goBackToRoot()
        }

        // --- Активность ---
        tapTab("Активность")
        sleepShort()
        shot("13-активность")

        // --- Профиль ---
        tapTab("Профиль")
        sleepShort()
        shot("14-профиль")
    }

    // MARK: - Хелперы

    private func loginIfNeeded() {
        let idField = app.textFields["Telegram ID"]
        guard idField.waitForExistence(timeout: 5) else { return } // уже залогинены

        idField.tap()
        idField.typeText("100")

        let nameField = app.textFields["Имя"]
        nameField.tap()
        nameField.typeText("Загир")

        dismissKeyboard()
        shot("00-логин-заполнен")

        let loginButton = app.buttons["Войти"]
        XCTAssertTrue(loginButton.isEnabled, "Кнопка «Войти» неактивна")
        loginButton.tap()

        XCTAssertTrue(app.tabBars.firstMatch.waitForExistence(timeout: 15), "Не дождались таб-бара после логина")
    }

    private func tapTab(_ name: String) {
        let tab = app.tabBars.buttons[name]
        if tab.waitForExistence(timeout: 5) {
            tab.tap()
        }
    }

    /// Среди всех hittable-кнопок с данным label выбирает самую правую (FAB, а не центральную кнопку таб-бара).
    private func rightmostHittableButton(labeled label: String) -> XCUIElement? {
        let matches = app.buttons.matching(NSPredicate(format: "label == %@", label))
        var best: XCUIElement?
        for i in 0..<matches.count {
            let e = matches.element(boundBy: i)
            guard e.exists, e.isHittable else { continue }
            if best == nil || e.frame.maxX > best!.frame.maxX { best = e }
        }
        return best
    }

    private func dismissKeyboard() {
        if app.keyboards.count > 0 {
            app.toolbars.buttons["Готово"].exists
                ? app.toolbars.buttons["Готово"].tap()
                : app.swipeDown()
        }
    }

    private func dismissModalOrBack() {
        for title in ["Готово", "Закрыть", "Отмена", "Ок"] {
            let b = app.buttons[title]
            if b.exists, b.isHittable {
                b.tap()
                return
            }
        }
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists, back.isHittable {
            back.tap()
            return
        }
        // Свайп вниз для sheet
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.1))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9))
        start.press(forDuration: 0.05, thenDragTo: end)
    }

    private func goBackToRoot() {
        for _ in 0..<3 {
            let back = app.navigationBars.buttons.element(boundBy: 0)
            guard back.exists, back.isHittable else { break }
            back.tap()
        }
    }

    private func sleepShort() {
        _ = app.wait(for: .runningForeground, timeout: 1.5)
        Thread.sleep(forTimeInterval: 0.8)
    }

    private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
