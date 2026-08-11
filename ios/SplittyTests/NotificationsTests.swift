import XCTest
@testable import Splitty

/// Раздел «Уведомления»: контракт запросов, разбор ответа, счётчик и действия
/// на карточках приглашений.
///
/// Тесты появились после того, как отметка прочитанного оказалась мёртвой:
/// дата уходила ЧИСЛОМ (дефолтная стратегия `JSONEncoder`), сервер разбирает
/// `time.Time` только из строки RFC3339 и отвечал 400 на каждый вызов — бейдж
/// не гас никогда, и заметить это было нечем.
private let feedJSON = #"""
{
  "invites": [
    {"roomId":"room-pending","roomName":"Стамбул","inviterName":"Аня",
     "status":"pending","createdAt":"2026-07-30T10:00:00Z"},
    {"roomId":"room-added","roomName":"Дача","inviterName":"Пётр",
     "status":"added","createdAt":"2026-07-30T11:00:00Z"}
  ],
  "items": [
    {"roomId":"room-added","roomName":"Дача","roomCurrency":"RUB",
     "operation":{"id":"op-1","description":"Ужин","sum":1200,"isDebtRepayment":false,
      "donor":{"id":77,"username":null,"displayName":"Аня"},
      "recipients":[
        {"user":{"id":77,"username":null,"displayName":"Аня"},"sum":600},
        {"user":{"id":88,"username":null,"displayName":"Удалённый пользователь","deleted":true},"sum":600}
      ],
      "splitType":"equally","createdAt":"2026-07-30T11:30:00Z","files":[]}}
  ],
  "unreadCount": 3,
  "seenThrough": "2026-07-30T12:00:00Z"
}
"""#

/// «2026-07-30T12:00:00Z» — тот же момент, что в `seenThrough` ответа.
private let seenThroughDate = Date(timeIntervalSince1970: 1_785_412_800)

/// Разбирает и с долями секунды, и без них — клиент вправе прислать любой из них.
private func parseRFC3339(_ string: String) -> Date? {
    let plain = ISO8601DateFormatter()
    plain.formatOptions = [.withInternetDateTime]
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return plain.date(from: string) ?? fractional.date(from: string)
}

final class NotificationsAPITests: XCTestCase {
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
            token: "jwt",
            urlSession: URLSession(configuration: configuration)
        )
    }

    override func tearDown() {
        client = nil
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        super.tearDown()
    }

    // MARK: GET /notifications

    func testNotificationFeedDecodesInvitesItemsAndCounter() async throws {
        StubURLProtocol.handler = { _ in (200, Data(feedJSON.utf8)) }

        let feed = try await client.notificationFeed(limit: 30, offset: 0)

        XCTAssertEqual(feed.unreadCount, 3)
        XCTAssertEqual(feed.seenThrough, seenThroughDate)
        XCTAssertEqual(feed.invites.count, 2)
        XCTAssertEqual(feed.invites.first?.status, .pending)
        XCTAssertEqual(feed.invites.first?.roomName, "Стамбул")
        XCTAssertEqual(feed.invites.last?.status, .added)
        XCTAssertEqual(feed.items.count, 1)
        XCTAssertEqual(feed.items.first?.operation.description, "Ужин")
        // Снимок удалённого участника не должен ронять разбор всей ленты.
        XCTAssertEqual(feed.items.first?.operation.recipients.last?.user.deleted, true)
        XCTAssertEqual(feed.items.first?.operation.donor.deleted, false)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.path, "/api/v1/notifications")
        let query = URLComponents(url: try XCTUnwrap(request.url), resolvingAgainstBaseURL: false)?.queryItems
        XCTAssertEqual(query?.first { $0.name == "limit" }?.value, "30")
        XCTAssertEqual(query?.first { $0.name == "offset" }?.value, "0")
    }

    /// `seenThrough` — единственная дата API, которая рождается не в mongo, а из
    /// `time.Now()`, и уезжает в RFC3339Nano с девятью знаками после точки.
    /// Отвергни её декодер — раздел и бейдж умерли бы целиком, молча.
    func testNotificationFeedDecodesNanosecondPrecisionDate() async throws {
        let nanoJSON = feedJSON.replacingOccurrences(
            of: "\"seenThrough\": \"2026-07-30T12:00:00Z\"",
            with: "\"seenThrough\": \"2026-07-30T12:00:00.123456789Z\""
        )
        XCTAssertTrue(nanoJSON.contains("123456789"), "подмена даты в фикстуре не сработала")
        StubURLProtocol.handler = { _ in (200, Data(nanoJSON.utf8)) }

        let feed = try await client.notificationFeed(limit: 30, offset: 0)

        XCTAssertEqual(feed.seenThrough.timeIntervalSince1970,
                       seenThroughDate.timeIntervalSince1970 + 0.123, accuracy: 0.001)
    }

    // MARK: POST /me/notifications-seen

    /// Дата уходит СТРОКОЙ RFC3339: числу (`.deferredToDate`) сервер отвечает
    /// 400, и отметка прочитанного не срабатывает ни разу.
    func testMarkNotificationsSeenSendsRFC3339String() async throws {
        StubURLProtocol.handler = { _ in (204, Data()) }

        try await client.markNotificationsSeen(through: seenThroughDate)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/me/notifications-seen")

        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: try XCTUnwrap(StubURLProtocol.lastBody)) as? [String: Any]
        )
        XCTAssertNil(json["seenThrough"] as? NSNumber)
        let raw = try XCTUnwrap(json["seenThrough"] as? String)
        XCTAssertEqual(parseRFC3339(raw), seenThroughDate)
    }
}

/// Экран раздела: счётчик, отметка прочитанного и действия на карточках.
final class ActivityViewModelTests: XCTestCase {
    private var directory: URL!
    private var stubSession: URLSession!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        StubURLProtocol.responseDelay = nil

        directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("notifications-tests-\(UUID().uuidString)", isDirectory: true)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        stubSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: directory)
        StubURLProtocol.handler = nil
        StubURLProtocol.lastRequest = nil
        StubURLProtocol.lastBody = nil
        super.tearDown()
    }

    @MainActor
    private func makeRepo() -> DataRepo {
        DataRepo(
            api: APIClient(
                baseURL: URL(string: "https://api.example.test"),
                token: "jwt",
                urlSession: stubSession
            ),
            cache: OfflineStore(directory: directory),
            scope: "test"
        )
    }

    /// Лента отдаётся на GET, всё остальное — 204, если тест не сказал иначе.
    private func stubFeed(action: ((URLRequest) -> (Int, Data))? = nil) {
        StubURLProtocol.handler = { request in
            if request.url?.path == "/api/v1/notifications" {
                return (200, Data(feedJSON.utf8))
            }
            return action?(request) ?? (204, Data())
        }
    }

    @MainActor
    private func loadedModel() async -> ActivityViewModel {
        let model = ActivityViewModel()
        await model.load(repo: makeRepo())
        return model
    }

    // MARK: отметка прочитанного

    @MainActor
    func testMarkSeenSendsSeenThroughFromResponseAndClearsBadge() async throws {
        stubFeed()
        let model = await loadedModel()
        XCTAssertEqual(model.unreadCount, 3)

        let session = SessionStore(urlSession: stubSession)
        session.unreadNotifications = 3
        await model.markSeen(session: session)

        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/api/v1/me/notifications-seen")
        let json = try XCTUnwrap(
            JSONSerialization.jsonObject(with: try XCTUnwrap(StubURLProtocol.lastBody)) as? [String: Any]
        )
        // Именно серверное время ответа: «сейчас» погасило бы и то, что пришло
        // после ответа, так и не показавшись человеку.
        XCTAssertEqual(parseRFC3339(try XCTUnwrap(json["seenThrough"] as? String)), seenThroughDate)
        // Не ноль: ждущее решения приглашение остаётся непрочитанным, пока на
        // него не ответили — открытие раздела его не «прочитывает».
        XCTAssertEqual(session.unreadNotifications, 1)
        XCTAssertEqual(model.unreadCount, 1)
    }

    /// Счётчик из ответа обязан доехать до бейджа даже когда отмечать нечего:
    /// отметку мог поставить другой девайс, и без этой записи бейдж висел бы
    /// со старым числом до следующего возврата из фона.
    @MainActor
    func testMarkSeenPublishesCountWhenNothingToMark() async {
        let seenJSON = feedJSON
            .replacingOccurrences(of: "\"unreadCount\": 3", with: "\"unreadCount\": 0")
        StubURLProtocol.handler = { request in
            if request.url?.path == "/api/v1/notifications" {
                return (200, Data(seenJSON.utf8))
            }
            return (204, Data())
        }
        let model = await loadedModel()
        XCTAssertEqual(model.unreadCount, 0)

        let session = SessionStore(urlSession: stubSession)
        session.unreadNotifications = 5
        await model.markSeen(session: session)

        XCTAssertEqual(session.unreadNotifications, 0)
    }

    /// Сбой отметки не должен ни гасить бейдж, ни показывать алерт: человек
    /// этого действия не просил.
    @MainActor
    func testMarkSeenFailureKeepsBadgeAndStaysSilent() async {
        stubFeed { _ in (400, Data(#"{"error":{"code":"validation","message":"seenThrough из будущего"}}"#.utf8)) }
        let model = await loadedModel()

        let session = SessionStore(urlSession: stubSession)
        session.unreadNotifications = 3
        await model.markSeen(session: session)

        XCTAssertEqual(session.unreadNotifications, 3)
        XCTAssertEqual(model.unreadCount, 3)
        XCTAssertNil(model.errorMessage)
    }

    // MARK: действия на карточках

    @MainActor
    func testAcceptInviteRemovesCardAndNotesDataChange() async throws {
        stubFeed()
        let model = await loadedModel()
        let card = try XCTUnwrap(model.invites.first { $0.status == .pending })

        let session = SessionStore(urlSession: stubSession)
        let versionBefore = session.dataVersion
        await model.acceptInvite(card, session: session)

        XCTAssertEqual(StubURLProtocol.lastRequest?.httpMethod, "POST")
        XCTAssertEqual(StubURLProtocol.lastRequest?.url?.path, "/api/v1/invites/room-pending/accept")
        XCTAssertFalse(model.invites.contains { $0.roomId == "room-pending" })
        XCTAssertEqual(session.dataVersion, versionBefore + 1)
        XCTAssertNil(model.errorMessage)
    }

    @MainActor
    func testDeclineInviteRemovesCardAndNotesDataChange() async throws {
        stubFeed()
        let model = await loadedModel()
        let card = try XCTUnwrap(model.invites.first { $0.status == .pending })

        let session = SessionStore(urlSession: stubSession)
        let versionBefore = session.dataVersion
        await model.declineInvite(card, session: session)

        XCTAssertEqual(StubURLProtocol.lastRequest?.url?.path, "/api/v1/invites/room-pending/decline")
        XCTAssertFalse(model.invites.contains { $0.roomId == "room-pending" })
        XCTAssertEqual(session.dataVersion, versionBefore + 1)
    }

    @MainActor
    func testLeaveFromCardRemovesCardAndNotesDataChange() async throws {
        stubFeed()
        let model = await loadedModel()
        let card = try XCTUnwrap(model.invites.first { $0.status == .added })

        let session = SessionStore(urlSession: stubSession)
        let versionBefore = session.dataVersion
        await model.leaveFromCard(card, session: session)

        XCTAssertEqual(StubURLProtocol.lastRequest?.httpMethod, "DELETE")
        XCTAssertEqual(StubURLProtocol.lastRequest?.url?.path, "/api/v1/rooms/room-added/members/me")
        XCTAssertFalse(model.invites.contains { $0.roomId == "room-added" })
        XCTAssertEqual(session.dataVersion, versionBefore + 1)
    }

    /// Отказ выхода с карточки обязан объяснить путь наружу, а не отдать
    /// серверный текст и не считаться успехом: карточка остаётся на месте.
    @MainActor
    func testLeaveFromCardConflictKeepsCardAndExplainsWayOut() async throws {
        stubFeed { request in
            guard request.httpMethod == "DELETE" else { return (204, Data()) }
            return (409, Data(#"{"error":{"code":"has_operations","message":"конфликт"}}"#.utf8))
        }
        let model = await loadedModel()
        let card = try XCTUnwrap(model.invites.first { $0.status == .added })

        let session = SessionStore(urlSession: stubSession)
        await model.leaveFromCard(card, session: session)

        XCTAssertTrue(model.invites.contains { $0.roomId == "room-added" })
        let message = try XCTUnwrap(model.errorMessage)
        XCTAssertTrue(message.contains("Уберите себя"))
        XCTAssertFalse(message.contains("конфликт"))
    }
}

/// Текст бейджа: сервер отдаёт точное число до 99, а 100 означает «больше 99».
final class BadgeLabelTests: XCTestCase {
    func testBadgeHiddenWhenNothingUnread() {
        XCTAssertNil(MainTabView.badgeLabel(for: 0))
        XCTAssertNil(MainTabView.badgeLabel(for: -1))
    }

    func testExactCountShownUpToCeiling() {
        XCTAssertEqual(MainTabView.badgeLabel(for: 1), "1")
        XCTAssertEqual(MainTabView.badgeLabel(for: 99), "99")
    }

    /// Потолок рисуется «99+», а не числом: «100» выглядело бы точным
    /// количеством, которого сервер не считал.
    func testOverflowShownAsPlus() {
        XCTAssertEqual(MainTabView.badgeLabel(for: 100), "99+")
        XCTAssertEqual(MainTabView.badgeLabel(for: 500), "99+")
    }
}

/// Куда ведёт тап по push. Раньше событие постилось в пустоту: подписчиков
/// не было ни одного, и «переход по уведомлению» существовал только на бумаге.
final class PushTargetTests: XCTestCase {
    func testRoomPushOpensGroupsTabWithRoom() {
        let target = MainTabView.pushTarget(for: .room(id: "room-1"))
        XCTAssertEqual(target.tab, .groups)
        XCTAssertEqual(target.roomId, "room-1")
    }

    /// Приглашение ведёт в раздел, а не в комнату: доступа к ней у
    /// приглашённого ещё нет — переход упёрся бы в 403.
    func testInvitePushOpensNotificationsTabWithoutRoom() {
        let target = MainTabView.pushTarget(for: .notifications)
        XCTAssertEqual(target.tab, .activity)
        XCTAssertNil(target.roomId)
    }

    /// Сквозной путь: payload бэкенда → маршрут → вкладка.
    func testPayloadFromBackendLeadsToRoom() throws {
        let route = try XCTUnwrap(PushRoute(userInfo: [
            "channel": "operations",
            "roomId": "68f2a1c4d9",
            "type": "operation",
        ]))
        let target = MainTabView.pushTarget(for: route)
        XCTAssertEqual(target.tab, .groups)
        XCTAssertEqual(target.roomId, "68f2a1c4d9")
    }
}
