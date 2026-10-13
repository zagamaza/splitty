import XCTest

/// «+» в нижней строке не должен ездить при смене вкладки: точка выбранной,
/// жирное начертание и гаснущий на «Уведомлениях» бейдж меняли ширины пунктов,
/// гибкие зазоры перераспределялись, и кнопка сдвигалась на каждом переходе.
///
/// Бейдж обязателен для сценария: тест сам заводит одно непрочитанное (расход
/// от другого участника) и дожидается, пока счётчик погаснет. Без этого на
/// аккаунте без уведомлений главный случай просто не проверялся бы.
///
/// Запуск: бэкенд на 127.0.0.1:7171 с API_DEV_AUTH=true и `make seed`.
final class TabBarStabilityUITests: XCTestCase {
    private let base = URL(string: "http://127.0.0.1:7171/api/v1")!

    func testAddButtonStaysPutAcrossTabs() throws {
        continueAfterFailure = false
        try makeOneUnread()

        let app = makeApp()
        app.launchArguments += ["-AppleLanguages", "(ru)", "-AppleLocale", "ru_RU"]
        app.launch()
        loginIfNeeded(app)

        let add = app.otherElements["tabBar"].buttons["Добавить расход"]
        XCTAssertTrue(add.waitForExistence(timeout: 10), "нет «+» в нижней строке")
        let activity = app.buttons["tab.activity"]
        XCTAssertTrue(waitForValue(activity, equals: "1"), "бейджа «1» нет — сценарий без него ничего не проверяет")
        let origin = add.frame.origin

        activity.tap()
        XCTAssertTrue(waitForValue(activity, equals: ""), "бейдж не погас после открытия «Уведомлений»")
        assertSame(add.frame.origin, origin, "после «Уведомлений» и сброса бейджа")

        for tab in ["tab.friends", "tab.account", "tab.groups"] {
            app.buttons[tab].tap()
            XCTAssertTrue(app.buttons[tab].waitForSelected(), "вкладка \(tab) не выбралась")
            assertSame(add.frame.origin, origin, "на вкладке \(tab)")
        }
    }

    private func assertSame(_ point: CGPoint, _ origin: CGPoint, _ context: String) {
        XCTAssertEqual(point.x, origin.x, accuracy: 0.5, "«+» сдвинулся \(context)")
        XCTAssertEqual(point.y, origin.y, accuracy: 0.5, "«+» сдвинулся \(context)")
    }

    private func waitForValue(_ element: XCUIElement, equals value: String) -> Bool {
        // Пустое значение XCUITest может отдавать и как nil.
        let predicate = value.isEmpty
            ? NSPredicate(format: "value == nil OR value == ''")
            : NSPredicate(format: "value == %@", value)
        return XCTWaiter.wait(for: [expectation(for: predicate, evaluatedWith: element)], timeout: 10) == .completed
    }

    // MARK: - Непрочитанное через API

    /// Гасит всё прочитанное у протагониста и добавляет ровно один расход от
    /// Алмаза в «Квартиру на Тверской» — счётчик становится «1».
    private func makeOneUnread() throws {
        let ui = try call("POST", "/auth/login", body: ["email": Self.seedEmail, "password": Self.seedPassword])
        let uiToken = try XCTUnwrap(ui["token"] as? String)
        let uiId = try XCTUnwrap((ui["user"] as? [String: Any])?["id"] as? Int)
        let almaz = try call("POST", "/auth/dev", body: ["userId": 101, "displayName": "Алмаз", "username": "almaz"])
        let almazToken = try XCTUnwrap(almaz["token"] as? String)
        let almazId = try XCTUnwrap((almaz["user"] as? [String: Any])?["id"] as? Int)

        let feed = try call("GET", "/notifications", token: uiToken)
        _ = try call("POST", "/me/notifications-seen", token: uiToken, body: ["seenThrough": feed["seenThrough"] ?? ""])

        let rooms = try callList("GET", "/rooms", token: uiToken)
        let room = try XCTUnwrap(rooms.first { $0["name"] as? String == "Квартира на Тверской" }, "нет seed-группы")
        let roomId = try XCTUnwrap(room["id"] as? String)
        _ = try call("POST", "/rooms/\(roomId)/operations", token: almazToken, body: [
            "description": "Кофе для теста строки", "sum": 100, "donorId": almazId,
            "recipientIds": [almazId, uiId], "splitType": "equally",
        ])
    }

    private func call(_ method: String, _ path: String, token: String? = nil, body: [String: Any]? = nil) throws -> [String: Any] {
        let data = try request(method, path, token: token, body: body)
        return (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
    }

    private func callList(_ method: String, _ path: String, token: String) throws -> [[String: Any]] {
        let data = try request(method, path, token: token, body: nil)
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [[String: Any]])
    }

    private func request(_ method: String, _ path: String, token: String?, body: [String: Any]?) throws -> Data {
        var req = URLRequest(url: base.appendingPathComponent(path))
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body { req.httpBody = try JSONSerialization.data(withJSONObject: body) }
        var result: Result<(Data, Int), Error>?
        let done = expectation(description: "\(method) \(path)")
        URLSession.shared.dataTask(with: req) { data, response, error in
            if let error { result = .failure(error) } else {
                result = .success((data ?? Data(), (response as? HTTPURLResponse)?.statusCode ?? 0))
            }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 15)
        let (data, status) = try XCTUnwrap(result).get()
        XCTAssertTrue((200..<300).contains(status), "\(method) \(path) → \(status)")
        return data
    }
}

private extension XCUIElement {
    func waitForSelected(timeout: TimeInterval = 5) -> Bool {
        let predicate = NSPredicate(format: "isSelected == true")
        return XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: predicate, object: self)], timeout: timeout) == .completed
    }
}
