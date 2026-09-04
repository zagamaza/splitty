import XCTest
@testable import Splitty

/// Стор тарифа обязан брать клиента у сессии НА КАЖДЫЙ вызов.
///
/// Ревью App Store отклонило релиз 1.7 с формулировкой «After login we were
/// returned to login screen»: стор создаётся при старте приложения, когда
/// токена ещё нет, захватывал клиента навсегда и после входа продолжал ходить
/// со старым. Сервер отвечал 401, `onUnauthorized` гасил свежую сессию — и
/// человека выбрасывало на экран логина сразу после успешного входа.
@MainActor
final class SubscriptionStoreTokenTests: XCTestCase {
    private var urlSession: URLSession!
    /// Токены всех перехваченных запросов — по ним видно, чем ходил стор.
    nonisolated(unsafe) private static var seenTokens: [String] = []

    override func setUp() {
        super.setUp()
        Self.seenTokens = []
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.handler = { request in
            // Продуктовые события сюда не считаем: отправка терминального
            // события живёт своей задачей и может долететь из соседнего теста —
            // стаб-протокол общий на класс. К тому, чем ходит стор подписки,
            // это отношения не имеет.
            let auth = request.value(forHTTPHeaderField: "Authorization") ?? ""
            if (request.url?.path ?? "").hasSuffix("/api/v1/events") {
                return (200, Data(#"{"accepted":0,"duplicates":0,"rejected":0}"#.utf8))
            }
            Self.seenTokens.append(auth.replacingOccurrences(of: "Bearer ", with: ""))
            let body = #"{"tier":"free","limit":5,"remaining":5,"unlimited":false,"resetsAt":"2026-09-01T00:00:00Z"}"#
            return (200, Data(body.utf8))
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        urlSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        StubURLProtocol.handler = nil
        urlSession = nil
        super.tearDown()
    }

    private func client(token: String?) -> APIClient {
        APIClient(baseURL: URL(string: "https://api.example.test"), token: token, urlSession: urlSession)
    }

    /// Токен, появившийся ПОСЛЕ создания стора, обязан доехать до сервера.
    func testStoreUsesTokenIssuedAfterItWasCreated() async {
        var current: String?
        // Стор создаётся до входа — ровно как в SplittyApp.init.
        let store = SubscriptionStore(api: { [self] in client(token: current) })

        await store.refreshQuota()
        XCTAssertEqual(Self.seenTokens, [""], "до входа токена нет — это нормально")

        // Человек вошёл: сессия выдала токен.
        current = "свежий-токен"
        await store.refreshQuota()

        XCTAssertEqual(Self.seenTokens.last, "свежий-токен",
                       "стор ушёл со старым токеном — именно за это ревью вернуло 1.7")
    }

    /// То же для подписки: она читается на том же экране и валила вход так же.
    func testSubscriptionRequestUsesCurrentToken() async {
        var current: String? = "старый"
        let store = SubscriptionStore(api: { [self] in client(token: current) })

        await store.refreshSubscription()
        current = "новый"
        await store.refreshSubscription()

        XCTAssertEqual(Self.seenTokens.last, "новый")
    }
}
