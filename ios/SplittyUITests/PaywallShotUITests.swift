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
///     TEST_RUNNER_SHOTS_LANG=ja TEST_RUNNER_SHOTS_EMAIL=shots-ja@splitty.test \
///       xcodebuild test -only-testing:SplittyUITests/PaywallShotUITests …
///
/// Язык задаётся так же, как у витринных кадров: экран оплаты — самая тесная
/// вёрстка в приложении, и обрезанную кнопку на японском видно только на нём.
final class PaywallShotUITests: XCTestCase {

    /// Подписи для поиска по локали. Русский и английский лежат вместе —
    /// симулятор между прогонами меняет язык, и таб надо узнавать на обоих.
    private struct Labels {
        let appleLanguage: String
        let locale: String
        let groups: [String]
        let addExpense: [String]
        let remaining: String
        let restore: [String]
        let cancel: String
        let profile: [String]
    }

    private static let sets: [String: Labels] = [
        "ru": Labels(appleLanguage: "ru", locale: "ru_RU",
                     groups: ["Группы", "Groups"], addExpense: ["Добавить расход", "Add expense"],
                     remaining: "Осталось", restore: ["Восстановить покупки", "Restore purchases"],
                     cancel: "Отмена", profile: ["Профиль", "Profile"]),
        "ja": Labels(appleLanguage: "ja", locale: "ja_JP",
                     groups: ["グループ"], addExpense: ["支出を追加"],
                     remaining: "残り", restore: ["購入を復元"],
                     cancel: "キャンセル", profile: ["プロフィール"]),
        "zh-Hans": Labels(appleLanguage: "zh-Hans", locale: "zh_CN",
                          groups: ["群组"], addExpense: ["添加支出"],
                          remaining: "剩余", restore: ["恢复购买"],
                          cancel: "取消", profile: ["个人资料"]),
        "ko": Labels(appleLanguage: "ko", locale: "ko_KR",
                     groups: ["그룹"], addExpense: ["지출 추가"],
                     remaining: "인식", restore: ["구입 복원"],
                     cancel: "취소", profile: ["프로필"]),
        "pt-BR": Labels(appleLanguage: "pt-BR", locale: "pt_BR",
                        groups: ["Grupos"], addExpense: ["Adicionar despesa"],
                        remaining: "Restam", restore: ["Restaurar compras"],
                        cancel: "Cancelar", profile: ["Perfil"]),
        "it": Labels(appleLanguage: "it", locale: "it_IT",
                     groups: ["Gruppi"], addExpense: ["Aggiungi spesa"],
                     remaining: "Restano", restore: ["Ripristina acquisti"],
                     cancel: "Annulla", profile: ["Profilo"]),
    ]

    private var labels: Labels!
    private var shotsDir: URL!
    private var index = 0

    override func setUpWithError() throws {
        continueAfterFailure = false
        let documents = try XCTUnwrap(
            FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        )
        let lang = ProcessInfo.processInfo.environment["SHOTS_LANG"] ?? "ru"
        labels = try XCTUnwrap(Self.sets[lang], "нет набора подписей для «\(lang)»")
        shotsDir = documents.appendingPathComponent("splitty-paywall/\(lang)", isDirectory: true)
        try? FileManager.default.removeItem(at: shotsDir)
        try FileManager.default.createDirectory(at: shotsDir, withIntermediateDirectories: true)
    }

    func testPaywallAppearsWhenDailyQuotaIsSpent() throws {
        let env = ProcessInfo.processInfo.environment

        let app = XCUIApplication()
        app.launchEnvironment["SPLITTY_BASE_URL"] = env["SHOTS_BASE_URL"] ?? "http://127.0.0.1:7171"
        app.launchArguments += [
            "-AppleLanguages", "(\(labels.appleLanguage))",
            "-AppleLocale", labels.locale,
        ]
        app.launch()

        login(app,
              email: env["SHOTS_EMAIL"] ?? "shots-ru@splitty.test",
              password: env["SHOTS_PASSWORD"] ?? "20260806")

        let groups = app.tabBars.buttons.matching(
            NSPredicate(format: "label IN %@", labels.groups)
        ).firstMatch
        XCTAssertTrue(groups.waitForExistence(timeout: 25), "вход не прошёл")

        // Композер расхода — центральная кнопка таб-бара.
        let addExpense = app.buttons.matching(
            NSPredicate(format: "label IN %@", labels.addExpense)
        ).firstMatch
        XCTAssertTrue(addExpense.waitForExistence(timeout: 15), "нет кнопки композера")
        addExpense.tap()
        settle(2)
        shoot(app, "composer")

        // Подпись остатка под микрофоном ведёт прямо на экран оплаты. Она
        // показывается, только когда распознаваний мало, — при квоте 1 это
        // ровно наш случай.
        let remainingHint = app.buttons.matching(
            NSPredicate(format: "label CONTAINS %@ OR label CONTAINS %@", labels.remaining, "left")
        ).firstMatch
        if remainingHint.waitForExistence(timeout: 15) {
            remainingHint.tap()
        } else {
            // Остаток приезжает с сервера отдельным запросом и на холодном
            // старте иногда не успевает к моменту снимка. Второй вход на тот
            // же экран — из профиля, и он же описан в заметках для ревью.
            openPaywallFromProfile(app)
        }
        settle(2)

        // Экран оплаты узнаём по обязательной кнопке восстановления покупок:
        // без неё подписку не пропустят на ревью, и её отсутствие — сразу баг.
        let restore = app.buttons.matching(
            NSPredicate(format: "label IN %@", labels.restore)
        ).firstMatch
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

        // Поле ищем позицией, а не подписью: плейсхолдер локализован
        // («Correo», «メール»), и поиск по «Email» ронял все прогоны кроме русского.
        let emailField = app.textFields.element(boundBy: 0)
        XCTAssertTrue(emailField.waitForExistence(timeout: 10), "шторка входа не открылась")
        emailField.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        emailField.typeText(email)

        let passwordField = app.secureTextFields.element(boundBy: 0)
        passwordField.tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        passwordField.typeText(password + "\n")
    }

    /// Профиль → «Splitor Plus»: путь, которым экран оплаты открывает человек,
    /// не упёршийся в лимит (и ревьюер по нашим заметкам).
    private func openPaywallFromProfile(_ app: XCUIApplication) {
        app.buttons[labels.cancel].firstMatch.tap()
        let profile = app.tabBars.buttons.matching(
            NSPredicate(format: "label IN %@", labels.profile)
        ).firstMatch
        XCTAssertTrue(profile.waitForExistence(timeout: 15), "нет вкладки профиля")
        profile.tap()
        // CONTAINS, а не точное совпадение: в подпись кнопки склеивается ещё и
        // строка состояния тарифа, и на любом языке кроме русского (где тест
        // сюда не заходил) поиск по точному «Splitor Plus» не находил ничего.
        let plus = app.buttons.matching(
            NSPredicate(format: "label CONTAINS %@", "Splitor Plus")
        ).firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 15), "в профиле нет строки Splitor Plus")
        plus.tap()
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
