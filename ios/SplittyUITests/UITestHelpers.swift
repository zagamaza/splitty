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

    /// Учётка протагониста прогонов. Та же пара зашита в scripts/seed-local.py,
    /// который её и заводит вместе с демо-группами — меняете здесь, меняйте там.
    static let seedEmail = "ui-tests@splitty.test"
    static let seedPassword = "20260806"

    /// Вход по email и паролю, если сессии ещё нет. Другого пути у теста нет:
    /// dev-вход с экрана убран, а Apple/Google/Telegram требуют внешних
    /// сервисов и системных листов.
    ///
    /// Попыток три, и решает исход, а не содержимое полей: `typeText` иногда
    /// теряет символы, а проверить это у пароля нечем — `SecureField` отдаёт в
    /// accessibility постоянную маску из пяти точек независимо от длины. Так что
    /// признак успеха один — таб-бар; алерт «неверный email или пароль» означает
    /// «набралось не то», и пара набирается заново.
    func loginIfNeeded(
        _ app: XCUIApplication,
        email: String = XCTestCase.seedEmail,
        password: String = XCTestCase.seedPassword
    ) {
        let emailField = app.textFields["Email"]
        guard emailField.waitForExistence(timeout: 5) else { return } // уже залогинены

        let passwordField = app.secureTextFields["Пароль"]
        XCTAssertTrue(passwordField.waitForExistence(timeout: 5), "Поле пароля не найдено")

        for attempt in 1...3 {
            clear(emailField, app: app)
            type(email, into: emailField, app: app)
            clear(passwordField, app: app)
            type(password, into: passwordField, app: app)
            dismissKeyboard(app)

            // Лейбл «Войти» уникален: код-вход и dev-вход с экрана убраны.
            let loginButton = app.buttons["Войти"]
            XCTAssertTrue(loginButton.waitForExistence(timeout: 5), "Кнопка «Войти» не найдена")
            XCTAssertTrue(loginButton.isEnabled, "Кнопка «Войти» неактивна — форма считает пару невалидной")
            loginButton.tap()

            if app.tabBars.firstMatch.waitForExistence(timeout: 15) { return }

            let alertOk = app.alerts.buttons["Ок"]
            guard alertOk.waitForExistence(timeout: 3) else {
                XCTFail("Ни таб-бара, ни алерта после входа — сервер молчит?")
                return
            }
            alertOk.tap()
            XCTAssertNotEqual(
                attempt, 3,
                "Три раза «неверный email или пароль» — прогнан ли scripts/seed-local.py?"
            )
        }
    }

    /// Опустошает поле: ставит курсор и жмёт backspace с запасом.
    /// Длину `SecureField` узнать нельзя (постоянная маска), поэтому бьём
    /// по верхней границе — лишние нажатия по пустому полю безвредны.
    func clear(_ field: XCUIElement, app: XCUIApplication, maxLength: Int = 40) {
        guard (field.value as? String)?.isEmpty == false else { return }
        field.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 5)
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: maxLength))
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

    /// Набирает текст в поле, дождавшись клавиатуры.
    ///
    /// Ждать обязательно: `typeText` сразу после `tap()` теряет символы, пока
    /// клавиатура выезжает, а у пароля это всплывает уже ответом сервера — 401
    /// на обрезанной паре выглядит как «неверная учётка», хотя учётка верная.
    func type(_ text: String, into field: XCUIElement, app: XCUIApplication) {
        field.tap()
        XCTAssertTrue(
            app.keyboards.firstMatch.waitForExistence(timeout: 5),
            "Клавиатура не появилась — печатать некуда"
        )
        field.typeText(text)
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
