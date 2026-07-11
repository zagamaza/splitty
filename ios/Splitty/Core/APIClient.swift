import Foundation

// MARK: - Ошибки API

/// Ошибка работы с REST API. `localizedDescription` — по-русски,
/// для ошибок бэкенда берётся `message` из тела `{"error": {code, message}}`.
enum APIError: LocalizedError {
    /// Не удалось собрать URL запроса (битый baseURL).
    case invalidURL
    /// Сетевая ошибка (нет соединения, таймаут и т.п.).
    case transport(Error)
    /// Ошибка бэкенда: HTTP-статус + code/message из тела ответа.
    case server(status: Int, code: String, message: String)
    /// Не удалось разобрать ответ сервера.
    case decoding(Error)

    /// true для 401 — сессию нужно сбросить.
    var isUnauthorized: Bool {
        if case .server(let status, _, _) = self {
            return status == 401
        }
        return false
    }

    var errorDescription: String? {
        switch self {
        case .invalidURL:
            return "Некорректный адрес сервера"
        case .transport:
            return "Нет соединения с сервером"
        case .server(let status, let code, let message):
            return message.isEmpty ? Self.fallbackMessage(status: status, code: code) : message
        case .decoding:
            return "Не удалось обработать ответ сервера"
        }
    }

    private static func fallbackMessage(status: Int, code: String) -> String {
        switch code {
        case "validation":
            return "Некорректные данные"
        case "unauthorized":
            return "Требуется вход"
        case "forbidden":
            return "Нет доступа"
        case "not_found":
            return "Не найдено"
        case "conflict":
            return "Действие сейчас невозможно"
        default:
            return "Ошибка сервера (\(status))"
        }
    }
}

extension Error {
    /// true, если ошибка — отмена задачи (`CancellationError` / `URLError.cancelled`,
    /// в том числе завёрнутые в `APIError.transport`). Такие ошибки VM молча
    /// игнорируют: это не сбой сети, а уход с экрана/перезапуск `.task`.
    var isTaskCancellation: Bool {
        if self is CancellationError {
            return true
        }
        if let urlError = self as? URLError, urlError.code == .cancelled {
            return true
        }
        if let apiError = self as? APIError, case let .transport(underlying) = apiError {
            return underlying.isTaskCancellation
        }
        return false
    }
}

// MARK: - Тела запросов операций (контракт v2)

/// Способ деления расхода в теле POST/PUT операции.
enum ExpenseSplit {
    /// Поровну между `recipientIds`: сервер раскладывает доли канонически
    /// (base = S/n, остаток по рублю первым получателям массива).
    case equally(recipientIds: [Int])
    /// Точными суммами: сервер валидирует Σ сумм == сумме операции (400 иначе).
    case byExactAmount(recipientSums: [RecipientSum])
}

/// Доля получателя в теле запроса `by_exact_amount` (целые рубли).
/// Codable: переиспользуется в payload офлайн-outbox (см. `OutboxPayload`).
struct RecipientSum: Codable, Hashable {
    let userId: Int
    let sum: Int
}

/// Тело POST/PUT операции: ровно ОДНО из полей `recipientIds`/`recipientSums`
/// (nil-поле не сериализуется — режим определяет сервер по наличию поля).
/// `clientOpId` — опциональный идемпотентный ключ создания (отправка из
/// офлайн-outbox): на повтор сервер отвечает 200 существующей операцией.
struct OperationBody: Encodable {
    let description: String
    let sum: Int
    let donorId: Int
    let recipientIds: [Int]?
    let recipientSums: [RecipientSum]?
    let clientOpId: String?

    init(description: String, sum: Int, donorId: Int, split: ExpenseSplit, clientOpId: String? = nil) {
        self.description = description
        self.sum = sum
        self.donorId = donorId
        self.clientOpId = clientOpId
        switch split {
        case .equally(let ids):
            recipientIds = ids
            recipientSums = nil
        case .byExactAmount(let sums):
            recipientIds = nil
            recipientSums = sums
        }
    }
}

// MARK: - Клиент

/// Клиент REST API Splitty (контракт — docs/API.md).
/// Все методы бросают `APIError`.
final class APIClient {
    /// nil — адрес сервера невалиден: каждый запрос бросит `APIError.invalidURL`
    /// (никакой тихой подмены дефолтным адресом).
    private let baseURL: URL?
    private let token: String?
    private let urlSession: URLSession = .shared
    private let decoder: JSONDecoder
    private let encoder = JSONEncoder()

