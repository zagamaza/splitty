import XCTest

/// Сквозная проверка офлайн-режима. Тесты запускаются ПО ОДНОМУ,
/// оркестрация снаружи (бэкенд останавливает/запускает раннер):
/// 1. DemoFlowUITests (бэкенд запущен) — логин + прогрев кеша.
/// 2. testOfflineCacheAndQueue (бэкенд ОСТАНОВЛЕН) — кеш виден, расход в outbox.
/// 3. testAfterSync (бэкенд снова запущен) — запись синхронизировалась.
final class OfflineSmokeUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        // Прогон против локального бэкенда независимо от прод-дефолта приложения.
        app.launchEnvironment["SPLITTY_BASE_URL"] = "http://127.0.0.1:7171"
        app.launch()
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

        // Добавляем расход офлайн.
        let fab = app.buttons.matching(NSPredicate(format: "label == %@", "Добавить расход"))
        var fabButton: XCUIElement?
        for i in 0..<fab.count {
            let e = fab.element(boundBy: i)
            if e.exists, e.isHittable, fabButton == nil || e.frame.maxX > fabButton!.frame.maxX {
                fabButton = e
            }
        }
        fabButton?.tap()
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
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
