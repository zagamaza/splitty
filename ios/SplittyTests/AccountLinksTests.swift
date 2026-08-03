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
        // Флаг незавершённой чистки персистентный (UserDefaults): без сброса он
        // протекал бы из теста в тест, и «токен сохранился» проходило бы даром.
        UserDefaults.standard.removeObject(forKey: "splitty.purgePending")
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil
        StubURLProtocol.failure = nil

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
        UserDefaults.standard.removeObject(forKey: "splitty.purgePending")
        client = nil
        stubSession = nil
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil
        StubURLProtocol.failure = nil
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

    func testLinkAppleSendsRawNonceAndAuthorizationCode() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"user":{"id":7,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google","apple"],"notificationOn":true}}
            """#.utf8))
        }

        _ = try await client.linkApple(
            idToken: "apple-id-token",
            nonce: "raw-nonce",
            authorizationCode: "auth-code"
        )

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/api/v1/me/link/apple")

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        // Сырой nonce: в подписанном Apple токене лежит его SHA256, сервер
        // сверяет одно с другим — отправь мы хеш, совпадения не было бы никогда.
        XCTAssertEqual(json["nonce"] as? String, "raw-nonce")
        XCTAssertEqual(json["idToken"] as? String, "apple-id-token")
        // Без кода сервер не получит Apple refresh token, и отозвать доступ
        // при удалении аккаунта будет нечем (Guideline 5.1.1(v)) — а «добрать»
        // код позже Apple не даёт. Привязка обязана слать его так же, как вход.
        XCTAssertEqual(json["authorizationCode"] as? String, "auth-code")
    }

    func testLinkAppleOmitsEmptyAuthorizationCode() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"user":{"id":7,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google","apple"],"notificationOn":true}}
            """#.utf8))
        }

        _ = try await client.linkApple(idToken: "apple-id-token", nonce: "raw", authorizationCode: nil)

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        // Поле на сервере опциональное: пустое значение не отправляем вовсе,
        // привязка личности проходит и без него.
        XCTAssertNil(json["authorizationCode"])
        XCTAssertEqual(json.count, 2)
    }

    /// Отказ ПРОВАЙДЕРА сервер отдаёт 400 `provider_rejected`, а не 401:
    /// иначе одна неудачная привязка выкидывала бы человека на экран входа.
    func testLinkAppleProviderRejectedIsNotUnauthorized() async {
        StubURLProtocol.handler = { _ in
            (400, Data(#"{"error":{"code":"provider_rejected","message":"nonce не совпал"}}"#.utf8))
        }

        do {
            _ = try await client.linkApple(idToken: "bad", nonce: "raw", authorizationCode: nil)
            XCTFail("ожидали APIError.server(400)")
        } catch let error as APIError {
            XCTAssertFalse(error.isUnauthorized)
            XCTAssertEqual(identityErrorText(error), "Не удалось подтвердить аккаунт. Попробуйте ещё раз")
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
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

    /// 500 с кодом `internal` — сбой ДО tombstone (сервер ставит его первым
    /// шагом и с этого момента отвечает `purge_incomplete`). Аккаунт цел и
    /// нетронут, поэтому и трогать на устройстве нечего: раньше любой 500
    /// гнал `logout()`, и транзиентный сбой mongo уносил очередь
    /// неотправленных офлайн-расходов навсегда — при живом аккаунте.
    @MainActor
    func testDeleteAccountPreTombstoneFailureKeepsSessionAndOutbox() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        PendingJoin.shared.set("0123456789abcdef01234567")
        session.outbox.add(
            roomId: "room-1",
            payload: OutboxPayload(
                description: "Кофе",
                sum: 300,
                donorId: 77,
                recipientIds: [77],
                recipientSums: nil
            )
        )

        StubURLProtocol.handler = { _ in (500, Data(#"{"error":{"code":"internal","message":"сбой"}}"#.utf8)) }

        do {
            try await session.deleteAccount()
            XCTFail("ожидали ошибку удаления")
        } catch {
            // Текст не смеет обещать «данные с устройства удалены»: они на месте.
            let text = deleteAccountErrorText(error)
            XCTAssertFalse(text.contains("Данные с устройства удалены"), text)
        }

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertNotNil(session.me)
        XCTAssertNotNil(KeychainStore.read(key: "splitty.apiToken"))
        XCTAssertEqual(session.outbox.entries(roomId: "room-1").count, 1)

        session.logout()
    }

    /// 500 с кодом `purge_incomplete` — сбой ПОСЛЕ tombstone: аккаунт удалён,
    /// но его PII осталась в снимках комнат и побочных коллекциях. Доделать
    /// чистку может только повторный DELETE /me — маршрут висит на
    /// `authDeleted` ровно ради этого, и повторить его может ТОЛЬКО этот
    /// токен: `SoftDeleteUser` уже вычистил telegram_id/google_sub/apple_sub,
    /// так что войти заново нельзя. Выбросив токен (а раньше клиент делал
    /// именно это), мы навсегда закрывали единственный путь довести удаление
    /// до конца — то есть проваливали 5.1.1(v)/GDPR.
    @MainActor
    func testDeleteAccountPurgeIncompleteKeepsTokenForRetry() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"purge_incomplete","message":"аккаунт удалён, но очистка данных не завершена: повторите запрос"}}"#.utf8))
        }

        do {
            try await session.deleteAccount()
            XCTFail("ожидали ошибку удаления")
        } catch {
            // Совет обязан быть выполнимым: «повторите», а не «войдите снова».
            let text = deleteAccountErrorText(error)
            XCTAssertTrue(text.lowercased().contains("ещё раз"), text)
            XCTAssertFalse(text.lowercased().contains("войдите"), text)
        }

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertNotNil(KeychainStore.read(key: "splitty.apiToken"))

        // Повтор действительно возможен: тем же токеном, из той же сессии.
        StubURLProtocol.handler = { _ in (204, Data()) }
        try await session.deleteAccount()
        XCTAssertFalse(session.isAuthenticated)
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
    }

    // MARK: - Удаление аккаунта: сценарий ЭКРАНА, а не только хранилища

    /// Повтор удаления после `purge_incomplete` больше не уничтожает свой
    /// собственный токен.
    ///
    /// Инвариант «токен переживает `purge_incomplete`» держится в
    /// `SessionStore` и проверен тестом выше — но ломался ЭТАЖОМ ВЫШЕ, в
    /// сценарии экрана: тот безусловно звал отвязку push-токена ПЕРЕД
    /// удалением. На повторном нажатии аккаунт уже tombstone, а
    /// `DELETE /me/devices` висит на обычном `s.auth` — приходил 401,
    /// `onUnauthorized` звал `expireSession`, и токен исчезал из Keychain
    /// РАНЬШЕ, чем уходил повторный `DELETE /me`. Повтор летел без
    /// Authorization, получал 401, человека выбрасывало на экран входа — а
    /// войти он уже не мог (`SoftDeleteUser` вычистил все личности). Его PII
    /// оставалась в базе навсегда, то есть проваливалось ровно то требование
    /// 5.1.1(v)/GDPR, ради которого удаление и написано.
    @MainActor
    func testScreenRetryAfterPurgeIncompleteKeepsTokenAndSkipsDeviceUnregister() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        let push = SpyPushBinding()

        // Все пути, которые реально ушли в сеть: «запроса не было» иначе не
        // отличить от «запрос был, но его ответ проигнорировали».
        let paths = PathRecorder()
        StubURLProtocol.handler = { request in
            paths.record(request)
            return (500, Data(#"{"error":{"code":"purge_incomplete","message":"чистка не завершена"}}"#.utf8))
        }

        let firstError = await runAccountDeletion(session: session, push: push)
        XCTAssertNotNil(firstError)
        // Аккаунт был ЖИВ до этого нажатия — отвязка токена устройства законна.
        XCTAssertEqual(push.unregisterCalls, 1)
        // А обратной регистрации быть не должно: аккаунта уже нет.
        XCTAssertEqual(push.registerCalls, 0)
        XCTAssertTrue(session.isPurgePending)
        XCTAssertTrue(session.isAuthenticated)

        // Второе нажатие «Удалить аккаунт» — тот самый повтор.
        paths.reset()
        StubURLProtocol.handler = { request in
            paths.record(request)
            return (204, Data())
        }
        let secondError = await runAccountDeletion(session: session, push: push)

        XCTAssertNil(secondError)
        // Главное: повтор НЕ ходил в /me/devices. Именно этот запрос и сносил
        // токен, которым повтор только и мог быть выполнен.
        XCTAssertEqual(push.unregisterCalls, 1, "повтор не смеет отвязывать push-токен")
        XCTAssertFalse(
            paths.paths.contains { $0.contains("/me/devices") },
            "повтор ушёл в /me/devices: \(paths.paths)"
        )
        // Повтор ушёл ИМЕННО с токеном — без него сервер ответил бы 401.
        XCTAssertEqual(paths.authorizations, ["Bearer jwt-777"])
        XCTAssertEqual(paths.paths, ["/api/v1/me"])
        // И дошёл до 204: сессия закрыта по-настоящему, флаг снят.
        XCTAssertFalse(session.isAuthenticated)
        XCTAssertFalse(session.isPurgePending)
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
        XCTAssertFalse(UserDefaults.standard.bool(forKey: "splitty.purgePending"))
    }

    /// Пока чистка не доделана, 401 от ЛЮБОГО другого маршрута не смеет
    /// стирать токен. Это вторая половина той же дыры: чтобы её открыть,
    /// повтор нажимать не обязательно — аккаунт уже tombstone, и 401 отвечает
    /// каждый маршрут на `s.auth` (переключение вкладки, `refreshMe` на
    /// «Профиле», открытие группы). Первый же такой ответ уносил единственный
    /// ключ к `authDeleted`-маршруту.
    @MainActor
    func testUnauthorizedWhilePurgePendingKeepsToken() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"purge_incomplete","message":"чистка не завершена"}}"#.utf8))
        }
        try? await session.deleteAccount()
        XCTAssertTrue(session.isPurgePending)

        // Ровно тот запрос, который делала отвязка push-токена, — и любой
        // другой запрос удалённого аккаунта ведёт себя так же.
        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        try? await session.api.unregisterDevice(token: "fcm-token")
        // `onUnauthorized` асинхронный (Task на MainActor) — даём ему пройти.
        await settleMainActor()

        XCTAssertTrue(session.isAuthenticated, "401 стёр токен незавершённой чистки")
        XCTAssertEqual(KeychainStore.read(key: "splitty.apiToken"), "jwt-777")

        session.logout()
    }

    /// Обратная сторона: БЕЗ флага незавершённой чистки протухшая сессия
    /// обязана разлогинивать, как и раньше. Иначе «защита токена» превратилась
    /// бы в «приложение с мёртвым токеном никогда не показывает экран входа».
    @MainActor
    func testUnauthorizedWithoutPurgePendingStillLogsOut() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        XCTAssertFalse(session.isPurgePending)

        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        try? await session.api.unregisterDevice(token: "fcm-token")
        await settleMainActor()

        XCTAssertFalse(session.isAuthenticated)
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
    }

    /// Человек НЕ нажал «повторить», а просто ушёл с экрана или закрыл
    /// приложение. Флаг персистентный, поэтому корень (`RootView.task`) на
    /// следующем запуске доводит чистку сам: фонового реконсилятора на сервере
    /// нет, и без этого PII оставалась бы в базе навсегда.
    @MainActor
    func testRootFinishesPendingPurgeOnNextLaunch() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"purge_incomplete","message":"чистка не завершена"}}"#.utf8))
        }
        try? await session.deleteAccount()
        XCTAssertTrue(UserDefaults.standard.bool(forKey: "splitty.purgePending"))

        // Новый запуск приложения: и токен (Keychain), и флаг (UserDefaults)
        // поднимаются из хранилищ.
        let relaunched = SessionStore(urlSession: stubSession)
        XCTAssertTrue(relaunched.isAuthenticated)
        XCTAssertTrue(relaunched.isPurgePending)

        let paths = PathRecorder()
        StubURLProtocol.handler = { request in
            paths.record(request)
            return (204, Data())
        }
        await relaunched.finishPendingPurge()

        XCTAssertEqual(paths.paths, ["/api/v1/me"])
        XCTAssertEqual(paths.authorizations, ["Bearer jwt-777"])
        XCTAssertFalse(relaunched.isAuthenticated)
        XCTAssertFalse(relaunched.isPurgePending)
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
    }

    /// 401 на самом повторе — единственный терминальный исход: `authDeleted`
    /// пускает удалённых, и раз отказал он, токен мёртв по-настоящему.
    /// Повторять нечем, поэтому флаг снимается и сессия закрывается — иначе на
    /// устройстве навсегда осталась бы зомби-сессия, которую ничто не выгоняет
    /// на экран входа.
    @MainActor
    func testFinishPendingPurgeGivesUpOnUnauthorized() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"purge_incomplete","message":"чистка не завершена"}}"#.utf8))
        }
        try? await session.deleteAccount()
        XCTAssertTrue(session.isPurgePending)

        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"нет доступа"}}"#.utf8))
        }
        await session.finishPendingPurge()
        await settleMainActor()

        XCTAssertFalse(session.isAuthenticated)
        XCTAssertFalse(session.isPurgePending)
        XCTAssertNil(KeychainStore.read(key: "splitty.apiToken"))
    }

    /// Транспортный сбой на повторе не смеет ни закрывать сессию, ни снимать
    /// флаг: запрос до сервера не дошёл, чистка по-прежнему не доделана.
    @MainActor
    func testFinishPendingPurgeKeepsFlagOnTransportFailure() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"purge_incomplete","message":"чистка не завершена"}}"#.utf8))
        }
        try? await session.deleteAccount()

        StubURLProtocol.handler = nil
        StubURLProtocol.failure = { _ in URLError(.notConnectedToInternet) }
        await session.finishPendingPurge()

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertTrue(session.isPurgePending)
        XCTAssertEqual(KeychainStore.read(key: "splitty.apiToken"), "jwt-777")

        StubURLProtocol.failure = nil
        session.logout()
    }

    /// Сбой ДО tombstone (`internal`) флаг НЕ поднимает: аккаунт жив, повторять
    /// нечего, и защита токена от 401 здесь была бы вредной — обычное
    /// протухание сессии обязано разлогинивать. Плюс отвязанный push-токен
    /// регистрируется обратно: человек остаётся в живой сессии, и без этого он
    /// молча сидел бы без пушей до следующего входа.
    @MainActor
    func testScreenPreTombstoneFailureRestoresPushAndLeavesNoPendingFlag() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        let push = SpyPushBinding()

        StubURLProtocol.handler = { _ in
            (500, Data(#"{"error":{"code":"internal","message":"сбой"}}"#.utf8))
        }
        let error = await runAccountDeletion(session: session, push: push)

        XCTAssertNotNil(error)
        XCTAssertEqual(push.unregisterCalls, 1)
        XCTAssertEqual(push.registerCalls, 1, "аккаунт жив — пуши обязаны вернуться")
        XCTAssertFalse(session.isPurgePending)
        XCTAssertTrue(session.isAuthenticated)

        session.logout()
    }

    /// Офлайн: запрос до сервера НЕ ДОШЁЛ вовсе. Сомнения «удалилось или нет»
    /// здесь не существует — аккаунт заведомо жив, — а `logout()` стирает
    /// outbox и кеш. «Удалить аккаунт», нажатое в метро, уносило очередь
    /// неотправленных расходов навсегда и выкидывало из живой сессии.
    @MainActor
    func testDeleteAccountOfflineKeepsSessionAndOutbox() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        PendingJoin.shared.set("0123456789abcdef01234567")
        session.outbox.add(
            roomId: "room-1",
            payload: OutboxPayload(
                description: "Кофе",
                sum: 300,
                donorId: 77,
                recipientIds: [77],
                recipientSums: nil
            )
        )

        StubURLProtocol.failure = { _ in URLError(.notConnectedToInternet) }

        do {
            try await session.deleteAccount()
            XCTFail("ожидали ошибку удаления")
        } catch {
            // Текст обязан быть про связь: обещать «данные с устройства
            // удалены» здесь — прямая ложь, они на месте.
            let text = deleteAccountErrorText(error)
            XCTAssertFalse(text.contains("Данные с устройства удалены"), text)
            XCTAssertTrue(text.lowercased().contains("соединени"), text)
        }

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertNotNil(session.me)
        XCTAssertNotNil(KeychainStore.read(key: "splitty.apiToken"))
        XCTAssertEqual(session.outbox.entries(roomId: "room-1").count, 1)
        XCTAssertNotNil(PendingJoin.shared.roomId)

        StubURLProtocol.failure = nil
        session.logout()
    }

    /// 403 — единственное исключение среди ОТВЕТОВ сервера: он осознанно
    /// отказался удалять демонстрационный аккаунт ревьюеров. Тот жив, сессия
    /// цела, и выкидывать человека на экран входа не за что.
    @MainActor
    func testDeleteAccountForbiddenKeepsSession() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (403, Data(#"{"error":{"code":"forbidden","message":"Демонстрационный аккаунт удалить нельзя"}}"#.utf8))
        }

        do {
            try await session.deleteAccount()
            XCTFail("ожидали ошибку удаления")
        } catch {
            XCTAssertEqual(deleteAccountErrorText(error), "Демонстрационный аккаунт удалить нельзя")
        }

        XCTAssertTrue(session.isAuthenticated)
        XCTAssertNotNil(session.me)

        session.logout()
    }

    /// Текст неудавшегося удаления говорит, что делать дальше: «Ошибка сервера
    /// (500)» человеку в этот момент не говорит ничего. И ни одна ветка больше
    /// не смеет обещать «данные с устройства удалены» — сессия при любой
    /// ошибке остаётся живой.
    func testDeleteAccountErrorTextTellsWhatToDoNext() {
        let preTombstone = deleteAccountErrorText(
            APIError.server(status: 500, code: "internal", message: "сбой")
        )
        XCTAssertFalse(preTombstone.contains("Данные с устройства удалены"), preTombstone)
        XCTAssertFalse(preTombstone.contains("500"), preTombstone)
        XCTAssertTrue(preTombstone.lowercased().contains("на месте"), preTombstone)

        let postTombstone = deleteAccountErrorText(
            APIError.server(status: 500, code: "purge_incomplete", message: "не завершена")
        )
        XCTAssertFalse(postTombstone.contains("Данные с устройства удалены"), postTombstone)
        XCTAssertTrue(postTombstone.lowercased().contains("ещё раз"), postTombstone)
    }

    // MARK: - SessionStore: привязка и параллельный /me

    /// Отказ провайдера (400 `provider_rejected`) не должен разлогинивать:
    /// выход делает только 401, а он после смены контракта означает мёртвую
    /// сессию Splitty, а не «Apple не подтвердил токен».
    @MainActor
    func testProviderRejectedDoesNotLogOut() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { _ in
            (400, Data(#"{"error":{"code":"provider_rejected","message":"nonce не совпал"}}"#.utf8))
        }

        do {
            try await session.linkApple(idToken: "bad", nonce: "raw", authorizationCode: "code")
            XCTFail("ожидали APIError.server(400)")
        } catch {
            XCTAssertEqual(identityErrorText(error), "Не удалось подтвердить аккаунт. Попробуйте ещё раз")
        }

        XCTAssertTrue(session.isAuthenticated)

        session.logout()
    }

    /// Экран «Профиль» стартует `refreshMe` в `.task`, и пользователь успевает
    /// нажать «Привязать» до того, как ответ придёт. Ответ /me в этот момент
    /// старее — стереть им только что привязанный способ входа нельзя.
    @MainActor
    func testStaleMeResponseDoesNotClobberFreshLink() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        StubURLProtocol.handler = { request in
            if request.url?.path == "/api/v1/me/link/apple" {
                return (200, Data(#"""
                {"user":{"id":77,"username":null,"displayName":"Аня","lang":"ru",
                 "linkedProviders":["google","apple"],"notificationOn":true}}
                """#.utf8))
            }
            // Профиль, каким он был ДО привязки.
            return (200, Data(#"""
            {"id":77,"username":null,"displayName":"Аня","lang":"ru",
             "linkedProviders":["google"],"notificationOn":true}
            """#.utf8))
        }
        // /me отвечает заведомо позже привязки — иначе гонка не воспроизводится.
        StubURLProtocol.responseDelay = { $0.url?.path == "/api/v1/me" ? 0.4 : nil }

        async let refresh: Void = session.refreshMe()
        try await session.linkApple(idToken: "ok", nonce: "raw", authorizationCode: "code")
        XCTAssertTrue(session.me?.isLinked(.apple) == true)
        await refresh

        XCTAssertTrue(
            session.me?.isLinked(.apple) == true,
            "устаревший ответ /me стёр только что привязанный Apple"
        )

        session.logout()
    }

    // MARK: - SessionStore: намерение решается своим владельцем, а не сменой аккаунта

    /// `expireSession` намерение сохраняет намеренно (тот же человек
    /// переавторизуется и дойдёт до группы), но если на устройстве вошёл
    /// ДРУГОЙ аккаунт — приглашение принадлежало предыдущему, и исполнять
    /// его от чужого имени нельзя.
    @MainActor
    func testForeignPendingJoinDroppedOnSignIn() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        // Ссылку открыл A, будучи в аккаунте: владелец записан (так делает
        // `SplittyApp.handleJoinLink`).
        PendingJoin.shared.set("0123456789abcdef01234567", ownerId: 77)
        session.expireSession()
        // Сессия протухла, но намерение ждёт — так и задумано.
        XCTAssertEqual(PendingJoin.shared.roomId, "0123456789abcdef01234567")

        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"jwt-888","user":{"id":88,"username":null,"displayName":"Пётр","lang":"ru",
             "linkedProviders":["google"],"notificationOn":true}}
            """#.utf8))
        }
        try await session.loginWithCode("EFGH6789")

        XCTAssertNil(PendingJoin.shared.roomId, "чужое приглашение пережило смену аккаунта")

        session.logout()
    }

    /// Обратная сторона той же проверки, и ломалось тут молча. Ссылку открыл
    /// ГОСТЬ — владельца у намерения нет, и оно обязано достаться первому же
    /// вошедшему: это и есть путь приглашения. Раньше `adoptOwner` стирал
    /// намерение на ЛЮБОЙ смене `ownerUserId`, а `expireSession` оставляет
    /// id прошлого аккаунта — поэтому «A разлогинило по 401 → гость B открыл
    /// ссылку → B вошёл» выглядело сменой владельца, и приглашение самого B
    /// удалялось детерминированно: `adoptOwner` отрабатывает синхронно внутри
    /// `loginWith*`, до того как SwiftUI доставит `.onChange(of:
    /// session.isAuthenticated)`, так что `RootView` не находил уже ничего.
    @MainActor
    func testGuestPendingJoinSurvivesSignInAfterExpiry() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)

        // Сессия A протухла: ownerUserId остаётся равным 77.
        session.expireSession()
        // Ссылку открыл гость: живой сессии нет, владельца у намерения тоже.
        PendingJoin.shared.set("0123456789abcdef01234567")

        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"jwt-888","user":{"id":88,"username":null,"displayName":"Пётр","lang":"ru",
             "linkedProviders":["google"],"notificationOn":true}}
            """#.utf8))
        }
        try await session.loginWithCode("EFGH6789")

        XCTAssertEqual(
            PendingJoin.shared.roomId,
            "0123456789abcdef01234567",
            "своё приглашение гостя не пережило вход"
        )
        // И оно теперь принадлежит вошедшему: следующая смена аккаунта его уже
        // выбросит, а `RootView` пропустит к вступлению.
        XCTAssertEqual(PendingJoin.shared.ownerUserId, 88)

        session.logout()
    }

    /// Явный выход намерение стирает целиком — в отличие от протухшей сессии.
    /// Следующий человек на устройстве не должен даже узнать, что оно было.
    @MainActor
    func testLogoutClearsPendingJoinEntirely() async throws {
        let session = SessionStore(urlSession: stubSession)
        try await login(session)
        PendingJoin.shared.set("0123456789abcdef01234567", ownerId: 77)

        session.logout()

        XCTAssertNil(PendingJoin.shared.roomId)
        XCTAssertNil(PendingJoin.shared.ownerUserId)
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

    /// Даёт отработать `Task { @MainActor … }`, поставленному в очередь из
    /// `APIClient.onUnauthorized`: без этого проверка «токен на месте» проходила
    /// бы просто потому, что сброс ещё не успел случиться.
    @MainActor
    private func settleMainActor() async {
        for _ in 0..<10 {
            await Task.yield()
        }
        try? await Task.sleep(nanoseconds: 50_000_000)
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

// MARK: - Дублёры для сценария экрана

/// Подставная привязка push-токена: считает вызовы вместо похода в Firebase.
/// Настоящий `PushManager` в юнит-тестах молчит (нет Firebase и FCM-токена),
/// поэтому без дублёра «отвязка не вызывалась» было бы неотличимо от
/// «вызывалась, но ничего не сделала» — и тест проходил бы на сломанном коде.
final class SpyPushBinding: PushTokenBinding {
    private(set) var unregisterCalls = 0
    private(set) var registerCalls = 0

    func unregisterCurrentToken() async {
        unregisterCalls += 1
    }

    func registerCurrentToken() {
        registerCalls += 1
    }
}

/// Запоминает ВСЕ ушедшие в сеть запросы: путь и заголовок Authorization.
/// `StubURLProtocol.lastRequest` хранит только последний, а проверять надо
/// именно отсутствие лишнего запроса и наличие токена в повторе.
final class PathRecorder: @unchecked Sendable {
    private let lock = NSLock()
    private var storedPaths: [String] = []
    private var storedAuthorizations: [String] = []

    var paths: [String] {
        lock.withLock { storedPaths }
    }

    var authorizations: [String] {
        lock.withLock { storedAuthorizations }
    }

    func record(_ request: URLRequest) {
        lock.withLock {
            storedPaths.append(request.url?.path ?? "")
            storedAuthorizations.append(request.value(forHTTPHeaderField: "Authorization") ?? "")
        }
    }

    func reset() {
        lock.withLock {
            storedPaths.removeAll()
            storedAuthorizations.removeAll()
        }
    }
}
