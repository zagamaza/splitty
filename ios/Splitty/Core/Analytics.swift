import Foundation
import SwiftUI
import UIKit

/// Продуктовое событие. Перечисление, а не строки на месте вызова: имя события —
/// проводной контракт с сервером и вторым клиентом, и опечатка в нём ничего не
/// уронит, а молча уведёт шаг воронки в никуда.
///
/// Набор и допустимые значения — `docs/analytics-events.md`. Он источник правды;
/// новое значение сначала попадает туда, потом сюда.
enum AnalyticsEvent {
    case appOpen(cold: Bool)
    case loginCompleted(method: String)
    case onboardingStarted
    case onboardingStep(step: String)
    case onboardingCompleted
    case onboardingSkipped
    case roomCreated
    case roomJoined(via: String)
    case roomJoinFailed(reason: String)
    case expenseAdded(method: String, edited: Bool)
    case expenseParseFailed(kind: String, reason: String)
    case settleUpOpened
    case settleUpDone
    case paywallShown(from: String)
    case paywallDismissed(from: String)
    case purchaseStarted(product: String)
    case purchaseCompleted(product: String)
    case purchaseFailed(reason: String)
    case inviteSent(channel: String)
    case screenView(screen: String)
    case settingsChanged(what: String)
    case accountLinked(provider: String)
    case accountUnlinked(provider: String)
    case accountDeleted
    case logout
    case memberAdded(via: String)
    case memberAddFailed(reason: String)
    case memberRemoved
    case roomLeft
    case roomArchived
    case roomUnarchived
    case roomSettingsChanged(what: String)
    case captureStarted(kind: String)
    case captureCancelled(kind: String)
    case parseStarted(kind: String)
    case parseSucceeded(kind: String, items: String)
    case parseRetried(kind: String)
    case receiptItemEdited
    case receiptUnknownResolved

    var name: String {
        switch self {
        case .appOpen: return "app_open"
        case .loginCompleted: return "login_completed"
        case .onboardingStarted: return "onboarding_started"
        case .onboardingStep: return "onboarding_step"
        case .onboardingCompleted: return "onboarding_completed"
        case .onboardingSkipped: return "onboarding_skipped"
        case .roomCreated: return "room_created"
        case .roomJoined: return "room_joined"
        case .roomJoinFailed: return "room_join_failed"
        case .expenseAdded: return "expense_added"
        case .expenseParseFailed: return "expense_parse_failed"
        case .settleUpOpened: return "settle_up_opened"
        case .settleUpDone: return "settle_up_done"
        case .paywallShown: return "paywall_shown"
        case .paywallDismissed: return "paywall_dismissed"
        case .purchaseStarted: return "purchase_started"
        case .purchaseCompleted: return "purchase_completed"
        case .purchaseFailed: return "purchase_failed"
        case .inviteSent: return "invite_sent"
        case .screenView: return "screen_view"
        case .settingsChanged: return "settings_changed"
        case .accountLinked: return "account_linked"
        case .accountUnlinked: return "account_unlinked"
        case .accountDeleted: return "account_deleted"
        case .logout: return "logout"
        case .memberAdded: return "member_added"
        case .memberAddFailed: return "member_add_failed"
        case .memberRemoved: return "member_removed"
        case .roomLeft: return "room_left"
        case .roomArchived: return "room_archived"
        case .roomUnarchived: return "room_unarchived"
        case .roomSettingsChanged: return "room_settings_changed"
        case .captureStarted: return "capture_started"
        case .captureCancelled: return "capture_cancelled"
        case .parseStarted: return "parse_started"
        case .parseSucceeded: return "parse_succeeded"
        case .parseRetried: return "parse_retried"
        case .receiptItemEdited: return "receipt_item_edited"
        case .receiptUnknownResolved: return "receipt_unknown_resolved"
        }
    }

