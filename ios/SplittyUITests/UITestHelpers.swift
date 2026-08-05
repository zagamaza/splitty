import XCTest

/// Общие шаги UI-прогонов. Вынесены из DemoFlowUITests, потому что каждый
/// класс запускается отдельным процессом и на своей установке приложения:
/// «залогинится один тест, остальные пользуются» не работает, логиниться
/// обязан каждый.
extension XCTestCase {
    /// Приложение, настроенное на локальный бэкенд независимо от прод-дефолта.
    func makeApp() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["SPLITTY_BASE_URL"] = "http://127.0.0.1:7171"
        return app
    }

    /// Dev-вход под seed-пользователем, если сессии ещё нет.
    /// Кнопку берём по `devLoginSubmit`: лейбл «Войти» есть и у карточки
    /// входа по email, и запрос по нему падает на «multiple matches».
    func loginIfNeeded(_ app: XCUIApplication, userId: String = "100", name: String = "Загир") {
        let idField = app.textFields["Telegram ID"]
        guard idField.waitForExistence(timeout: 5) else { return } // уже залогинены

        idField.tap()
        idField.typeText(userId)

        let nameField = app.textFields["Имя"]
        nameField.tap()
        nameField.typeText(name)

        dismissKeyboard(app)

        let loginButton = app.buttons["devLoginSubmit"]
        XCTAssertTrue(loginButton.waitForExistence(timeout: 5), "Кнопка dev-входа не найдена")
        XCTAssertTrue(loginButton.isEnabled, "Кнопка dev-входа неактивна")
        loginButton.tap()

        XCTAssertTrue(app.tabBars.firstMatch.waitForExistence(timeout: 15), "Не дождались таб-бара после логина")
    }

    /// Открывает форму расхода из экрана группы. FAB ведёт на AI-экран
    /// (надиктовать/сфотографировать чек), ручной ввод спрятан за
    /// «Ввести вручную» — без этого шага полей «Описание»/суммы на экране нет.
    /// Возвращает false, если FAB не нашёлся.
    @discardableResult
    func openManualExpenseForm(_ app: XCUIApplication) -> Bool {
        guard let fab = rightmostHittableButton(app, labeled: "Добавить расход") else { return false }
        fab.tap()

        let manual = app.buttons["Ввести вручную"]
        if manual.waitForExistence(timeout: 5) {
            manual.tap()
        }
        return true
    }

    /// Среди всех hittable-кнопок с данным label выбирает самую правую
    /// (FAB, а не центральную кнопку таб-бара).
    func rightmostHittableButton(_ app: XCUIApplication, labeled label: String) -> XCUIElement? {
        let matches = app.buttons.matching(NSPredicate(format: "label == %@", label))
        var best: XCUIElement?
        for i in 0..<matches.count {
            let e = matches.element(boundBy: i)
            guard e.exists, e.isHittable else { continue }
            if best == nil || e.frame.maxX > best!.frame.maxX { best = e }
        }
        return best
    }

    func dismissKeyboard(_ app: XCUIApplication) {
        if app.keyboards.count > 0 {
            app.toolbars.buttons["Готово"].exists
                ? app.toolbars.buttons["Готово"].tap()
                : app.swipeDown()
        }
    }

    /// Скриншот-attachment, переживающий успешный прогон.
    func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
