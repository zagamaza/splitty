import XCTest

/// Сквозная проверка офлайн-режима. Тесты запускаются ПО ОДНОМУ
/// (`-only-testing:SplittyUITests/OfflineSmokeUITests/<тест>`), оркестрация
/// снаружи — раннер останавливает и запускает бэкенд между ними:
/// 1. testOnlineWarmup (бэкенд запущен) — логин + прогрев кеша.
/// 2. testOfflineCacheAndQueue (бэкенд ОСТАНОВЛЕН) — кеш виден, расход в outbox.
/// 3. testAfterSync (бэкенд снова запущен) — запись синхронизировалась.
///
/// ВАЖНО: бэкенду нужен ФИКСИРОВАННЫЙ `API_JWT_SECRET`. При пустом секрете
/// (а `.env` по умолчанию пуст) `API_DEV_AUTH=true` генерирует случайный
/// эфемерный секрет на каждый старт — шаг 3 поднимает бэкенд уже с другим
/// секретом, выданный на шаге 1 токен получает 401, сессия истекает, и
/// «нет сессии» выглядит как провал офлайна, хотя офлайн ни при чём.
final class OfflineSmokeUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = makeApp()
        app.launch()
    }

    /// Шаг 1: логин и прогрев кеша, пока бэкенд ещё доступен.
    /// Отдельным тестом, а не «сначала прогоните DemoFlowUITests»: классы
    /// запускаются по одному, и зависимость от чужого прогона молча ломалась.
    func testOnlineWarmup() throws {
        loginIfNeeded(app)

        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10),
                      "Группа из seed-данных не появилась — прогревать нечего")
        app.staticTexts["Поездка в Стамбул"].tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "Экран группы не открылся")
        shot("офлайн-00-прогрев")
    }

    /// Бэкенд недоступен: данные из кеша, баннер офлайна, расход — в очередь.
    func testOfflineCacheAndQueue() throws {
        // Группы из кеша (сервер лежит).
        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10), "Таб-бар не появился (нет сессии?)")
        tab.tap()
        XCTAssertTrue(
            app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10),
            "Кешированная группа не показана офлайн"
        )
        shot("офлайн-01-группы-из-кеша")

        app.staticTexts["Поездка в Стамбул"].tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10), "Экран группы не открылся из кеша")

        // Добавляем расход офлайн (FAB → «Ввести вручную»).
        XCTAssertTrue(openManualExpenseForm(app), "Не нашли кнопку добавления расхода в группе")
        let descField = app.textFields["Описание"]
        XCTAssertTrue(descField.waitForExistence(timeout: 5))
        descField.tap()
        descField.typeText("Офлайн кофе")
        let sumField = app.textFields["0"].firstMatch
        XCTAssertTrue(sumField.exists)
        sumField.tap()
        sumField.typeText("300")
        let save = app.buttons["Сохранить"]
        XCTAssertTrue(save.waitForExistence(timeout: 3))
        XCTAssertTrue(save.isEnabled, "Сохранить неактивна офлайн — очередь не работает")
        save.tap()

        // Запись видна с бейджем «не отправлено».
        XCTAssertTrue(
            app.staticTexts["Офлайн кофе"].waitForExistence(timeout: 8),
            "Локальная операция не показана"
        )
        XCTAssertTrue(
            app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "не отправлено")).firstMatch
                .waitForExistence(timeout: 5),
            "Нет бейджа «не отправлено»"
        )
        shot("офлайн-02-очередь")
    }

    /// Бэкенд снова доступен: очередь досылается, бейдж исчезает.
    func testAfterSync() throws {
        let tab = app.tabBars.buttons["Группы"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        XCTAssertTrue(app.staticTexts["Поездка в Стамбул"].waitForExistence(timeout: 10))
        app.staticTexts["Поездка в Стамбул"].tap()
        XCTAssertTrue(app.buttons["Балансы"].waitForExistence(timeout: 10))

        // Ждём синк (триггеры: запуск приложения/появление сети).
        let synced = app.staticTexts["Офлайн кофе"].waitForExistence(timeout: 15)
        XCTAssertTrue(synced, "Операция пропала после синка")
        // Бейдж должен исчезнуть в течение таймаута.
        let badge = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "не отправлено")).firstMatch
        var gone = false
        for _ in 0..<15 {
            if !badge.exists {
                gone = true
                break
            }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTAssertTrue(gone, "Бейдж «не отправлено» не исчез — синк не прошёл")
        shot("офлайн-03-после-синка")
    }

    private func shot(_ name: String) {
        shot(app, name)
    }
}
