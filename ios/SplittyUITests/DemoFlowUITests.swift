import XCTest

/// Сквозной демо-прогон по основным сценариям против локального бэкенда
/// (http://127.0.0.1:7171, API_DEV_AUTH=true, данные из seed-скрипта).
/// Каждый шаг снимает скриншот-attachment (lifetime .keepAlways).
final class DemoFlowUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = true
        app = makeApp()
        app.launch()
    }

    func testDemoFlow() throws {
        shot("00-запуск")
        loginIfNeeded(app)

        // --- Группы ---
        tapTab("Группы")
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10),
                      "Группа из seed-данных не появилась")
        shot("01-группы")

        app.staticTexts["Поездка в Стамбул"].tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "Экран группы не открылся")
        sleepShort()
        shot("02-группа-стамбул")

        // --- Добавление расхода (FAB внутри группы → «Ввести вручную») ---
        if openManualExpenseForm(app) {
            let descField = app.textFields["Описание"]
            if descField.waitForExistence(timeout: 5) {
                descField.tap()
                descField.typeText("Кофе на двоих")
                let sumField = app.textFields["0"].firstMatch
                if sumField.exists {
                    sumField.tap()
                    sumField.typeText("600")
                }
                dismissKeyboard(app)
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

        // --- Балансы (вкладка бара тусы) ---
        if app.buttons["Балансы"].waitForExistence(timeout: 5) {
            app.buttons["Балансы"].tap()
            sleepShort()
            shot("06-балансы")
        }

        // --- Итоги (вкладка бара тусы) ---
        if app.buttons["Итоги"].waitForExistence(timeout: 5) {
            app.buttons["Итоги"].tap()
            sleepShort()
            shot("07-итоги")
        }

        // назад на операции — там hero-карточка с «Погасить»
        if app.buttons["Операции"].waitForExistence(timeout: 5) {
            app.buttons["Операции"].tap()
            sleepShort()
        }

        // --- Погашение долга (CTA в hero-карточке) ---
        if app.buttons["Погасить"].waitForExistence(timeout: 5), app.buttons["Погасить"].isEnabled {
            app.buttons["Погасить"].tap()
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

    private func tapTab(_ name: String) {
        let tab = app.tabBars.buttons[name]
        if tab.waitForExistence(timeout: 5) {
            tab.tap()
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
        shot(app, name)
    }
}