    var params: [String: String] {
        switch self {
        case let .appOpen(cold): return ["cold": cold ? "true" : "false"]
        case let .loginCompleted(method): return ["method": method]
        case let .onboardingStep(step): return ["step": step]
        case let .roomJoined(via): return ["via": via]
        case let .roomJoinFailed(reason): return ["reason": reason]
        case let .expenseAdded(method, edited): return ["method": method, "edited": edited ? "true" : "false"]
        case let .expenseParseFailed(kind, reason): return ["kind": kind, "reason": reason]
        case let .paywallShown(from): return ["from": from]
        case let .paywallDismissed(from): return ["from": from]
        case let .purchaseStarted(product): return ["product": product]
        case let .purchaseCompleted(product): return ["product": product]
        case let .purchaseFailed(reason): return ["reason": reason]
        case .onboardingStarted, .onboardingCompleted, .onboardingSkipped,
             .roomCreated, .settleUpOpened, .settleUpDone:
            return [:]
        case let .inviteSent(channel): return ["channel": channel]
        case let .screenView(screen): return ["screen": screen]
        case let .settingsChanged(what): return ["what": what]
        case let .accountLinked(provider): return ["provider": provider]
        case let .accountUnlinked(provider): return ["provider": provider]
        case let .memberAdded(via): return ["via": via]
        case let .memberAddFailed(reason): return ["reason": reason]
        case let .roomSettingsChanged(what): return ["what": what]
        case let .captureStarted(kind): return ["kind": kind]
        case let .captureCancelled(kind): return ["kind": kind]
        case let .parseStarted(kind): return ["kind": kind]
        case let .parseSucceeded(kind, items): return ["kind": kind, "items": items]
        case let .parseRetried(kind): return ["kind": kind]
        case .accountDeleted, .logout, .memberRemoved, .roomLeft, .roomArchived,
             .roomUnarchived, .receiptItemEdited, .receiptUnknownResolved:
            return [:]
        }
    }
}

/// Одна запись очереди — ровно то, что уедет на сервер.
struct AnalyticsRecord: Codable, Equatable {
    let id: String
    let name: String
    let at: Date
    let session: String
    let platform: String
    let appVersion: String
    let locale: String
    let params: [String: String]
    /// Кому принадлежит запись. Событие чужого человека под чужим номером —
    /// это и испорченная аналитика, и приватность.
    let ownerUserId: Int
}

/// Очередь событий на диске.
///
/// Отдельная сущность, а не тот же `OutboxStore`: его payload завязан на
/// операции. Приём повторяется, модель — нет.
///
/// Ключевое отличие от очереди расходов: события НЕ наследуются при смене
/// владельца, а выбрасываются. Расход терять нельзя — человек его ввёл; событие
/// содержимого не несёт, и приклеить его чужому человеку хуже, чем потерять.
final class AnalyticsQueue {
    /// Потолок очереди. Переполнение выбрасывает самые старые: расти без предела
    /// файл не должен, а свежие события полезнее давних.
    static let capacity = 500

    private(set) var records: [AnalyticsRecord] = []

    private let fileURL: URL
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder
    private let io = DispatchQueue(label: "splitty.analytics.io", qos: .utility)
    /// Файл прочитан — писать безопасно. Как в OutboxStore: «файла нет» и «не
    /// смог прочитать» это разные вещи, и во втором случае перезапись затёрла
    /// бы накопленное.
    private var didLoad = false

    static var defaultFileURL: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("analytics.json")
    }

    private struct QueueFile: Codable {
        var schemaVersion: Int
        var records: [AnalyticsRecord]
    }

    static let schemaVersion = 1

    init(fileURL: URL = AnalyticsQueue.defaultFileURL) {
        self.fileURL = fileURL
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder

        if FileManager.default.fileExists(atPath: fileURL.path) {
            if let data = try? Data(contentsOf: fileURL) {
                didLoad = true
                if let file = try? decoder.decode(QueueFile.self, from: data) {
                    records = file.records
                }
            }
        } else {
            didLoad = true
        }
    }

    func append(_ record: AnalyticsRecord) {
        records.append(record)
        if records.count > Self.capacity {
            records.removeFirst(records.count - Self.capacity)
        }
        persist()
    }

    /// Забирает до `limit` записей владельца для отправки.
    func take(_ limit: Int, owner: Int) -> [AnalyticsRecord] {
        Array(records.filter { $0.ownerUserId == owner }.prefix(limit))
    }

    func remove(ids: Set<String>) {
        guard !ids.isEmpty else { return }
        records.removeAll { ids.contains($0.id) }
        persist()
    }

    /// Оставляет только записи этого владельца. Смена аккаунта на устройстве —
    /// не повод отправить события прошлого человека под новым номером.
    func keepOwned(by userId: Int?) {
        guard let userId else {
            records.removeAll()
            persist()
            return
        }
        let before = records.count
        records.removeAll { $0.ownerUserId != userId }
        if records.count != before {
            persist()
        }
    }

    private func persist() {
        guard didLoad else { return }
        let file = QueueFile(schemaVersion: Self.schemaVersion, records: records)
        let url = fileURL
        let encoder = self.encoder
        io.async {
            guard let data = try? encoder.encode(file) else { return }
            try? data.write(to: url, options: .atomic)
        }
    }

    /// Ждёт незавершённые записи файла. Нужен тестам.
    func waitForPendingWrites() {
        io.sync {}
    }
}

