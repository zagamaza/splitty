import XCTest
@testable import Splitty

/// Запоздавший 401 из ПРОШЛОЙ сессии не имеет права гасить новую.
///
/// Это второй слой защиты от отказа ревью 1.7 («After login we were returned to
/// login screen»): первый — стор больше не держит захваченного клиента, второй —
/// сессия игнорирует 401, относящийся к другому поколению токена. Без него
/// любой висевший в полёте запрос, ответивший после переавторизации, снова
/// выбрасывал человека на экран входа.
@MainActor
final class StaleSessionTests: XCTestCase {
    private var stubSession: URLSession!

    override func setUp() {
        super.setUp()
        UserDefaults.standard.removeObject(forKey: "splitty.purgePending")
        StubURLProtocol.handler = nil
        StubURLProtocol.responseDelay = nil
        StubURLProtocol.failure = nil

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        stubSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        StubURLProtocol.handler = nil
        StubURLProtocol.responseDelay = nil
        stubSession = nil
        super.tearDown()
    }

    private func login(_ session: SessionStore, token: String) async throws {
        StubURLProtocol.responseDelay = nil
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"\#(token)","user":{"id":77,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["password"],"notificationOn":true}}
            """#.utf8))
        }
        try await session.loginWithPassword(email: "anya@splitty.test", password: "Passw0rd!")
    }

    private func settle() async {
        for _ in 0..<10 { await Task.yield() }
        try? await Task.sleep(nanoseconds: 200_000_000)
    }

    /// Клиент, созданный ДО входа, отвечает 401 уже после него. Ровно этот
    /// сценарий ловило ревью: человек вошёл и его сразу вернуло на логин.
    func testStale401FromPreviousSessionKeepsNewSession() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session, token: "jwt-first")

        // Клиент прошлого поколения — держим его, как это делал SubscriptionStore.
        let staleClient = session.api

        // Человек перевошёл: поколение сменилось.
        session.logout()
        try await login(session, token: "jwt-second")
        XCTAssertTrue(session.isAuthenticated)

        // И только теперь прилетает 401 на запрос старого клиента.
        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        try? await staleClient.unregisterDevice(token: "fcm")
        await settle()

        XCTAssertTrue(session.isAuthenticated,
                      "401 из прошлой сессии погасил новую — это и есть отказ ревью 1.7")
        XCTAssertEqual(KeychainStore.read(key: "splitty.apiToken"), "jwt-second")

        session.logout()
    }

    /// Обратная сторона: 401 на ДЕЙСТВУЮЩИЙ токен обязан разлогинивать, иначе
    /// приложение с мёртвой сессией никогда не покажет экран входа.
    func testCurrent401StillExpiresSession() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session, token: "jwt-first")

        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        try? await session.api.unregisterDevice(token: "fcm")
        await settle()

        XCTAssertFalse(session.isAuthenticated)
    }

    /// refreshMe гасил сессию в обход этой защиты — он зовётся на каждом старте,
    /// и его ответ легко переживает вход. Нашёл Codex при разборе фикса.
    func testStale401FromRefreshMeKeepsNewSession() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session, token: "jwt-first")

        // /me отвечает 401, но с задержкой — за это время человек перевойдёт.
        StubURLProtocol.responseDelay = { _ in 0.4 }
        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        let pending = Task { await session.refreshMe() }

        try? await Task.sleep(nanoseconds: 100_000_000)
        session.logout()
        try await login(session, token: "jwt-second")

        await pending.value
        await settle()

        XCTAssertTrue(session.isAuthenticated,
                      "запоздавший 401 из refreshMe погасил новую сессию")
        XCTAssertEqual(KeychainStore.read(key: "splitty.apiToken"), "jwt-second")

        session.logout()
    }
}
