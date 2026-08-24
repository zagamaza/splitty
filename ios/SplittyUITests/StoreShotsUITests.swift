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
/// Переменные раннеру передаются ТОЛЬКО с префиксом `TEST_RUNNER_` — он
/// снимается на входе в процесс. Без префикса `SHOTS_LANG=en` уходит в
/// настройки сборки, тест его не видит и молча снимает русский набор.
///
/// Симулятор перед сменой языка надо стирать: токен входа лежит в keychain,
/// а он переживает переустановку — иначе английский прогон логинится русским
/// аккаунтом и снимает пустой список групп.
///
///   xcrun simctl shutdown booted && xcrun simctl erase <udid>
///   xcrun simctl boot <udid>
///   xcrun simctl status_bar <udid> override --time 9:41 --cellularBars 4 \
///     --wifiBars 3 --batteryState charging --batteryLevel 100
///   TEST_RUNNER_SHOTS_LANG=en TEST_RUNNER_SHOTS_EMAIL=shots-en@splitty.test \
///     xcodebuild test -only-testing:SplittyUITests/StoreShotsUITests …
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
        /// Расход, разобранный по позициям чека: главный кадр витрины.
        let receiptExpense: String
        /// Красная подсказка композера: если она в кадре — группа не выбрана.
        let pickRoomWarning: String
        /// Кнопка ручного ввода — по ней узнаём, что композер реально открылся.
        let manualEntry: String
        let emailDisclosure = "emailLoginDisclosure"
    }

    private static let sets: [String: Labels] = [
        "ru": Labels(
            appleLanguage: "ru", locale: "ru_RU",
            tabGroups: "Группы", tabFriends: "Друзья", tabAdd: "Добавить расход",
            totals: "Итоги", settle: "Погасить", firstRoom: "Поездка в Стамбул",
            balances: "Балансы",
            rooms: ["Дача на выходные", "Квартира на Тверской", "Поездка в Стамбул"],
            receiptExpense: "Ужин в Кадыкёе",
            pickRoomWarning: "Сначала выберите группу",
            manualEntry: "Ввести вручную"
        ),
        "en": Labels(
            appleLanguage: "en", locale: "en_US",
            tabGroups: "Groups", tabFriends: "Friends", tabAdd: "Add expense",
            totals: "Totals", settle: "Settle up", firstRoom: "Trip to Lisbon",
            balances: "Balances",
            rooms: ["Weekend cabin", "Flat share", "Trip to Lisbon"],
            receiptExpense: "Dinner in Alfama",
            pickRoomWarning: "Pick a group first",
            manualEntry: "Enter manually"
        ),
        "es": Labels(
            appleLanguage: "es", locale: "es_ES",
            tabGroups: "Grupos", tabFriends: "Amigos", tabAdd: "Añadir gasto",
            totals: "Totales", settle: "Saldar", firstRoom: "Viaje a Barcelona",
            balances: "Saldos",
            rooms: ["Finca el finde", "Piso compartido", "Viaje a Barcelona"],
            receiptExpense: "Cena en el Born",
            pickRoomWarning: "Elige primero un grupo",
            manualEntry: "Introducir a mano"
        ),
        "de": Labels(
            appleLanguage: "de", locale: "de_DE",
            tabGroups: "Gruppen", tabFriends: "Freunde", tabAdd: "Hinzufügen",
            totals: "Auswertung", settle: "Begleichen", firstRoom: "Städtetrip Berlin",
            balances: "Salden",
            rooms: ["Wochenende am See", "WG Prenzlauer Berg", "Städtetrip Berlin"],
            receiptExpense: "Abendessen in Kreuzberg",
            pickRoomWarning: "Wähle zuerst eine Gruppe",
            manualEntry: "Manuell eingeben"
        ),
        "fr": Labels(
            appleLanguage: "fr", locale: "fr_FR",
            tabGroups: "Groupes", tabFriends: "Amis", tabAdd: "Ajouter",
            totals: "Bilan", settle: "Régler", firstRoom: "Week-end à Lyon",
            balances: "Soldes",
            rooms: ["Chalet en montagne", "Coloc rue Vieille", "Week-end à Lyon"],
            receiptExpense: "Dîner à la Croix-Rousse",
            pickRoomWarning: "Choisissez d'abord un groupe",
            manualEntry: "Saisir manuellement"
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

        // Главный кадр витрины: расход, РАЗОБРАННЫЙ по позициям чека.
        // Открывается правкой существующего расхода — так же, как его увидит
        // человек сразу после распознавания, но без записи голоса в тесте.
        let receipt = app.staticTexts[labels.receiptExpense].firstMatch
        if receipt.waitForExistence(timeout: 15) {
            receipt.tap()
            settle(1.6)
            // Прокручиваем к «Позициям чека»: сверху итог и участники, а
            // витрине нужен сам разбор — ради него кадр и снимается.
            app.swipeUp()
            settle(1.2)
            shoot(app, "receipt")
            // Возвращаемся в группу: следующие кадры снимаются оттуда.
            app.navigationBars.buttons.element(boundBy: 0).tap()
            settle(1.2)
        } else {
            XCTFail("нет расхода «\(labels.receiptExpense)» — прогнан ли scripts/seed-store-shots.py?")
        }

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

        // Композер надиктовки — главный кадр витрины, и снимать его в покое
        // бессмысленно: продаёт нас момент, когда сказанное на глазах
        // становится текстом. Микрофона у симулятора нет, поэтому оверлей
        // записи поднимает Debug-заготовка (`SPLITTY_DEMO_RECORDING`), а
        // переменную можно задать только при запуске — отсюда перезапуск.
        app.terminate()
        app.launchEnvironment["SPLITTY_DEMO_RECORDING"] = "1"
        app.launch()
        relogin(app, email: env["SHOTS_EMAIL"] ?? "shots-ru@splitty.test",
                password: env["SHOTS_PASSWORD"] ?? "20260806")

        tapAddTab(app)
        XCTAssertTrue(app.buttons[labels.manualEntry].waitForExistence(timeout: 15),
                      "композер не открылся — в кадр уедет список групп")
        settle(1.5)
        // Без выбранной группы экран показывает красное «Сначала выберите
        // группу» и гасит кнопки — витрине нужен рабочий вид, а не заглушка.
        XCTAssertTrue(pickAnyGroupChip(app), "композер не открылся: не видно чипов групп")
        settle(1.2)
        XCTAssertFalse(app.staticTexts[labels.pickRoomWarning].exists,
                       "группа не выбралась — в кадр попадёт красное предупреждение")
        shoot(app, "add")

        print("СКРИНЫ: \(shotsDir.path), кадров: \(index)")
    }

    /// Выбирает группу в композере расхода. Порядок в [Labels.rooms] — как на
    /// экране: тап по дальнему чипу прокручивает ряд, и левый край режет слово
    /// пополам прямо в кадре витрины.
    @discardableResult
    private func pickAnyGroupChip(_ app: XCUIApplication) -> Bool {
        for name in labels.rooms {
            let chip = app.scrollViews.buttons[name].firstMatch
            if chip.waitForExistence(timeout: 5), chip.isHittable {
                chip.tap()
                return true
            }
        }
        return false
    }

    /// Вход после перезапуска: сессия обычно переживает его, и тогда логиниться
    /// заново не надо — но полагаться на это нельзя.
    private func relogin(_ app: XCUIApplication, email: String, password: String) {
        if app.tabBars.buttons[labels.tabGroups].waitForExistence(timeout: 12) { return }
        login(app, email: email, password: password)
        XCTAssertTrue(app.tabBars.buttons[labels.tabGroups].waitForExistence(timeout: 20),
                      "после перезапуска вход не прошёл")
    }

    /// Центральная кнопка «+» композера.
    ///
    /// В иерархии она НЕ внутри `tabBars`, а обычной кнопкой поверх него —
    /// поиск по `app.tabBars.buttons` промахивался, откат «средняя кнопка
    /// таб-бара» жал соседний таб, и витрине уезжал кадр со списком групп под
    /// заголовком «Скажите вслух». Ищем так же, как PaywallShotUITests.
    private func tapAddTab(_ app: XCUIApplication) {
        let named = app.buttons[labels.tabAdd].firstMatch
        guard named.waitForExistence(timeout: 10) else {
            return XCTFail("нет кнопки композера «\(labels.tabAdd)»")
        }
        named.tap()
    }

    /// Вход по email: форма живёт в шторке за ссылкой внизу экрана.
    private func login(_ app: XCUIApplication, email: String, password: String) {
        let disclosure = app.buttons[labels.emailDisclosure]
        guard disclosure.waitForExistence(timeout: 15) else { return } // уже вошли
        disclosure.tap()

        // Поле ищем позицией, а не подписью: плейсхолдер локализован
        // («Correo», «E-Mail»), и поиск по «Email» ронял испанский прогон.
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
