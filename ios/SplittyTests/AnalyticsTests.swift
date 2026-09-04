import XCTest
@testable import Splitty

/// Продуктовая аналитика: очередь, владение и отправка.
final class AnalyticsTests: XCTestCase {
    private var fileURL: URL!

    override func setUp() {
        super.setUp()
        fileURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("analytics-\(UUID().uuidString).json")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: fileURL)
        StubURLProtocol.handler = nil
        super.tearDown()
    }

    private func record(_ id: String, owner: Int, name: String = "app_open") -> AnalyticsRecord {
        AnalyticsRecord(
            id: id, name: name, at: Date(), session: "s-1", platform: "ios",
            appVersion: "1.8", locale: "ru", params: [:], ownerUserId: owner
        )
    }

    /// Очередь переживает перезапуск: события копятся между сеансами, и файл —
    /// единственное, что их держит.
    func testQueueSurvivesRestart() {
        let queue = AnalyticsQueue(fileURL: fileURL)
        queue.append(record("a-1", owner: 1))
        queue.append(record("a-2", owner: 1))
        queue.waitForPendingWrites()

        let reopened = AnalyticsQueue(fileURL: fileURL)
        XCTAssertEqual(reopened.records.map(\.id), ["a-1", "a-2"])
    }

    /// Вход другого человека НЕ забирает события предыдущего.
    ///
    /// Расход в очереди наследуется — его ввели руками, и терять нельзя. С
    /// событием наоборот: содержимого оно не несёт, а приклеенное чужому
    /// человеку это и испорченная аналитика, и приватность.
    func testEventsOfPreviousOwnerAreDropped() {
        let queue = AnalyticsQueue(fileURL: fileURL)
        queue.append(record("a-1", owner: 1))
        queue.append(record("b-1", owner: 2))

        queue.keepOwned(by: 2)

        XCTAssertEqual(queue.records.map(\.id), ["b-1"])
        XCTAssertTrue(queue.take(10, owner: 1).isEmpty)
    }

    /// Выход чистит очередь целиком: отправлять эти события больше некому и не
    /// под кем.
    func testLogoutClearsQueue() {
        let queue = AnalyticsQueue(fileURL: fileURL)
        queue.append(record("a-1", owner: 1))
        queue.keepOwned(by: nil)
        XCTAssertTrue(queue.records.isEmpty)
    }

    /// Потолок очереди: файл не должен расти без предела, а свежие события
    /// полезнее давних.
    func testQueueDropsOldestOverCapacity() {
        let queue = AnalyticsQueue(fileURL: fileURL)
        for i in 0...(AnalyticsQueue.capacity + 10) {
            queue.append(record("e-\(i)", owner: 1))
        }
        XCTAssertEqual(queue.records.count, AnalyticsQueue.capacity)
        XCTAssertEqual(queue.records.first?.id, "e-11")
    }

    /// 401 на отправке событий НЕ гасит сессию.
    ///
    /// Пачка уходит в фоне и по токену, который мог быть отозван («выйти на
    /// всех устройствах»). Общий обработчик 401 выкинул бы человека из
    /// приложения на ровном месте — из-за аналитики, которой он не просил.
    @MainActor
    func testEventsRequestDoesNotExpireSession() async throws {
        StubURLProtocol.handler = { _ in (401, Data(#"{"error":{"code":"unauthorized","message":""}}"#.utf8)) }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        let client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "живой-токен",
            urlSession: URLSession(configuration: configuration)
        )

        var expired = false
        client.onUnauthorized = { expired = true }

        struct Body: Encodable { let events: [String] }
        do {
            try await client.postEvents(Body(events: []))
            XCTFail("401 должен был дойти ошибкой")
        } catch {
            // Ожидаемо: сервер отказал.
        }

        XCTAssertFalse(expired, "отправка событий погасила живую сессию")
    }

    /// Обычный запрос на 401 сессию гасит — это поведение остаётся прежним.
    @MainActor
    func testOrdinaryRequestStillExpiresSession() async throws {
        StubURLProtocol.handler = { _ in (401, Data(#"{"error":{"code":"unauthorized","message":""}}"#.utf8)) }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        let client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "протухший",
            urlSession: URLSession(configuration: configuration)
        )

        var expired = false
        client.onUnauthorized = { expired = true }

        _ = try? await client.me()

        XCTAssertTrue(expired, "обычный 401 обязан гасить сессию")
    }

    /// Имена и параметры совпадают с контрактом: событие — проводной договор с
    /// сервером, и «почти то же имя» означает потерянный шаг воронки.
    func testEventNamesMatchContract() {
        XCTAssertEqual(AnalyticsEvent.appOpen(cold: true).name, "app_open")
        XCTAssertEqual(AnalyticsEvent.appOpen(cold: true).params, ["cold": "true"])
        XCTAssertEqual(AnalyticsEvent.onboardingStep(step: "who_paid").name, "onboarding_step")
        XCTAssertEqual(AnalyticsEvent.purchaseCompleted(product: "yearly").params, ["product": "yearly"])
        XCTAssertEqual(AnalyticsEvent.roomJoinFailed(reason: "not_found").name, "room_join_failed")
    }

    /// Причины — из закрытого множества, а не текст ошибки: свободный текст не
    /// группируется в агрегатах и утаскивает наружу подробности.
    func testReasonsAreClosedSet() {
        XCTAssertEqual(analyticsJoinReason(APIError.server(status: 404, code: "not_found", message: "")), "not_found")
        XCTAssertEqual(analyticsJoinReason(APIError.server(status: 403, code: "", message: "")), "forbidden")
        XCTAssertEqual(analyticsJoinReason(URLError(.notConnectedToInternet)), "network")
        XCTAssertEqual(analyticsParseReason(APIError.server(status: 429, code: "rate_limited", message: "")), "rate_limited")
        XCTAssertEqual(analyticsParseReason(APIError.server(status: 500, code: "", message: "")), "internal")
        XCTAssertEqual(analyticsProduct("com.zagir.splitty.plus.yearly"), "yearly")
        XCTAssertEqual(analyticsProduct("com.zagir.splitty.plus.monthly"), "monthly")
    }
}
