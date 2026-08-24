import XCTest

/// Снимает экран оплаты живьём: логинится, упирается в суточный лимит и
/// фотографирует то, что реально увидит человек.
///
/// Проверять paywall превью недостаточно: он зависит от цен из магазина,
/// остатка с сервера и состояния кнопок, а всё это собирается только в
/// работающем приложении. Запускать против ЛОКАЛЬНОГО бэкенда с маленькой
/// квотой:
///
///     AI_FREE_DAILY_QUOTA=1 ./bin/splitty
///     xcodebuild test -only-testing:SplittyUITests/PaywallShotUITests …
final class PaywallShotUITests: XCTestCase {
    private var shotsDir: URL!
    private var index = 0

    override func setUpWithError() throws {
        continueAfterFailure = false
        let documents = try XCTUnwrap(
            FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        )
        shotsDir = documents.appendingPathComponent("splitty-paywall", isDirectory: true)
        try? FileManager.default.removeItem(at: shotsDir)
        try FileManager.default.createDirectory(at: shotsDir, withIntermediateDirectories: true)
    }

    func testPaywallAppearsWhenDailyQuotaIsSpent() throws {
        let env = ProcessInfo.processInfo.environment

        let app = XCUIApplication()
        app.launchEnvironment["SPLITTY_BASE_URL"] = env["SHOTS_BASE_URL"] ?? "http://127.0.0.1:7171"
        app.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        app.launch()

        login(app,
              email: env["SHOTS_EMAIL"] ?? "shots-ru@splitty.test",
              password: env["SHOTS_PASSWORD"] ?? "20260806")

        let groups = app.tabBars.buttons["Группы"]
        XCTAssertTrue(groups.waitForExistence(timeout: 25), "вход не прошёл")

        // Композер расхода — центральная кнопка таб-бара.
        let addExpense = app.buttons["Добавить расход"]
        XCTAssertTrue(addExpense.waitForExistence(timeout: 15), "нет кнопки композера")
        addExpense.tap()
        settle(2)
        shoot(app, "composer")

        // Подпись остатка под микрофоном ведёт прямо на экран оплаты. Она
        // показывается, только когда распознаваний мало, — при квоте 1 это
        // ровно наш случай.
        let remainingHint = app.buttons.matching(
            NSPredicate(format: "label CONTAINS %@", "Осталось")
        ).firstMatch
        XCTAssertTrue(remainingHint.waitForExistence(timeout: 15),
                      "подпись остатка не показалась, хотя распознаваний мало")
        remainingHint.tap()
        settle(2)

        // Экран оплаты узнаём по обязательной кнопке восстановления покупок:
        // без неё подписку не пропустят на ревью, и её отсутствие — сразу баг.
        let restore = app.buttons["Восстановить покупки"]
        XCTAssertTrue(restore.waitForExistence(timeout: 15),
                      "экран оплаты не открылся: суточный лимит не привёл к paywall")
        settle()
        shoot(app, "paywall")

        // Тёмная тема — второй обязательный кадр: токены адаптивные, и
        // проверять их надо обоими.
        XCUIDevice.shared.appearance = .dark
        settle(1.5)
        shoot(app, "paywall-dark")
        XCUIDevice.shared.appearance = .light
    }

    // MARK: - Вспомогательное

    private func typeExpense(_ app: XCUIApplication, text: String) {
        let field = app.textFields.element(boundBy: 0)
        guard field.waitForExistence(timeout: 10) else { return }
        field.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        field.typeText(text + "\n")
    }

    private func login(_ app: XCUIApplication, email: String, password: String) {
        let disclosure = app.buttons["emailLoginDisclosure"]
        guard disclosure.waitForExistence(timeout: 20) else { return } // уже вошли
        disclosure.tap()

        let emailField = app.textFields["Email"]
        XCTAssertTrue(emailField.waitForExistence(timeout: 10), "шторка входа не открылась")
        emailField.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        emailField.typeText(email)

        let passwordField = app.secureTextFields.element(boundBy: 0)
        passwordField.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        passwordField.typeText(password + "\n")
    }

    private func settle(_ seconds: TimeInterval = 1.0) {
        Thread.sleep(forTimeInterval: seconds)
    }

    private func shoot(_ app: XCUIApplication, _ name: String) {
        index += 1
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let file = shotsDir.appendingPathComponent(String(format: "%02d-%@.png", index, name))
        try? png.write(to: file)
    }
}