    /// Вызывается при любом ответе 401 (протухший/невалидный токен):
    /// SessionStore сбрасывает сессию (см. `SessionStore.api`).
    var onUnauthorized: (() -> Void)?

    init(baseURL: URL?, token: String?) {
        self.baseURL = baseURL
        self.token = token
        let decoder = JSONDecoder()
        // Бэкенд шлёт даты в RFC3339 («2026-07-05T12:00:00Z»),
        // возможно с долями секунды — поддерживаем оба варианта.
        decoder.dateDecodingStrategy = .custom { d in
            let container = try d.singleValueContainer()
            let string = try container.decode(String.self)
            if let date = Self.rfc3339.date(from: string) ?? Self.rfc3339Fractional.date(from: string) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Некорректная дата RFC3339: \(string)"
            )
        }
        self.decoder = decoder
    }

    private static let rfc3339: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    private static let rfc3339Fractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    // MARK: Auth

    func devLogin(userId: Int, displayName: String, username: String?) async throws -> AuthResponse {
        struct Body: Encodable {
            let userId: Int
            let displayName: String
            let username: String?
        }
        return try await request(
            "POST", "/api/v1/auth/dev",
            body: Body(userId: userId, displayName: displayName, username: username)
        )
    }

    /// Вход по одноразовому коду из Telegram-бота (@split_money_bot):
    /// POST /auth/code, тело `{"code": "ABCD2345"}`, без авторизационного
    /// заголовка (клиент на экране входа создаётся с token == nil).
    /// 401 `invalid_code` — код неверный, просроченный или уже использованный.
    func loginWithCode(_ code: String) async throws -> AuthResponse {
        struct Body: Encodable {
            let code: String
        }
        return try await request("POST", "/api/v1/auth/code", body: Body(code: code))
    }

    // MARK: Профиль

    func me() async throws -> Me {
        try await request("GET", "/api/v1/me")
    }

    func updateMe(displayName: String?, lang: String?, notificationOn: Bool?) async throws -> Me {
        struct Body: Encodable {
            let displayName: String?
            let lang: String?
            let notificationOn: Bool?
        }
        return try await request(
            "PATCH", "/api/v1/me",
            body: Body(displayName: displayName, lang: lang, notificationOn: notificationOn)
        )
    }

    // MARK: Комнаты (группы)

    func rooms(archived: Bool) async throws -> [RoomSummary] {
        try await request(
            "GET", "/api/v1/rooms",
            query: [URLQueryItem(name: "archived", value: archived ? "true" : "false")]
        )
    }

    func createRoom(name: String) async throws -> RoomDetail {
        struct Body: Encodable {
            let name: String
        }
        return try await request("POST", "/api/v1/rooms", body: Body(name: name))
    }

    func room(id: String) async throws -> RoomDetail {
        try await request("GET", "/api/v1/rooms/\(id)")
    }

    func joinRoom(id: String) async throws -> RoomDetail {
        try await request("POST", "/api/v1/rooms/\(id)/join")
    }

    func archiveRoom(id: String) async throws {
        try await send("POST", "/api/v1/rooms/\(id)/archive")
    }

    func unarchiveRoom(id: String) async throws {
        try await send("POST", "/api/v1/rooms/\(id)/unarchive")
    }

    // MARK: Валюты

    /// Справочник валют для пикера настроек группы.
    func currencies() async throws -> [CurrencyInfo] {
        try await request("GET", "/api/v1/currencies")
    }

    /// Смена валюты комнаты: PUT /rooms/{id}/currency, ответ 204 без тела.
    func setRoomCurrency(roomId: String, currency: String) async throws {
        struct Body: Encodable {
            let currency: String
        }
        try await send("PUT", "/api/v1/rooms/\(roomId)/currency", body: Body(currency: currency))
    }

    // MARK: Операции (расходы)

    func operations(roomId: String, type: String) async throws -> [Operation] {
        try await request(
            "GET", "/api/v1/rooms/\(roomId)/operations",
            query: [URLQueryItem(name: "type", value: type)]
        )
    }

    /// `clientOpId` — идемпотентный ключ создания (uuid записи outbox):
    /// повтор с тем же ключом возвращает 200 + существующую операцию (без дубля).
    func addOperation(
        roomId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        clientOpId: String? = nil
    ) async throws -> Operation {
        try await request(
            "POST", "/api/v1/rooms/\(roomId)/operations",
            body: OperationBody(
                description: description,
                sum: sum,
                donorId: donorId,
                split: split,
                clientOpId: clientOpId
            )
        )
    }

    func updateOperation(
        roomId: String,
        operationId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit
    ) async throws -> Operation {
        try await request(
            "PUT", "/api/v1/rooms/\(roomId)/operations/\(operationId)",
            body: OperationBody(description: description, sum: sum, donorId: donorId, split: split)
        )
    }

    func deleteOperation(roomId: String, operationId: String) async throws {
        try await send("DELETE", "/api/v1/rooms/\(roomId)/operations/\(operationId)")
    }

    // MARK: Долги и погашение

    func debts(roomId: String, involving: String) async throws -> [Debt] {
        try await request(
            "GET", "/api/v1/rooms/\(roomId)/debts",
            query: [URLQueryItem(name: "involving", value: involving)]
        )
    }

    func repay(roomId: String, debtorId: Int, lenderId: Int, sum: Int) async throws -> Operation {
        struct Body: Encodable {
            let debtorId: Int
            let lenderId: Int
            let sum: Int
        }
        return try await request(
            "POST", "/api/v1/rooms/\(roomId)/repayments",
            body: Body(debtorId: debtorId, lenderId: lenderId, sum: sum)
        )
    }

    // MARK: Друзья, активность, статистика

    func friends() async throws -> [FriendBalance] {
        try await request("GET", "/api/v1/friends")
    }

    func activity(limit: Int, offset: Int) async throws -> [ActivityItem] {
        try await request(
            "GET", "/api/v1/activity",
            query: [
                URLQueryItem(name: "limit", value: String(limit)),
                URLQueryItem(name: "offset", value: String(offset)),
            ]
        )
    }

    func statistics(roomId: String) async throws -> Statistics {
        try await request("GET", "/api/v1/rooms/\(roomId)/statistics")
    }

    // MARK: Файлы

    /// Скачивает вложение операции (чек/фото/видео):
    /// GET /api/v1/files/{fileId}, возвращает сырые байты файла.
    func fileData(id: String) async throws -> Data {
        try await send("GET", "/api/v1/files/\(id)")
    }

    // MARK: - Внутреннее

    private struct ErrorEnvelope: Decodable {
        struct Payload: Decodable {
            let code: String
            let message: String
        }

        let error: Payload
    }

    private func request<T: Decodable>(
        _ method: String,
        _ path: String,
        query: [URLQueryItem] = [],
        body: (any Encodable)? = nil
    ) async throws -> T {
        let data = try await send(method, path, query: query, body: body)
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            throw APIError.decoding(error)
        }
    }

    /// Выполняет запрос, проверяет статус, возвращает сырое тело ответа.
    @discardableResult
    private func send(
        _ method: String,
        _ path: String,
        query: [URLQueryItem] = [],
        body: (any Encodable)? = nil
    ) async throws -> Data {
        guard let baseURL,
              var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw APIError.invalidURL
        }
        // Не затираем path-префикс baseURL: сервер может жить за реверс-прокси
        // («https://host/splitty» + «/api/v1/…» = «/splitty/api/v1/…»).
        let basePath = components.path.hasSuffix("/")
            ? String(components.path.dropLast())
            : components.path
        components.path = basePath + path
        if !query.isEmpty {
            components.queryItems = query
        }
        guard let url = components.url else {
            throw APIError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = method
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try encoder.encode(body)
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await urlSession.data(for: request)
        } catch {
            throw APIError.transport(error)
        }

        guard let http = response as? HTTPURLResponse else {
            throw APIError.transport(URLError(.badServerResponse))
        }
        guard (200..<300).contains(http.statusCode) else {
            if http.statusCode == 401 {
                // Мёртвая сессия: централизованный выход из любого запроса.
                onUnauthorized?()
            }
            if let envelope = try? decoder.decode(ErrorEnvelope.self, from: data) {
                throw APIError.server(
                    status: http.statusCode,
                    code: envelope.error.code,
                    message: envelope.error.message
                )
            }
            throw APIError.server(status: http.statusCode, code: "", message: "")
        }
        return data
    }
}