/// Сбор продуктовых событий.
@MainActor
final class Analytics {
    static let shared = Analytics()

    /// Выключатель — константа сборки, а не настройка.
    ///
    /// Настройка потянула бы строки в пять локалей и перезапись эталонов
    /// снимков на Android ради переключателя, который нужен нам, а не человеку.
    static let isEnabled = true

    /// Сколько событий накапливаем до отправки.
    static let batchSize = 20
    /// Через сколько отправляем, даже если пачка не набралась.
    static let flushInterval: TimeInterval = 30
    /// Сколько минут в фоне начинают новую сессию.
    static let sessionIdleLimit: TimeInterval = 30 * 60

    private let queue: AnalyticsQueue
    private var api: APIClient?
    private var ownerUserId: Int?

    private var sessionId = UUID().uuidString
    private var lastActivity = Date()
    private var isFlushing = false
    private var flushTask: Task<Void, Never>?
    private var timerTask: Task<Void, Never>?

    init(queue: AnalyticsQueue = AnalyticsQueue()) {
        self.queue = queue
    }

    /// Подключает клиента API и владельца очереди.
    ///
    /// Нет сессии — не пишем вовсе: приём на сервере закрыт авторизацией, а
    /// копить события «до входа» значило бы решать, кому они достанутся, когда
    /// человек войдёт. Приветствие на обоих клиентах пост-логинное, так что
    /// теряется практически только `app_open` холодного старта.
    func configure(api: APIClient?, userId: Int?) {
        if ownerUserId != userId {
            // Аккаунт на устройстве сменился: события прошлого человека
            // выбрасываем, а не переклеиваем на нового.
            queue.keepOwned(by: userId)
            sessionId = UUID().uuidString
        }
        self.api = api
        self.ownerUserId = userId
        if api != nil && userId != nil {
            startTimer()
        } else {
            timerTask?.cancel()
            timerTask = nil
        }
    }

    func track(_ event: AnalyticsEvent) {
        guard Self.isEnabled, let owner = ownerUserId else { return }

        let now = Date()
        if now.timeIntervalSince(lastActivity) > Self.sessionIdleLimit {
            sessionId = UUID().uuidString
        }
        lastActivity = now

        queue.append(AnalyticsRecord(
            id: UUID().uuidString,
            name: event.name,
            at: now,
            session: sessionId,
            platform: "ios",
            // Версия НА МОМЕНТ СОБЫТИЯ: запись может пролежать в очереди и
            // уехать уже после обновления приложения. Заголовок X-Client-Version
            // отвечает на другой вопрос — какая версия отправила пачку.
            appVersion: APIClient.clientVersion,
            locale: Locale.current.identifier,
            params: event.params,
            ownerUserId: owner
        ))

        if queue.take(Self.batchSize, owner: owner).count >= Self.batchSize {
            scheduleFlush()
        }
    }

    /// Новая сессия на холодном старте.
    func startSession() {
        sessionId = UUID().uuidString
        lastActivity = Date()
    }

    /// Отправка на уходе в фон.
    ///
    /// Через beginBackgroundTask: запрос, стартовавший на переходе, система
    /// приостанавливает, и пачка просто не доехала бы — молча, как и всё в
    /// аналитике.
    func flushInBackground() {
        guard Self.isEnabled else { return }
        var taskId = UIApplication.shared.beginBackgroundTask(withName: "splitty.analytics.flush")
        Task { [weak self] in
            await self?.flush()
            if taskId != .invalid {
                UIApplication.shared.endBackgroundTask(taskId)
                taskId = .invalid
            }
        }
    }

