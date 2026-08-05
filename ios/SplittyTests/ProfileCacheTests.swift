import XCTest
@testable import Splitty

/// Профиль в офлайн-кеше. Ответ `/auth/*` несёт тот же `Me`, что и `GET /me`,
/// но идёт мимо `DataRepo.me`, поэтому вход обязан положить профиль в кеш сам
/// (`DataRepo.cacheMe`). Без этого офлайн-старт сразу после входа не находит
/// `me`, и экран группы показывает «Профиль не загружен» вместо кешированных
/// операций — сами комнаты в кеше при этом есть.
@MainActor
final class ProfileCacheTests: XCTestCase {
    private var directory: URL!
    private var cache: OfflineStore!
    private var stubSession: URLSession!

    override func setUp() {
        super.setUp()
        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("profile-cache-tests-\(UUID().uuidString)", isDirectory: true)
        cache = OfflineStore(directory: directory)

        StubURLProtocol.handler = nil
        StubURLProtocol.failure = nil
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        stubSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: directory)
        StubURLProtocol.handler = nil
        StubURLProtocol.failure = nil
        stubSession = nil
        super.tearDown()
    }

    private func makeRepo(scope: String) -> DataRepo {
        DataRepo(
            api: APIClient(
                baseURL: URL(string: "https://api.example.test"),
                token: "jwt",
                urlSession: stubSession
            ),
            cache: cache,
            scope: scope
        )
    }

    private let me = Me(
        id: 100,
        username: "zagir",
        displayName: "Загир",
        lang: "ru",
        linkedProviders: ["telegram"],
        notificationOn: true,
        loginEmail: nil
    )

    /// Профиль, положенный входом, доживает до офлайн-чтения: сеть недоступна,
    /// а `me()` всё равно отдаёт значение (isFromCache), а не бросает ошибку.
    func testCachedProfileSurvivesOfflineRead() async throws {
        await makeRepo(scope: "u100").cacheMe(me)

        StubURLProtocol.failure = { _ in URLError(.notConnectedToInternet) }
        var served: Me?
        let result = try await makeRepo(scope: "u100").me { served = $0 }

        XCTAssertEqual(served, me, "Кеш не отдан мгновенно через onCached")
        XCTAssertEqual(result.value, me)
        XCTAssertTrue(result.isFromCache, "Офлайн-чтение обязано пометить значение кешевым")
    }

    /// Namespace владельца соблюдается: профиль одного аккаунта не подставляется
    /// другому — иначе после смены пользователя экраны рисовали бы чужое имя.
    func testCachedProfileIsScopedToOwner() async throws {
        await makeRepo(scope: "u100").cacheMe(me)

        StubURLProtocol.failure = { _ in URLError(.notConnectedToInternet) }
        var served: Me?
        do {
            _ = try await makeRepo(scope: "u200").me { served = $0 }
            XCTFail("Чужой кеш подставился вместо ошибки")
        } catch {
            XCTAssertNil(served)
        }
    }
}
