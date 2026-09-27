import XCTest

/// Сквозной путь «надиктовал → поправил суммы участников → сохранил».
///
/// До него главная фича продукта была единственной, до которой сквозные тесты
/// не дотягивались: распознаванию нужен Gemini, у локального бэкенда ключа нет.
/// Теперь бэкенд поднимается с `AI_FAKE_PARSER=true` (предсказуемый чек из
/// одной позиции на всех), а приложение по `SPLITTY_FAKE_DICTATION` отправляет
/// фразу текстом тем же запросом, что и надиктовку — у симулятора нет микрофона.
///
/// Жалоба, ради которой тест заведён: после разбора с позициями карточки
/// деления не было, и поправить долю одного человека было нельзя.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true, AI_FAKE_PARSER=true,
/// AI_FREE_DAILY_QUOTA=-1 и засеянными данными (`make seed`). Квоту снимать
/// обязательно: протагонист прогонов на бесплатном тарифе, пять разборов в
/// сутки, и шестой прогон падал бы на 429, а не на дефекте.
final class ManualSharesAfterParseUITests: XCTestCase {
    private var app: XCUIApplication!
    /// Уникальное имя расхода: иначе запись от прошлого прогона давала бы
    /// ложный успех. Только буквы — подставной разбор берёт первое число суммой.
    private let marker = "Такси " + String((0..<6).map { _ in "абвгдежзиклмнопрстуф".randomElement()! })

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = makeApp()
        app.launchEnvironment["SPLITTY_FAKE_DICTATION"] = "\(marker) 100"
        app.launch()
    }

    /// Заменить текст поля целиком.
    ///
    /// Поле суммы выровнено вправо, и тап ставит курсор в НАЧАЛО строки:
    /// «удаления» ничего не стирали, и 34 → 50 превращалось в «5034». Повторное
    /// касание по уже сфокусированному полю курсор не двигает. Поэтому цифры
    /// выделяются двойным касанием у правого края, где они стоят, и ввод
    /// заменяет выделенное.
    private func replace(_ field: XCUIElement, with value: String) {
        field.tap()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "клавиатура не появилась")
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).doubleTap()
        field.typeText(value)
        XCTAssertEqual(field.value as? String, value, "поле \(field.identifier) не приняло значение")
    }

    func testParsedReceiptCanBeTurnedIntoEditableAmounts() throws {
        loginIfNeeded(app)
        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10), "нет таб-бара — вход не прошёл")
        tab.tap()
        let room = app.staticTexts["Поездка в Стамбул"]
        XCTAssertTrue(room.waitForExistence(timeout: 10), "группа из seed-данных не появилась")
        room.tap()
        // Форму открываем через «+» и НЕ жмём «Ввести вручную»: эта ссылка
        // прячет ИИ-композер вместе с микрофоном.
        let fab = try XCTUnwrap(rightmostHittableButton(app, labeled: "Добавить расход"),
                                "не нашли добавление расхода")
        fab.tap()

        // Касание микрофона запускает разбор фразы (см. SPLITTY_FAKE_DICTATION).
        // Микрофон — слой-жест поверх круга, в дереве доступности он не кнопка.
        let mic = app.descendants(matching: .any)["Записать голосом"].firstMatch
        XCTAssertTrue(mic.waitForExistence(timeout: 10), "микрофона в пустой форме нет")
        mic.press(forDuration: 0.3)

        // Чек из одной позиции на всех — режим чека, где карточки деления нет.
        let edit = app.buttons["editParticipantAmounts"]
        XCTAssertTrue(edit.waitForExistence(timeout: 15),
                      "после разбора нет «Изменить суммы участников» — долю не поправить")
        edit.tap()

        // Кнопка ушла, форма стала плоской — карточка деления со суммами.
        XCTAssertFalse(edit.waitForExistence(timeout: 2), "чек не свернулся")
        let fields = app.textFields.matching(NSPredicate(format: "identifier BEGINSWITH 'amountField.'"))
        XCTAssertTrue(fields.firstMatch.waitForExistence(timeout: 5), "полей сумм участников нет")
        XCTAssertEqual(fields.count, 3, "делим на троих — полей должно быть три")

        // То, ради чего всё затевалось: РУКАМИ меняем доли. 34/33/33 → 50/25/25.
        var expected: [String: String] = [:]
        for (index, value) in ["50", "25", "25"].enumerated() {
            let field = fields.element(boundBy: index)
            replace(field, with: value)
            expected[field.identifier] = value
        }
        dismissKeyboard(app)

        let save = app.buttons["Сохранить"]
        XCTAssertTrue(save.waitForExistence(timeout: 5))
        XCTAssertTrue(save.isEnabled, "50/25/25 сходится со 100, а сохранить нельзя")
        save.tap()

        // Открываем сохранённое заново и сверяем каждого участника.
        let saved = app.staticTexts[marker]
        if !saved.waitForExistence(timeout: 10) {
            let alert = app.alerts.firstMatch
            let alertText = alert.exists ? alert.staticTexts.allElementsBoundByIndex.map(\.label).joined(separator: " | ") : "алерта нет"
            let values = fields.allElementsBoundByIndex.map { "\($0.identifier)=\($0.value as? String ?? "?")" }
            XCTFail("расход не появился в группе. Алерт: \(alertText). Поля: \(values)")
            return
        }
        saved.tap()
        let change = app.buttons["Изменить"]
        XCTAssertTrue(change.waitForExistence(timeout: 10), "карточка операции не открылась")
        change.tap()

        for (identifier, value) in expected {
            let field = app.textFields[identifier]
            XCTAssertTrue(field.waitForExistence(timeout: 10), "после повторного открытия нет поля \(identifier)")
            XCTAssertEqual(field.value as? String, value,
                           "у \(identifier) сохранилось не то, что ввели руками")
        }
        // Правка открылась плоской, а не чеком: позиций в сохранённой операции нет.
        XCTAssertFalse(app.buttons["editParticipantAmounts"].exists,
                       "после сохранения операция снова открылась чеком — ручные суммы не записались")
    }
}