    /// Периодическая отправка: пачка может не набраться неделями, а событие,
    /// которое лежит на телефоне, для аналитики всё равно что не случилось.
    private func startTimer() {
        timerTask?.cancel()
        timerTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(Self.flushInterval * 1_000_000_000))
                await self?.flush()
            }
        }
    }

    func scheduleFlush() {
        flushTask?.cancel()
        flushTask = Task { [weak self] in
            await self?.flush()
        }
    }

    /// Отправляет накопленное. Ошибка — не повод чистить очередь: события
    /// подождут следующей попытки.
    func flush() async {
        guard Self.isEnabled, !isFlushing, let owner = ownerUserId, let api else { return }
        let batch = queue.take(Self.batchSize, owner: owner)
        guard !batch.isEmpty else { return }

        isFlushing = true
        defer { isFlushing = false }

        do {
            try await api.postEvents(EventsBody(events: batch.map(EventBody.init)))
            queue.remove(ids: Set(batch.map(\.id)))
        } catch let APIError.server(status, _, _, _) {
            // Постоянный отказ не крутится вечно: сервер сказал, что эта пачка
            // ему не годится, и повтор ничего не изменит.
            if status == 400 || status == 401 || status == 403 || status == 413 {
                queue.remove(ids: Set(batch.map(\.id)))
            }
        } catch {
            // Сеть. Оставляем как есть.
        }
    }

    private struct EventsBody: Encodable {
        let events: [EventBody]
    }

    private struct EventBody: Encodable {
        let id: String
        let name: String
        let at: Date
        let session: String
        let platform: String
        let appVersion: String
        let locale: String
        let params: [String: String]

        init(_ record: AnalyticsRecord) {
            id = record.id
            name = record.name
            at = record.at
            session = record.session
            platform = record.platform
            appVersion = record.appVersion
            locale = record.locale
            params = record.params
        }
    }
}

/// Причина неудачного входа в тусу — из закрытого множества контракта.
///
/// Свободный текст ошибки сюда попасть не должен: он не группируется в
/// агрегатах и утаскивает наружу подробности, которых в аналитике быть не может.
func analyticsJoinReason(_ error: Error) -> String {
    guard case let APIError.server(status, code, _, _) = error else { return "network" }
    switch code {
    case "not_found": return "not_found"
    case "room_deleted", "deleted": return "deleted"
    case "forbidden": return "forbidden"
    default:
        return status == 404 ? "not_found" : (status == 403 ? "forbidden" : "network")
    }
}

/// Причина, по которой не удалось добавить человека в тусу.
func analyticsMemberAddReason(_ error: Error) -> String {
    guard case let APIError.server(status, code, _, _) = error else { return "network" }
    switch code {
    case "not_found": return "not_found"
    case "already_member": return "already_member"
    case "forbidden": return "forbidden"
    default:
        return status == 404 ? "not_found" : (status == 403 ? "forbidden" : "network")
    }
}

/// Причина неудачного распознавания — тоже из закрытого множества.
func analyticsParseReason(_ error: Error) -> String {
    guard case let APIError.server(status, code, _, _) = error else { return "network" }
    switch code {
    case "quota", "rate_limited", "unsupported_media", "too_large", "validation", "internal":
        return code
    default:
        return status >= 500 ? "internal" : "validation"
    }
}

/// Продукт подписки в термине контракта. Идентификаторы магазина
/// (com.zagir.splitty.plus.monthly) в аналитику не уезжают: они длинные,
/// платформенные и разъедутся между App Store и Google Play.
func analyticsProduct(_ productId: String) -> String {
    productId.contains("year") ? "yearly" : "monthly"
}

/// Сколько позиций распозналось — бакетом, как на сервере.
///
/// Диапазоны обязаны совпадать с `analytics.ItemsBucket` в Go и с Android:
/// поделив их по-своему, один и тот же чек попал бы в разные корзины, и
/// сравнивать платформы стало бы нельзя.
func analyticsItemsBucket(_ count: Int) -> String {
    switch count {
    case ..<1: return "none"
    case ..<4: return "few"
    case ..<11: return "many"
    default: return "lots"
    }
}

extension View {
    /// Отметить открытие экрана.
    ///
    /// Отдельным модификатором, а не `.onAppear` на месте: `onAppear` легко
    /// повесить не на тот уровень, и тогда событие полетит на каждый рендер
    /// строки списка. Здесь один вызов и одно место, где это можно проверить.
    func trackScreen(_ screen: String) -> some View {
        onAppear { Analytics.shared.track(.screenView(screen: screen)) }
    }
}
