import XCTest
@testable import Splitty

/// Подставной транспорт: перехватывает ЛЮБОЙ запрос сессии и отвечает тем,
/// что положили в `handler`. Регистрируется не глобально, а в конфигурации
/// конкретной `URLSession` (`protocolClasses`) — иначе стаб пережил бы тест
/// и отвечал за соседние.
final class StubURLProtocol: URLProtocol {
    /// Возвращает статус, тело и запомненный запрос. Ставится в `setUp`.
    /// nonisolated(unsafe): тесты в классе идут последовательно, а протокол
    /// создаётся системой и до `handler` дотягивается только из него.
    nonisolated(unsafe) static var handler: ((URLRequest) -> (Int, Data))?
    /// Последний перехваченный запрос — по нему проверяются метод, путь и тело.
    nonisolated(unsafe) static var lastRequest: URLRequest?
    /// Тело запроса отдельно: `URLProtocol` затирает `httpBody` потоком,
    /// поэтому его снимают до подмены (см. `canonicalRequest`).
    nonisolated(unsafe) static var lastBody: Data?
    /// Задержка ответа на конкретный запрос, секунды (nil — ответить сразу).
    /// Нужна тестам на ГОНКУ двух запросов: без неё быстрый ответ приходит
    /// раньше, чем тест успевает отправить второй запрос, и проверяемое
    /// перекрытие просто не воспроизводится.
    nonisolated(unsafe) static var responseDelay: ((URLRequest) -> TimeInterval?)?
    /// Транспортный сбой ВМЕСТО ответа (nil — отвечать как обычно). Нужен
    /// тестам, для которых принципиально, что запрос до сервера не дошёл:
    /// 5xx для них — принципиально другой случай (сервер запрос обработал).
    nonisolated(unsafe) static var failure: ((URLRequest) -> Error?)?

    override class func canInit(with request: URLRequest) -> Bool { true }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        // httpBodyStream читается один раз, поэтому тело снимаем здесь —
        // на этой стадии URLRequest ещё несёт httpBody.
        lastBody = request.httpBody ?? request.httpBodyStream.map(readAll)
        return request
    }

    override func startLoading() {
        Self.lastRequest = request
        if let error = Self.failure?(request) {
            client?.urlProtocol(self, didFailWithError: error)
            return
        }
        let (status, body) = Self.handler?(request) ?? (200, Data())
        let finish = { [weak self] in
            guard let self else { return }
            let response = HTTPURLResponse(
                url: self.request.url!,
                statusCode: status,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"]
            )!
            self.client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            self.client?.urlProtocol(self, didLoad: body)
            self.client?.urlProtocolDidFinishLoading(self)
        }
        // Отвечаем отложенно (и не блокируя поток загрузки), только если тест
        // об этом попросил: иначе второй запрос не успел бы уйти в полёт.
        if let delay = Self.responseDelay?(request), delay > 0 {
            DispatchQueue.global().asyncAfter(deadline: .now() + delay, execute: finish)
        } else {
            finish()
        }
    }

    override func stopLoading() {}

    private static func readAll(_ stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}

/// Вход через сторонние провайдеры на уровне APIClient: путь, тело и разбор
/// ответа. Сеть не трогается — транспорт подменён (`StubURLProtocol`).
final class APIClientAuthTests: XCTestCase {
    private var client: APIClient!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        client = APIClient(
            baseURL: URL(string: "https://api.example.test"),
            token: nil,
            urlSession: URLSession(configuration: configuration)
        )
    }

    override func tearDown() {
        client = nil
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil
        super.tearDown()
    }

    // MARK: loginWithGoogle — успех

    func testLoginWithGoogleSendsIdTokenAndParsesResponse() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"jwt-123","user":{"id":1000000000001,"username":null,
             "displayName":"Аня","lang":"ru","notificationOn":true}}
            """#.utf8))
        }

        let response = try await client.loginWithGoogle(idToken: "google-id-token")

        XCTAssertEqual(response.token, "jwt-123")
        XCTAssertEqual(response.user.id, 1_000_000_000_001)
        XCTAssertEqual(response.user.displayName, "Аня")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/auth/google")
        // Экран входа работает без токена — заголовок авторизации слать нечем
        // и не нужно: 401 от чужого протухшего JWT сорвал бы вход.
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: body) as? [String: Any]
        )
        // Ровно одно поле: имя и почта живут ВНУТРИ подписанного токена,
        // и сервер берёт их оттуда — клиент их не диктует.
        XCTAssertEqual(json["idToken"] as? String, "google-id-token")
        XCTAssertEqual(json.count, 1)
    }

    // MARK: loginWithGoogle — 401

    func testLoginWithGoogleUnauthorized() async {
        StubURLProtocol.handler = { _ in
            (401, Data(#"{"error":{"code":"unauthorized","message":"не удалось проверить токен Google"}}"#.utf8))
        }

        do {
            _ = try await client.loginWithGoogle(idToken: "bad-token")
            XCTFail("ожидали APIError.server(401)")
        } catch let error as APIError {
            XCTAssertTrue(error.isUnauthorized)
            XCTAssertEqual(error.errorDescription, "не удалось проверить токен Google")
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
    }

    // MARK: loginWithGoogle — 503 (вход через Google не сконфигурирован)

    func testLoginWithGoogleUnavailableIsNotUnauthorized() async {
        StubURLProtocol.handler = { _ in
            (503, Data(#"{"error":{"code":"unavailable","message":"вход через Google не сконфигурирован"}}"#.utf8))
        }

        do {
            _ = try await client.loginWithGoogle(idToken: "any")
            XCTFail("ожидали APIError.server(503)")
        } catch let error as APIError {
            // 503 не должен трактоваться как протухшая сессия — иначе клиент
            // выкинул бы пользователя из аккаунта из-за настройки сервера.
            XCTAssertFalse(error.isUnauthorized)
        } catch {
            XCTFail("ожидали APIError, получили \(error)")
        }
    }

    // MARK: loginWithApple — тот же транспорт, полный набор полей

    func testLoginWithAppleSendsAllFields() async throws {
        StubURLProtocol.handler = { _ in
            (200, Data(#"""
            {"token":"jwt-apple","user":{"id":1000000000002,"username":null,
             "displayName":"Пётр","lang":"ru","notificationOn":false}}
            """#.utf8))
        }

        let response = try await client.loginWithApple(
            idToken: "apple-id-token",
            displayName: "Пётр",
            nonce: "raw-nonce",
            authorizationCode: "auth-code"
        )

        XCTAssertEqual(response.token, "jwt-apple")

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/api/v1/auth/apple")

        let body = try XCTUnwrap(StubURLProtocol.lastBody)
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: body) as? [String: Any]
        )
        // nonce уходит СЫРЫМ: в токене лежит его SHA256, сервер сверяет одно
        // с другим. Отправь мы хеш — совпадения не было бы никогда.
        XCTAssertEqual(json["nonce"] as? String, "raw-nonce")
        XCTAssertEqual(json["idToken"] as? String, "apple-id-token")
        XCTAssertEqual(json["displayName"] as? String, "Пётр")
        XCTAssertEqual(json["authorizationCode"] as? String, "auth-code")
    }
}
