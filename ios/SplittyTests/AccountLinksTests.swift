import XCTest
@testable import Splitty

/// Экран «Способы входа» и «Удалить аккаунт»: модель `Me.linkedProviders`,
/// запросы `APIClient` и поведение `SessionStore` при удалении аккаунта.
/// Сеть не трогается — транспорт подменён (`StubURLProtocol`, см.
/// APIClientAuthTests: там же живёт сам протокол).
final class AccountLinksTests: XCTestCase {
    private var stubSession: URLSession!
    private var client: APIClient!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        stubSession = URLSession(configuration: configuration)
        client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: "jwt",
            urlSession: stubSession
        )
    }

    override func tearDown() {
        client = nil
        stubSession = nil
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        super.tearDown()
    }

    // MARK: - Модель Me

    func testMeDecodesLinkedProviders() throws {
        let json = Data(#"""
        {"id":1000000000001,"username":null,"displayName":"Аня","lang":"ru",
         "linkedProviders":["telegram","google"],"notificationOn":true}
        """#.utf8)

        let me = try JSONDecoder().decode(Me.self, from: json)

        XCTAssertEqual(me.linkedProviders, ["telegram", "google"])
        XCTAssertTrue(me.isLinked(.telegram))
        XCTAssertTrue(me.isLinked(.google))
        XCTAssertFalse(me.isLinked(.apple))
    }

    /// Ключ появился в API позже — профили в офлайн-кеше записаны без него.
    /// Строгий decode ронял бы весь кешированный профиль на холодном старте.
    func testMeDecodesWithoutLinkedProviders() throws {
        let json = Data(#"""
        {"id":42,"username":"anya","displayName":"Аня","lang":"ru","notificationOn":true}
        """#.utf8)

        let me = try JSONDecoder().decode(Me.self, from: json)

        XCTAssertEqual(me.linkedProviders, [])
        XCTAssertFalse(me.isLinked(.google))
    }

    // MARK: - Отвязка последнего способа заблокирована в UI

    func testCanUnlinkRequiresSecondMethod() {
        let single = makeMe(providers: ["telegram"])
        // Единственный способ: кнопка обязана гаснуть ДО запроса, иначе человек
        // узнаёт о запрете из алерта (409 last_identity) уже после действия.
        XCTAssertFalse(single.canUnlink(.telegram))

        let two = makeMe(providers: ["telegram", "google"])
        XCTAssertTrue(two.canUnlink(.telegram))
        XCTAssertTrue(two.canUnlink(.google))
        // Непривязанный способ отвязать нечем — кнопки «Отвязать» у него нет.
        XCTAssertFalse(two.canUnlink(.apple))
    }

    func testCanUnlinkWithEmptyProviders() {
        let none = makeMe(providers: [])
        for provider in LoginProvider.allCases {
            XCTAssertFalse(none.canUnlink(provider))
            XCTAssertFalse(none.isLinked(provider))
        }
    }

    // MARK: - Тексты ошибок

    func testIdentityErrorTextHidesServerCodes() {
        let taken = APIError.server(status: 409, code: "identity_taken", message: "любой серверный текст")
        let text = identityErrorText(taken)
        XCTAssertEqual(text, "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него")
        // Код ошибки пользователю не показываем — ни в каком виде.
        XCTAssertFalse(text.contains("identity_taken"))
        XCTAssertFalse(text.contains("409"))

        let last = APIError.server(status: 409, code: "last_identity", message: "")
        XCTAssertEqual(
            identityErrorText(last),
            "Нельзя отвязать единственный способ входа. Сначала привяжите другой"
        )

        // Незнакомый сбой уходит в общий человеческий текст, а не в «Ошибка 500».
        let transport = APIError.transport(URLError(.notConnectedToInternet))
        XCTAssertEqual(identityErrorText(transport), humanErrorText(transport))
    }

    /// Подтверждение удаления обязано честно говорить, что расходы и долги
    /// в группах остаются: снимки участника из комнат не исчезают.
    func testDeleteConfirmMessageMentionsKeptDebts() {
        let message = AccountView.deleteConfirmMessage
        XCTAssertTrue(message.contains("безвозвратно"))
        XCTAssertTrue(message.contains("Расходы и долги в группах останутся"))
    }

    // MARK: - APIClient: привязка

    func testLinkGoogleSendsIdTokenAndParsesProviders() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"user":{"id":7,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["telegram","google"],"notificationOn":true}}
            """#.utf8))
        }

        let response = try await client.linkGoogle(idToken: "google-id-token")

        XCTAssertEqual(response.user.linkedProviders, ["telegram", "google"])
        XCTAssertNil(response.warning)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/me/link/google")
        // Привязка идёт к ТЕКУЩЕМУ аккаунту — его определяет JWT, а не тело.
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer jwt")

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        XCTAssertEqual(json["idToken"] as? String, "google-id-token")
        XCTAssertEqual(json.count, 1)
    }

    func testLinkAppleSendsRawNonce() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"user":{"id":7,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google","apple"],"notificationOn":true}}
            """#.utf8))
        }

        _ = try await client.linkApple(idToken: "apple-id-token", nonce: "raw-nonce")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/api/v1/me/link/apple")

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        // Сырой nonce: в подписанном Apple токене лежит его SHA256, сервер
        // сверяет одно с другим — отправь мы хеш, совпадения не было бы никогда.
        XCTAssertEqual(json["nonce"] as? String, "raw-nonce")
        XCTAssertEqual(json["idToken"] as? String, "apple-id-token")
    }

    func testLinkGoogleConflictIsIdentityTaken() async {
        StubURLProtocol.handler = { _ in
            (409, Data(#"""
            {"error":{"code":"identity_taken","message":"Этот аккаунт уже связан с другим профилем Splitty. Войдите через него."}}
            """#.utf8))
        }

        do {
            _ = try await client.linkGoogle(idToken: "taken")
            XCTFail("ожидали APIError.server(409)")
        } catch let error as APIError {
            XCTAssertFalse(error.isUnauthorized)
            XCTAssertEqual(
                identityErrorText(error),
                "Этот аккаунт уже связан с другим профилем Splitty. Войдите через него"
            )
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
    }

    // MARK: - APIClient: отвязка

    func testUnlinkProviderUsesDeleteAndReturnsWarning() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"user":{"id":7,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google"],"notificationOn":true},
             "warning":"Telegram отвязан."}
            """#.utf8))
        }

        let response = try await client.unlinkProvider(.telegram)

        XCTAssertEqual(response.user.linkedProviders, ["google"])
        // Предупреждение сервера обязано доехать до UI: в нём про потерю групп.
        XCTAssertEqual(response.warning, "Telegram отвязан.")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/api/v1/me/link/telegram")
    }

    // MARK: - APIClient: удаление аккаунта

    func testDeleteAccountSendsDeleteMe() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.deleteAccount()

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "DELETE")
        XCTAssertEqual(request.url?.path, "/api/v1/me")
    }

    /// Демо-аккаунт ревьюеров: сервер отвечает 403, и это не 401 —
    /// сессия обязана остаться живой.
    func testDeleteAccountForbiddenIsNotUnauthorized() async {
        StubURLProtocol.handler = { _ in
            (403, Data(#"{"error":{"code":"forbidden","message":"Демонстрационный аккаунт удалить нельзя"}}"#.utf8))
        }

        do {
            try await client.deleteAccount()
            XCTFail("ожидали APIError.server(403)")
        } catch let error as APIError {
            XCTAssertFalse(error.isUnauthorized)
            XCTAssertEqual(error.errorDescription, "Демонстрационный аккаунт удалить нельзя")
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
    }

    // MARK: - SessionStore: удаление аккаунта разлогинивает и чистит

    @MainActor
    func testDeleteAccountLogsOutAndClearsLocalData() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        PendingJoin.shared.set("0123456789abcdef01234567")

        StubURLProtocol.handler = { _ in (204, Data()) }
        try await session.deleteAccount()

        XCTAssertFalse(session.isAuthenticated)
        XCTAssertNil(session.me)
        // Токен обязан исчезнуть и из Keychain: иначе следующий запуск
        // приложения поднял бы сессию удалённого аккаунта.
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
        // Отложенное вступление по ссылке — тоже локальные данные удалённого.
        XCTAssertNil(PendingJoin.shared.roomId)
    }

    /// Сетевой сбой не должен выглядеть как успешное удаление: аккаунт жив,
    /// и выкидывать человека на экран входа значило бы соврать ему.
    @MainActor
    func testDeleteAccountNetworkFailureKeepsSession() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in (500, Data(#"{"error":{"code":"internal","message":"сбой"}}"#.utf8)) }

        do {
            try await session.deleteAccount()
            XCTFail("ожидали ошибку удаления")
        } catch {
            // ожидаемо
        }

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertNotNil(session.me)

        session.logout()
    }

    // MARK: - Вспомогательное

    private func makeMe(providers: [String]) -> Me {
        Me(
            id: 1,
            username: nil,
            displayName: "Аня",
            lang: "ru",
            linkedProviders: providers,
            notificationOn: true
        )
    }

    /// Логин через подставной транспорт: даёт живой токен в Keychain и профиль.
    @MainActor
    private func login(_ session: SessionStore) async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"jwt-777","user":{"id":77,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google"],"notificationOn":true}}
            """#.utf8))
        }
        try await session.loginWithCode("ABCD2345")
        XCTAssertTrue(session.isAuthenticated)
    }
}
