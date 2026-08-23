import XCTest

/// Снимает экраны для витрин App Store и Google Play.
///
/// Не координатами: демо-данные и размер устройства фиксированы, но экраны
/// меняются, а тест переживает вёрстку — он ищет элементы по подписям. Язык
/// приложения задаётся аргументами запуска, поэтому подписи для поиска
/// приходят из набора [Labels] под нужную локаль.
///
/// Файлы пишутся в Documents раннера: он песочный, и его /tmp хостовому
/// каталогу устройства не соответствует — запись туда молча не проходила.
/// Путь печатается в лог, забрать оттуда проще, чем выковыривать вложения
/// из result-бандла.
///
///   xcodebuild test -only-testing:SplittyUITests/StoreShotsUITests \
///     SHOTS_LANG=ru SHOTS_EMAIL=shots-ru@splitty.test
final class StoreShotsUITests: XCTestCase {

    /// Подписи, по которым тест ищет элементы, — по одному набору на локаль.
    private struct Labels {
        let appleLanguage: String
        let locale: String
        let tabGroups: String
        let tabFriends: String
        let tabAdd: String
        let totals: String
        let settle: String
        let firstRoom: String
        let balances: String
        let rooms: [String]
        let emailDisclosure = "emailLoginDisclosure"
    }

    private static let sets: [String: Labels] = [
        "ru": Labels(
            appleLanguage: "ru", locale: "ru_RU",
            tabGroups: "Группы", tabFriends: "Друзья", tabAdd: "Добавить расход",
            totals: "Итоги", settle: "Погасить", firstRoom: "Поездка в Стамбул",
            balances: "Балансы",
            rooms: ["Дача на выходные", "Квартира на Тверской", "Поездка в Стамбул"]
        ),
        "en": Labels(
            appleLanguage: "en", locale: "en_US",
            tabGroups: "Groups", tabFriends: "Friends", tabAdd: "Add expense",
            totals: "Totals", settle: "Settle up", firstRoom: "Trip to Lisbon",
            balances: "Balances",
            rooms: ["Weekend cabin", "Flat share", "Trip to Lisbon"]
        ),
    ]

    private var labels: Labels!
    private var shotsDir: URL!
    private var index = 0

    func testCaptureStoreShots() throws {
        let env = ProcessInfo.processInfo.environment
        let lang = env["SHOTS_LANG"] ?? "ru"
        labels = try XCTUnwrap(Self.sets[lang], "нет набора подписей для «\(lang)»")

        let documents = try XCTUnwrap(
            FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        )
        shotsDir = documents.appendingPathComponent("splitty-shots/\(lang)", isDirectory: true)
        try? FileManager.default.removeItem(at: shotsDir)
        try FileManager.default.createDirectory(at: shotsDir, withIntermediateDirectories: true)

        let app = XCUIApplication()
        app.launchEnvironment["SPLITTY_BASE_URL"] = env["SHOTS_BASE_URL"] ?? "http://127.0.0.1:7171"
        app.launchArguments += [
            "-AppleLanguages", "(\(labels.appleLanguage))",
            "-AppleLocale", labels.locale,
        ]
        app.launch()

        login(app, email: env["SHOTS_EMAIL"] ?? "shots-ru@splitty.test",
              password: env["SHOTS_PASSWORD"] ?? "20260806")

        let groups = app.tabBars.buttons[labels.tabGroups]
        XCTAssertTrue(groups.waitForExistence(timeout: 20), "нет таб-бара — вход не прошёл")
        groups.tap()
        XCTAssertTrue(app.staticTexts[labels.firstRoom].waitForExistence(timeout: 15),
                      "демо-группы нет — прогнан ли scripts/seed-store-shots.py?")
        settle(); shoot(app, "groups")

        app.staticTexts[labels.firstRoom].tap()
        XCTAssertTrue(app.buttons[labels.totals].waitForExistence(timeout: 15))
        settle(); shoot(app, "group")

        // Погашение снимаем ЗДЕСЬ, не откатившись назад: кнопка живёт в шапке
        // группы, и со списка групп её уже не видно.
        let settleButton = app.buttons[labels.settle].firstMatch
        if settleButton.exists, settleButton.isHittable {
            settleButton.tap()
            settle(1.4); shoot(app, "settle")
            app.navigationBars.buttons.element(boundBy: 0).tap()
            settle()
        }

        // «Балансы» вместо второго кадра дашборда: прокрученный дашборд как
        // ни останавливай, оставляет под статус-баром обрезанную строку, а
        // «кто кому должен» — самостоятельная мысль и снимается без скролла.
        app.buttons[labels.balances].tap()
        settle(1.3); shoot(app, "balances")

        app.buttons[labels.totals].tap()
        settle(1.5); shoot(app, "totals")

        app.navigationBars.buttons.element(boundBy: 0).tap()
        settle()

        app.tabBars.buttons[labels.tabFriends].tap()
        settle(); shoot(app, "friends")

        tapAddTab(app)
        settle(1.5)
        // Без выбранной группы экран показывает красное «Сначала выберите
        // группу» и гасит кнопки — витрине нужен рабочий вид, а не заглушка.
        pickAnyGroupChip(app)
        settle(1.2)
        shoot(app, "add")

        print("СКРИНЫ: \(shotsDir.path), кадров: \(index)")
    }

    /// Выбирает группу в композере расхода. Порядок в [Labels.rooms] — как на
    /// экране: тап по дальнему чипу прокручивает ряд, и левый край режет слово
    /// пополам прямо в кадре витрины.
    private func pickAnyGroupChip(_ app: XCUIApplication) {
        for name in labels.rooms {
            let chip = app.buttons[name].firstMatch
            if chip.exists, chip.isHittable {
                chip.tap()
                return
            }
        }
    }

    /// Центральная вкладка «+»: у неё может не быть текстовой подписи, поэтому
    /// сначала пробуем по имени, а иначе берём среднюю кнопку таб-бара.
    private func tapAddTab(_ app: XCUIApplication) {
        let named = app.tabBars.buttons[labels.tabAdd]
        if named.exists {
            named.tap()
            return
        }
        let buttons = app.tabBars.buttons
        guard buttons.count > 0 else { return XCTFail("таб-бар пуст") }
        buttons.element(boundBy: buttons.count / 2).tap()
    }

    /// Вход по email: форма живёт в шторке за ссылкой внизу экрана.
    private func login(_ app: XCUIApplication, email: String, password: String) {
        let disclosure = app.buttons[labels.emailDisclosure]
        guard disclosure.waitForExistence(timeout: 15) else { return } // уже вошли
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

    /// Даёт анимациям и сети успокоиться: кадр со спиннером витрине не нужен.
    private func settle(_ seconds: TimeInterval = 1.0) {
        Thread.sleep(forTimeInterval: seconds)
    }

    private func shoot(_ app: XCUIApplication, _ name: String) {
        index += 1
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let file = shotsDir.appendingPathComponent(String(format: "%02d-%@.png", index, name))
        do {
            try png.write(to: file)
        } catch {
            XCTFail("не записался кадр \(file.lastPathComponent): \(error)")
        }
    }
}
