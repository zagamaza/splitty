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

    /// true для 403 — сервер осознанно отказал живому аккаунту (например,
    /// удаление демонстрационного профиля ревьюеров). В отличие от 5xx и
    /// сетевых сбоев, здесь точно известно, что на сервере ничего не
    /// изменилось, — см. `SessionStore.deleteAccount`.
    var isForbidden: Bool {
        if case .server(let status, _, _) = self {
            return status == 403
        }
        return false
    }

    /// true — `DELETE /me` упал уже ПОСЛЕ tombstone: аккаунт удалён, а чистка
    /// данных не завершена. От обычного `internal` (сбой ДО tombstone, аккаунт
    /// цел и нетронут) отличается только кодом, и это различие критично:
    /// доделать чистку может лишь повторный запрос ЭТИМ ЖЕ токеном — маршрут
    /// висит на `authDeleted` ровно ради повтора, а войти заново нельзя,
    /// личности вычищены. См. `SessionStore.deleteAccount`.
    var isPurgeIncomplete: Bool {
        if case .server(_, let code, _) = self {
            return code == "purge_incomplete"
        }
        return false
    }

    /// true — сервер запрос ПОЛУЧИЛ и ответил (валидным телом или мусором,
    /// который не разобрался). false — запрос до сервера не дошёл вовсе (нет
    /// сети, таймаут, DNS, битый baseURL), и на той стороне заведомо ничего
    /// не изменилось.
    ///
    /// Отличать обязательно там, где неудача ведёт к УНИЧТОЖЕНИЮ локальных
    /// данных: `SessionStore.deleteAccount` стирает outbox и кеш, потому что
    /// «удаление прошло, а ответ потерялся» с клиента неотличимо от ошибки, —
    /// но офлайн такого сомнения нет, аккаунт заведомо жив, а очередь
    /// неотправленных расходов пропала бы навсегда.
    var isServerResponse: Bool {
        switch self {
        case .server, .decoding:
            return true
        case .invalidURL, .transport:
            return false
        }
    }

    var errorDescription: String? {
        switch self {
        case .invalidURL:
            return String(localized: "Некорректный адрес сервера")
        case .transport:
            return String(localized: "Нет соединения с сервером")
        case .server(let status, let code, let message):
            return message.isEmpty ? Self.fallbackMessage(status: status, code: code) : message
        case .decoding:
            return String(localized: "Не удалось обработать ответ сервера")
        }
    }

    private static func fallbackMessage(status: Int, code: String) -> String {
        switch code {
        case "validation":
            return String(localized: "Некорректные данные")
        case "unauthorized":
            return String(localized: "Требуется вход")
        case "forbidden":
            return String(localized: "Нет доступа")
        case "not_found":
            return String(localized: "Не найдено")
        case "conflict":
            return String(localized: "Действие сейчас невозможно")
        // Коды распознавания и троттлинга. Сюда попадаем, только когда тело
        // пустое (ответил прокси, а не приложение), но «Ошибка сервера (429)»
        // не говорит человеку ничего. Тексты — те же, что на Android: одна
        // ошибка не должна объясняться на платформах по-разному.
        case "too_large":
            return String(localized: "Слишком большой запрос")
        case "unsupported_media":
            return String(localized: "Неподдерживаемый формат файла")
        case "rate_limited":
            return String(localized: "Слишком много запросов. Попробуйте позже")
        case "ai_disabled":
            return String(localized: "Распознавание сейчас недоступно")
        default:
            return String(localized: "Ошибка сервера (\(status))")
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
    /// Позиции чека itemized-операции: сервер выводит суммы из них и игнорирует
    /// плоские `recipientSums`. nil — обычная (плоская) операция (не сериализуется).
    let items: [OperationItem]?
    let clientOpId: String?

    init(
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]? = nil,
        clientOpId: String? = nil
    ) {
        self.description = description
        self.sum = sum
        self.donorId = donorId
        self.items = items
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

// MARK: - Протокол write-path операций (шов для тестов)

/// Узкий контракт создания/правки/удаления операций, от которого зависит
/// `OutboxStore` при синхронизации. Позволяет подставить фейк в тестах офлайн-
/// раундтрипа (сам `APIClient` — `final` с приватной `URLSession`). В проде
/// реализуется `APIClient`, поведение идентично.
protocol OperationAPI {
    func addOperation(
        roomId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]?,
        clientOpId: String?
    ) async throws -> Operation

    func updateOperation(
        roomId: String,
        operationId: String,
        description: String,
        sum: Int,
        donorId: Int,
        split: ExpenseSplit,
        items: [OperationItem]?
    ) async throws -> Operation

    func deleteOperation(roomId: String, operationId: String) async throws
}

// MARK: - Клиент

/// Клиент REST API Splitty (контракт — docs/API.md).
/// Все методы бросают `APIError`.
final class APIClient: OperationAPI {
    /// nil — адрес сервера невалиден: каждый запрос бросит `APIError.invalidURL`
    /// (никакой тихой подмены дефолтным адресом).
    private let baseURL: URL?
    private let token: String?
    /// Шов для тестов: в проде всегда `.shared`, в тестах — сессия с
    /// подставленным `URLProtocol`. Через `URLProtocol.registerClass`
    /// перехватить `.shared` нельзя надёжно (общая конфигурация переживает
    /// тест и течёт в соседние), поэтому транспорт передаётся явно.
    private let urlSession: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    /// Вызывается при любом ответе 401 (протухший/невалидный токен):
    /// SessionStore сбрасывает сессию (см. `SessionStore.api`).
    var onUnauthorized: (() -> Void)?

    init(baseURL: URL?, token: String?, urlSession: URLSession = .shared) {
        self.baseURL = baseURL
        self.token = token
        self.urlSession = urlSession
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
        let encoder = JSONEncoder()
        // Зеркало декодера. Дефолтная стратегия шлёт дату ЧИСЛОМ, а `time.Time`
        // на сервере разбирается только из строки RFC3339 — отметка
        // прочитанного (`markNotificationsSeen`) отвечала бы 400 всегда.
        encoder.dateEncodingStrategy = .custom { date, e in
            var container = e.singleValueContainer()
            try container.encode(Self.rfc3339Fractional.string(from: date))
        }
        self.encoder = encoder
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

    /// Вход через Google: POST /auth/google, тело `{"idToken": "…"}`, без
    /// авторизационного заголовка (клиент на экране входа создаётся с
    /// token == nil).
    ///
    /// Токен — тот самый `idToken` из `GIDGoogleUser.idToken`, подписанный
    /// Google. Ничего больше слать не нужно: имя и почта лежат внутри токена,
    /// и сервер берёт их оттуда, а не из тела запроса — иначе клиент мог бы
    /// представиться кем угодно.
    ///
    /// 401 — токен не прошёл проверку (подпись, `aud`, срок); 503 — вход через
    /// Google на сервере не сконфигурирован (пустой `GOOGLE_CLIENT_IDS`).
    /// Вход через Telegram Login Widget: POST /auth/telegram.
    ///
    /// Поля идут ровно как их подписал Telegram — сервер пересобирает из них
    /// data-check-string и сверяет `hash`. Любая правка значения по дороге
    /// (обрезка, перекодировка) ломает подпись, поэтому передаём как есть.
    func loginWithTelegram(_ payload: TelegramWebAuth.Payload) async throws -> AuthResponse {
        struct Body: Encodable {
            let id: Int
            let firstName: String?
            let lastName: String?
            let username: String?
            let photoUrl: String?
            let authDate: Int64
            let hash: String
        }
        return try await request(
            "POST", "/api/v1/auth/telegram",
            body: Body(
                id: payload.id,
                firstName: payload.firstName,
                lastName: payload.lastName,
                username: payload.username,
                photoUrl: payload.photoUrl,
                authDate: payload.authDate,
                hash: payload.hash
            )
        )
    }

    func loginWithGoogle(idToken: String) async throws -> AuthResponse {
        struct Body: Encodable {
            let idToken: String
        }
        return try await request("POST", "/api/v1/auth/google", body: Body(idToken: idToken))
    }

    /// POST /api/v1/auth/register — регистрация по email и паролю.
    /// 409 `email_taken` — адрес занят; 400 `validation` — короткий пароль
    /// или невалидный email.
    func register(email: String, password: String, displayName: String) async throws -> AuthResponse {
        struct Body: Encodable {
            let email: String
            let password: String
            let displayName: String
        }
        return try await request(
            "POST", "/api/v1/auth/register",
            body: Body(email: email, password: password, displayName: displayName)
        )
    }

    /// POST /api/v1/auth/login — вход по email и паролю.
    /// 401 `invalid_credentials` одинаков для неверного пароля и незнакомого
    /// адреса: сервер намеренно не даёт проверить, зарегистрирован ли email.
    func loginWithPassword(email: String, password: String) async throws -> AuthResponse {
        struct Body: Encodable {
            let email: String
            let password: String
        }
        return try await request("POST", "/api/v1/auth/login", body: Body(email: email, password: password))
    }

    /// POST /api/v1/me/password — задать или сменить пароль.
    /// `current` опускается, когда пароля ещё не было.
    /// 403 `invalid_password` — не сошёлся текущий пароль (не 401: сессия жива).
    func setPassword(current: String?, new: String) async throws -> LinkedProvidersResponse {
        struct Body: Encodable {
            let currentPassword: String?
            let newPassword: String
        }
        return try await request(
            "POST", "/api/v1/me/password",
            body: Body(currentPassword: current?.isEmpty == false ? current : nil, newPassword: new)
        )
    }

    /// Вход через Sign in with Apple: POST /auth/apple, без авторизационного
    /// заголовка (клиент на экране входа создаётся с token == nil).
    ///
    /// - `nonce` уходит СЫРЫМ: в подписанном Apple id-токене лежит его SHA256,
    ///   и сервер сверяет одно с другим (см. AppleNonce).
    /// - `authorizationCode` одноразовый и живёт минуты. Сервер меняет его на
    ///   refresh token, без которого невозможен отзыв доступа при удалении
    ///   аккаунта (Apple Guideline 5.1.1(v)) — «добрать позже» его нельзя,
    ///   поэтому уходит сразу вместе с токеном. nil/пустой код (Apple его не
    ///   вернул) не сериализуется и вход не отменяет — ровно та же форма
    ///   запроса, что у `linkApple`: одно и то же поле не должно уезжать
    ///   на сервер двумя разными способами.
    /// - `displayName` Apple отдаёт только при ПЕРВОМ входе; дальше приходит
    ///   пустая строка — это норма, сервер не затирает уже сохранённое имя.
    ///
    /// 401 — токен невалиден или nonce не совпал; 503 — вход через Apple
    /// на сервере не сконфигурирован.
    func loginWithApple(
        idToken: String,
        displayName: String,
        nonce: String,
        authorizationCode: String?
    ) async throws -> AuthResponse {
        struct Body: Encodable {
            let idToken: String
            let displayName: String
            let nonce: String
            let authorizationCode: String?
        }
        let code = (authorizationCode?.isEmpty ?? true) ? nil : authorizationCode
        return try await request(
            "POST", "/api/v1/auth/apple",
            body: Body(
                idToken: idToken,
                displayName: displayName,
                nonce: nonce,
                authorizationCode: code
            )
        )
    }

    // MARK: Профиль

    func me() async throws -> Me {
        try await request("GET", "/api/v1/me")
    }

    /// GET /api/v1/me/notifications — эффективные настройки уведомлений.
    func notifications() async throws -> NotifySettings {
        try await request("GET", "/api/v1/me/notifications")
    }

    /// PATCH /api/v1/me/notifications — частичное обновление, ответ — новые значения.
    func updateNotifications(_ settings: NotifySettings) async throws -> NotifySettings {
        try await request("PATCH", "/api/v1/me/notifications", body: settings)
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

    // MARK: Способы входа (привязка/отвязка)

    /// POST /api/v1/me/link/google — привязать Google к ТЕКУЩЕМУ аккаунту.
    /// Кто «текущий», решает JWT, а не тело запроса.
    ///
    /// Повторная привязка того же аккаунта — 200 (идемпотентно);
    /// 409 `identity_taken` — эта личность уже принадлежит другому профилю
    /// Splitty (слияние профилей не поддерживается);
    /// 400 `provider_rejected` — id-токен Google не прошёл проверку. Отказ
    /// ПРОВАЙДЕРА сервер отдаёт именно 400, чтобы 401 отсюда однозначно
    /// означал мёртвую сессию Splitty и приводил к выходу (см. `perform`).
    func linkGoogle(idToken: String) async throws -> LinkedProvidersResponse {
        struct Body: Encodable {
            let idToken: String
        }
        return try await request(
            "POST", "/api/v1/me/link/google",
            body: Body(idToken: idToken)
        )
    }

    /// POST /api/v1/me/link/apple — привязать Apple ID к текущему аккаунту.
    ///
    /// `nonce` уходит СЫРЫМ (в токене лежит его SHA256) — тот же протокол, что
    /// и при входе, см. `AppleNonce`.
    ///
    /// `authorizationCode` обязателен здесь ровно по той же причине, что и при
    /// входе: сервер меняет его на Apple refresh token, без которого при
    /// удалении аккаунта нечем звать `auth/revoke` (Apple Guideline 5.1.1(v)).
    /// Пользователь, заведённый через Telegram/Google и привязавший Apple
    /// позже, иначе получал бы `apple_sub` без refresh token — и Splitty
    /// навсегда остался бы в его «Настройки → Apple ID → Вход с Apple».
    /// Код одноразовый и живёт минуты, «добрать позже» его нельзя.
    ///
    /// nil/пустой код (Apple не вернул `authorizationCode`) не сериализуется:
    /// поле на сервере опциональное, привязка личности пройдёт и без него.
    ///
    /// 400 `provider_rejected` — токен Apple не прошёл проверку или не сошёлся
    /// nonce; 401 отсюда означает мёртвую сессию Splitty, а не отказ Apple.
    func linkApple(
        idToken: String,
        nonce: String,
        authorizationCode: String?
    ) async throws -> LinkedProvidersResponse {
        struct Body: Encodable {
            let idToken: String
            let nonce: String
            let authorizationCode: String?
        }
        let code = (authorizationCode?.isEmpty ?? true) ? nil : authorizationCode
        return try await request(
            "POST", "/api/v1/me/link/apple",
            body: Body(idToken: idToken, nonce: nonce, authorizationCode: code)
        )
    }

    /// DELETE /api/v1/me/link/{provider} — отвязать способ входа.
    ///
    /// 409 `last_identity` — это последний способ войти, отвязать нельзя.
    /// Ответ по Telegram несёт `warning`, который клиент обязан показать.
    func unlinkProvider(_ provider: LoginProvider) async throws -> LinkedProvidersResponse {
        try await request("DELETE", "/api/v1/me/link/\(provider.rawValue)")
    }

    // MARK: Удаление аккаунта

    /// DELETE /api/v1/me — удаление аккаунта (Apple Guideline 5.1.1(v)).
    /// Ответ 204 без тела. Профиль и способы входа удаляются безвозвратно,
    /// а расходы и долги в группах остаются: в снимках комнат имя заменяется
    /// на «Удалённый пользователь», суммы и доли не меняются.
    ///
    /// 403 — демонстрационный аккаунт ревьюеров, его удалять запрещено.
    func deleteAccount() async throws {
        try await send("DELETE", "/api/v1/me")
    }

    // MARK: Устройства (push-токены)

    /// POST /api/v1/me/devices — привязать FCM-токен этого устройства к аккаунту
    /// (нужно для native-пушей). Идемпотентно: повтор с тем же токеном обновляет
    /// платформу, дублей не плодит. Ответ 204 без тела.
    func registerDevice(token: String, platform: String = "ios") async throws {
        struct Body: Encodable {
            let token: String
            let platform: String
        }
        try await send("POST", "/api/v1/me/devices", body: Body(token: token, platform: platform))
    }

    /// DELETE /api/v1/me/devices (тело `{"token": …}`) — отвязать токен при выходе.
    /// Отсутствие токена на сервере ошибкой не считается (idempotent). Звать, пока
    /// JWT ещё валиден (до `logout`), иначе запрос словит 401.
    func unregisterDevice(token: String) async throws {
        struct Body: Encodable {
            let token: String
        }
        try await send("DELETE", "/api/v1/me/devices", body: Body(token: token))
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
        items: [OperationItem]? = nil,
        clientOpId: String? = nil
    ) async throws -> Operation {
        try await request(
            "POST", "/api/v1/rooms/\(roomId)/operations",
            body: OperationBody(
                description: description,
                sum: sum,
                donorId: donorId,
                split: split,
                items: items,
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
        split: ExpenseSplit,
        items: [OperationItem]? = nil
    ) async throws -> Operation {
        try await request(
            "PUT", "/api/v1/rooms/\(roomId)/operations/\(operationId)",
            body: OperationBody(
                description: description,
                sum: sum,
                donorId: donorId,
                split: split,
                items: items
            )
        )
    }

    func deleteOperation(roomId: String, operationId: String) async throws {
        try await send("DELETE", "/api/v1/rooms/\(roomId)/operations/\(operationId)")
    }

    // MARK: AI-распознавание расхода

    /// AI-распознавание расхода: POST /rooms/{id}/operations/parse (multipart).
    /// Первый upload в проекте — тело собирается вручную (URLSession, без сторонних
    /// зависимостей). Опциональные части: `audio` (audio/wav), `image` (image/jpeg),
    /// `text` (поле), `draft` (JSON-поле текущего черновика для голосовой правки).
    /// Сервер сам выбирает медиа по приоритету audio → image → text; черновик
    /// НЕ создаёт операцию, а заполняет форму. 400 — не передано ни одного ввода.
    func parseOperation(
        roomId: String,
        audio: Data? = nil,
        image: Data? = nil,
        text: String? = nil,
        draft: ParseDraft? = nil
    ) async throws -> ParseResponse {
        guard let baseURL,
              var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw APIError.invalidURL
        }
        let basePath = components.path.hasSuffix("/")
            ? String(components.path.dropLast())
            : components.path
        components.path = basePath + "/api/v1/rooms/\(roomId)/operations/parse"
        guard let url = components.url else {
            throw APIError.invalidURL
        }

        let boundary = "SplittyBoundary-\(UUID().uuidString)"
        var body = Data()
        func appendLine(_ string: String) {
            body.append(Data(string.utf8))
        }
        // Поле формы (draft/text).
        func appendField(name: String, value: String) {
            appendLine("--\(boundary)\r\n")
            appendLine("Content-Disposition: form-data; name=\"\(name)\"\r\n\r\n")
            appendLine(value)
            appendLine("\r\n")
        }
        // Файловая часть (audio/image) — сервер проверяет Content-Type по allowlist.
        func appendFile(name: String, filename: String, contentType: String, data: Data) {
            appendLine("--\(boundary)\r\n")
            appendLine("Content-Disposition: form-data; name=\"\(name)\"; filename=\"\(filename)\"\r\n")
            appendLine("Content-Type: \(contentType)\r\n\r\n")
            body.append(data)
            appendLine("\r\n")
        }

        if let draft {
            let draftData = try encoder.encode(draft)
            appendField(name: "draft", value: String(decoding: draftData, as: UTF8.self))
        }
        if let audio {
            appendFile(name: "audio", filename: "audio.wav", contentType: "audio/wav", data: audio)
        }
        if let image {
            appendFile(name: "image", filename: "image.jpg", contentType: "image/jpeg", data: image)
        }
        if let text, !text.isEmpty {
            appendField(name: "text", value: text)
        }
        appendLine("--\(boundary)--\r\n")

        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        request.setValue(
            "multipart/form-data; boundary=\(boundary)",
            forHTTPHeaderField: "Content-Type"
        )
        request.httpBody = body

        let data = try await perform(request)
        do {
            return try decoder.decode(ParseResponse.self, from: data)
        } catch {
            throw APIError.decoding(error)
        }
    }

    /// Дозапись прозвища участнику: POST /users/{id}/aliases, тело `{"alias": …}`,
    /// ответ 204 без тела. После сопоставления нераспознанного имени участнику —
    /// чтобы следующее AI-распознавание сматчило его само. Сервер нормализует
    /// (trim/lower) и разрешает запись только при общей комнате (403 иначе).
    func addAlias(userId: Int, alias: String) async throws {
        struct Body: Encodable {
            let alias: String
        }
        try await send("POST", "/api/v1/users/\(userId)/aliases", body: Body(alias: alias))
    }

    // MARK: Долги и погашение

    func debts(roomId: String, involving: String) async throws -> [Debt] {
        try await request(
            "GET", "/api/v1/rooms/\(roomId)/debts",
            query: [URLQueryItem(name: "involving", value: involving)]
        )
    }

    /// `clientOpId` — идемпотентный ключ: на повтор с тем же ключом сервер
    /// возвращает уже созданное погашение, а не создаёт второе. Без него
    /// потерянный ответ на ЧАСТИЧНОЕ погашение приводил к двойному списанию:
    /// проверка переплаты ловит только возврат сверх долга.
    func repay(
        roomId: String,
        debtorId: Int,
        lenderId: Int,
        sum: Int,
        clientOpId: String
    ) async throws -> Operation {
        struct Body: Encodable {
            let debtorId: Int
            let lenderId: Int
            let sum: Int
            let clientOpId: String
        }
        return try await request(
            "POST", "/api/v1/rooms/\(roomId)/repayments",
            body: Body(debtorId: debtorId, lenderId: lenderId, sum: sum, clientOpId: clientOpId)
        )
    }

    // MARK: Друзья, активность, статистика

    func friends() async throws -> [FriendBalance] {
        try await request("GET", "/api/v1/friends")
    }

    /// Раздел «Уведомления»: приглашения + лента + счётчик.
    ///
    /// Имя намеренно НЕ `notifications()` — так уже называются настройки
    /// уведомлений (см. ниже), и совпадение имён было бы ловушкой.
    func notificationFeed(limit: Int, offset: Int) async throws -> NotificationsFeed {
        try await request(
            "GET", "/api/v1/notifications",
            query: [
                URLQueryItem(name: "limit", value: String(limit)),
                URLQueryItem(name: "offset", value: String(offset)),
            ]
        )
    }

    /// Отметить прочитанным всё, что было в ответе с этим `seenThrough`.
    func markNotificationsSeen(through: Date) async throws {
        struct Body: Encodable { let seenThrough: Date }
        try await send("POST", "/api/v1/me/notifications-seen", body: Body(seenThrough: through))
    }

    /// Отметить прочитанной ОДНУ группу — гасит счётчик на её карточке.
    /// Отдельно от `markNotificationsSeen`: раздел «Уведомления» счётчики групп
    /// не гасит, иначе их почти никто не успевал бы увидеть.
    func markRoomSeen(roomId: String, through: Date) async throws {
        struct Body: Encodable { let seenThrough: Date }
        try await send(
            "POST", "/api/v1/rooms/\(roomId)/notifications-seen", body: Body(seenThrough: through))
    }

    /// Позвать человека в группу. Возвращает статус: `added` — уже участник,
    /// `pending` — приглашение ждёт его решения.
    func addMember(roomId: String, userId: Int) async throws -> InviteStatus {
        struct Body: Encodable { let userId: Int }
        struct Response: Decodable { let status: InviteStatus }
        let out: Response = try await request(
            "POST", "/api/v1/rooms/\(roomId)/members", body: Body(userId: userId))
        return out.status
    }

    /// Выйти из группы самому.
    func leaveRoom(roomId: String) async throws {
        try await send("DELETE", "/api/v1/rooms/\(roomId)/members/me")
    }

    /// Убрать участника из группы.
    func removeMember(roomId: String, userId: Int) async throws {
        try await send("DELETE", "/api/v1/rooms/\(roomId)/members/\(userId)")
    }

    /// Принять приглашение вернуться в группу.
    func acceptInvite(roomId: String) async throws {
        try await send("POST", "/api/v1/invites/\(roomId)/accept")
    }

    /// Отклонить приглашение.
    func declineInvite(roomId: String) async throws {
        try await send("POST", "/api/v1/invites/\(roomId)/decline")
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
    /// GET /api/v1/users/{id}/avatar — фото профиля Telegram (байты);
    /// 404 — фото нет или скрыто приватностью.
    func userAvatar(id: Int) async throws -> Data {
        try await send("GET", "/api/v1/users/\(id)/avatar")
    }

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

        return try await perform(request)
    }

    /// Выполняет готовый запрос (в т.ч. multipart), проверяет статус, возвращает
    /// сырое тело. Общая обработка ответа для JSON-`send` и `parseOperation`.
    private func perform(_ request: URLRequest) async throws -> Data {
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
