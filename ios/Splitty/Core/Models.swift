import Foundation

// Codable DTO REST API — точно по контракту docs/API.md.
// Деньги — целые рубли (Int); id комнат/операций — hex-строки ObjectID;
// id пользователей — НОМЕР ПОЛЬЗОВАТЕЛЯ SPLITTY (Int). Раньше он совпадал
// с telegram user id, но с появлением входа через Google/Apple это больше
// не так: telegram id живёт на сервере отдельным полем и наружу не отдаётся,
// а у пользователей без Telegram номер начинается от 10¹².

/// Пользователь.
struct User: Codable, Identifiable, Hashable {
    let id: Int
    let username: String?
    let displayName: String
}

/// Каналы уведомлений одной категории.
struct ChannelPrefs: Codable, Hashable {
    var telegram: Bool
    var push: Bool
}

/// Настройки уведомлений: категория событий × канал доставки
/// (GET/PATCH /me/notifications, сервер отдаёт эффективные значения).
struct NotifySettings: Codable, Hashable {
    var operations: ChannelPrefs
    var debts: ChannelPrefs
}

/// Профиль текущего пользователя.
struct Me: Codable, Identifiable, Hashable {
    let id: Int
    let username: String?
    let displayName: String
    let lang: String
    /// Привязанные способы входа («telegram», «google», «apple») — сервер
    /// отдаёт только ФАКТ привязки, сами идентификаторы личности наружу не
    /// уходят. По этому списку экран «Профиль» рисует секцию «Способы входа»
    /// и решает, какой способ отвязывать нельзя (последний).
    var linkedProviders: [String] = []
    let notificationOn: Bool
}

// init(from:) в extension, чтобы сохранить memberwise-инициализатор.
// linkedProviders декодируется мягко: ключ появился в API позже, и в офлайн-
// кеше (OfflineStore) лежат профили, записанные БЕЗ него — строгий decode
// уронил бы весь кешированный профиль на первом же холодном старте.
extension Me {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int.self, forKey: .id)
        username = try c.decodeIfPresent(String.self, forKey: .username)
        displayName = try c.decode(String.self, forKey: .displayName)
        lang = try c.decode(String.self, forKey: .lang)
        linkedProviders = try c.decodeIfPresent([String].self, forKey: .linkedProviders) ?? []
        notificationOn = try c.decode(Bool.self, forKey: .notificationOn)
    }
}

/// Способ входа в аккаунт. `rawValue` — то же имя, что в `linkedProviders`
/// и в пути `/api/v1/me/link/{provider}`: одна строка на клиент и сервер,
/// дублировать её литералами по экранам нельзя.
enum LoginProvider: String, CaseIterable, Identifiable, Hashable {
    case telegram
    case google
    case apple

    var id: String { rawValue }

    /// Название для экрана «Способы входа».
    var title: String {
        switch self {
        case .telegram: return "Telegram"
        case .google: return "Google"
        case .apple: return "Apple"
        }
    }

    /// SF Symbol строки способа входа.
    var symbol: String {
        switch self {
        case .telegram: return "paperplane"
        case .google: return "g.circle"
        case .apple: return "apple.logo"
        }
    }
}

extension Me {
    /// Привязан ли способ входа к аккаунту.
    func isLinked(_ provider: LoginProvider) -> Bool {
        linkedProviders.contains(provider.rawValue)
    }

    /// Можно ли отвязать способ входа.
    ///
    /// Последний способ отвязывать нельзя: JWT живёт 90 дней, и аккаунт,
    /// оставшийся без единого входа, станет недоступен навсегда. Сервер
    /// отвечает на это `409 last_identity`, но кнопка обязана гаснуть ДО
    /// запроса — иначе человек узнаёт о запрете из алерта после действия.
    func canUnlink(_ provider: LoginProvider) -> Bool {
        isLinked(provider) && linkedProviders.count > 1
    }
}

/// Долг: `debtor` должен `lender`'у `sum`.
struct Debt: Codable, Identifiable, Hashable {
    let debtor: User
    let lender: User
    let sum: Int

    var id: String { "\(debtor.id)->\(lender.id)" }
}

/// Файл, прикреплённый к операции (чек/фото из Telegram).
struct OperationFile: Codable, Hashable, Identifiable {
    let type: String
    let fileId: String

    var id: String { fileId }
}

/// Способ деления расхода между получателями (контракт API v2).
enum SplitType: String, Codable, Hashable {
    /// Поровну: доли раскладывает сервер по каноническому правилу
    /// (base = S/n, остаток по рублю первым получателям массива).
    case equally
    /// Точными суммами: доли введены вручную, Σ долей == сумме операции.
    case byExactAmount = "by_exact_amount"

    /// Лениво: незнакомое/пустое значение (погашения, легаси-операции)
    /// читается как `equally`, а не роняет декодирование всего списка.
    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = SplitType(rawValue: raw) ?? .equally
    }
}

/// Получатель операции и его доля в ЦЕЛЫХ рублях — хранимая сервером:
/// для `equally` — канонически вычисленная, для `by_exact_amount` — введённая.
struct OperationRecipient: Codable, Hashable, Identifiable {
    let user: User
    let sum: Int

    var id: Int { user.id }
}

/// Доля участника в позиции чека (itemized-операция, AI-распознавание).
/// Сервер выводит плоские `recipients` операции из позиций и их долей.
struct ItemShare: Codable, Hashable, Identifiable {
    /// Telegram user id участника.
    let userId: Int
    /// Относительный вес доли (1 = поровну). Сервер игнорирует, если задан `amount`.
    let weight: Int
    /// Фиксированная сумма участника в целых рублях; nil — доля считается по весу.
    let amount: Int?

    var id: Int { userId }

    init(userId: Int, weight: Int = 1, amount: Int? = nil) {
        self.userId = userId
        self.weight = weight
        self.amount = amount
    }
}

/// Позиция чека itemized-операции: что заказали, почём и как делится.
/// Единый транспортный вид: read-модель операции, черновик `ParseDraft` и
/// write-path (`OperationBody.items`) — совпадает с серверным `ai.DraftItem`.
struct OperationItem: Codable, Hashable, Identifiable {
    /// Клиентский id строки — НЕ часть контракта (не кодируется и не участвует
    /// в сравнении). Нужен шиту правки: он живёт долго, а голосовая правка
    /// может за это время переставить позиции — по индексу «Готово» писало бы
    /// в чужую строку.
    let id = UUID()
    /// Название позиции («Пицца», «Сервисный сбор»).
    let name: String
    /// ВСЕГДА суммарная стоимость строки в целых рублях (уже с учётом количества).
    let price: Int
    /// Количество — только для отображения («×10»); в делении НЕ участвует.
    let qty: Int
    /// Доли участников; nil/пусто у надбавок (делятся по базе, а не по своим долям).
    let shares: [ItemShare]?
    /// «item» — обычная позиция, «surcharge» — надбавка (сбор/чаевые/доставка).
    let kind: String
    /// Правило деления надбавки «proportional» | «equally»; nil у обычных позиций.
    let split: String?
    /// Процент надбавки — только для показа («Сбор 10%»); в расчёте НЕ участвует.
    let percent: Int?
    /// Только в черновике (`ParseDraft`): нераспознанные имена для сопоставления
    /// участнику. В read-модели операции всегда nil.
    let unknown: [String]?

    /// Доли без опциональности (надбавки приходят без `shares`).
    var shareList: [ItemShare] { shares ?? [] }

    /// true — надбавка (сбор/чаевые/доставка), а не обычная позиция.
    var isSurcharge: Bool { kind == OperationItem.kindSurcharge }

    /// true — есть нераспознанные имена: черновик нельзя сохранять
    /// (сервер вернёт 400), пользователь должен сопоставить имена участникам.
    var hasUnknown: Bool { !(unknown ?? []).isEmpty }

    init(
        name: String,
        price: Int,
        qty: Int = 1,
        shares: [ItemShare]? = nil,
        kind: String = OperationItem.kindItem,
        split: String? = nil,
        percent: Int? = nil,
        unknown: [String]? = nil
    ) {
        self.name = name
        self.price = price
        self.qty = qty
        self.shares = shares
        self.kind = kind
        self.split = split
        self.percent = percent
        self.unknown = unknown
    }

    /// `id` — клиентская метка строки, поэтому вне кодирования и сравнения:
    /// иначе одинаковые позиции из ответа и из черновика считались бы разными
    /// (подсветка диффа, тесты) и id уезжал бы на сервер.
    private enum CodingKeys: String, CodingKey {
        case name, price, qty, shares, kind, split, percent, unknown
    }

    static func == (lhs: OperationItem, rhs: OperationItem) -> Bool {
        lhs.name == rhs.name && lhs.price == rhs.price && lhs.qty == rhs.qty
            && lhs.shares == rhs.shares && lhs.kind == rhs.kind && lhs.split == rhs.split
            && lhs.percent == rhs.percent && lhs.unknown == rhs.unknown
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(name)
        hasher.combine(price)
        hasher.combine(qty)
        hasher.combine(shares)
        hasher.combine(kind)
        hasher.combine(split)
        hasher.combine(percent)
        hasher.combine(unknown)
    }
}

extension OperationItem {
    /// Значения `kind`/`split` в контракте (см. серверный `internal/api`).
    static let kindItem = "item"
    static let kindSurcharge = "surcharge"
    static let splitProportional = "proportional"
    static let splitEqually = "equally"
}

/// Операция: расход или погашение долга.
struct Operation: Codable, Identifiable, Hashable {
    let id: String
    let description: String
    let sum: Int
    let isDebtRepayment: Bool
    /// Кто заплатил.
    let donor: User
    /// Получатели с долями; Σ долей == `sum`.
    let recipients: [OperationRecipient]
    /// Способ деления; может отсутствовать (погашения).
    let splitType: SplitType?
    let createdAt: Date
    /// Может быть пустым или отсутствовать.
    let files: [OperationFile]?
    /// Позиции чека itemized-операции (AI-распознавание); nil у обычных операций.
    /// `default = nil` — старый memberwise-инициализатор (тесты) остаётся валиден,
    /// а декодер читает поле опционально (сервер шлёт его только для itemized).
    var items: [OperationItem]? = nil

    var hasFiles: Bool { !(files ?? []).isEmpty }

    /// Позиции без опциональности; пусто — обычная (плоская) операция.
    var itemList: [OperationItem] { items ?? [] }
}

extension Operation {
    /// Операция «касается» пользователя: он платил или есть в получателях
    /// (фильтры «Со мной» на экране группы и «Только мои» в активности).
    func involves(_ userId: Int) -> Bool {
        donor.id == userId || recipients.contains { $0.user.id == userId }
    }

    /// Доля пользователя по ХРАНИМЫМ суммам получателей (не пересчёт!);
    /// nil — не участвует в делении.
    func recipientSum(of userId: Int) -> Int? {
        recipients.first { $0.user.id == userId }?.sum
    }

    /// Нетто-позиция пользователя в расходе по хранимым долям:
    /// >0 — одолжил, <0 — должен, 0 — расчёт, nil — не участвует.
    /// Донор: одолжил = `sum` − своя доля (если сам среди получателей).
    func netPosition(of userId: Int) -> Int? {
        let myShare = recipientSum(of: userId)
        if donor.id == userId {
            return sum - (myShare ?? 0)
        }
        if let myShare {
            return -myShare
        }
        return nil
    }
}

/// Черновик расхода из AI-распознавания (`POST /rooms/{id}/operations/parse`).
/// Клиент шлёт текущий черновик на голосовую правку, сервер возвращает новый.
struct ParseDraft: Codable, Hashable {
    let description: String
    let sum: Int
    /// Кто платил; nil — модель не определила донора.
    let donorId: Int?
    /// Позиции чека; item с непустым `unknown` требует сопоставления перед сохранением.
    let items: [OperationItem]?

    /// Позиции без опциональности.
    var itemList: [OperationItem] { items ?? [] }

    /// Есть ли нераспознанные имена хотя бы в одной позиции (блокирует сохранение).
    var hasUnknown: Bool { itemList.contains(where: \.hasUnknown) }

    init(description: String, sum: Int, donorId: Int? = nil, items: [OperationItem]? = nil) {
        self.description = description
        self.sum = sum
        self.donorId = donorId
        self.items = items
    }
}

/// Ответ распознавания: обновлённый черновик и опциональные уточняющие вопросы.
struct ParseResponse: Codable, Hashable {
    let draft: ParseDraft
    /// Уточняющие вопросы модели («кто платил?»); nil/пусто — вопросов нет.
    let questions: [String]?

    /// Вопросы без опциональности.
    var questionList: [String] { questions ?? [] }
}

// MARK: - Клиентское превью долей по позициям

extension Array where Element == OperationItem {
    /// Клиентское превью «кто сколько должен» по позициям — точное зеркало
    /// серверного `DeriveShares` (`internal/api/itemsplit.go`): снять фиксы →
    /// остаток по весам с детерминированным tie-break по userId → надбавки на базу.
    /// Возвращает (userId→сумма, итог) или nil, если позиции невалидны
    /// (перебор фиксов, неразделённый остаток, надбавка без цены).
    /// Нужно для превью в UI, чтобы клиент показывал ровно те суммы, что сохранит сервер.
    func derivedShares() -> (shares: [Int: Int], total: Int)? {
        var base: [Int: Int] = [:]
        var total = 0
        for item in self where !item.isSurcharge {
            guard let split = splitItem(item.price, item.shareList) else { return nil }
            for (id, value) in split { base[id, default: 0] += value }
            total += item.price
        }

        var out = base
        for item in self where item.isSurcharge {
            if item.price <= 0 { return nil }
            guard let surcharge = splitSurcharge(item.price, item.split, base) else { return nil }
            for (id, value) in surcharge {
                out[id, default: 0] += value
            }
            total += item.price
        }

        guard out.values.reduce(0, +) == total else { return nil }
        return (out, total)
    }
}

/// Делит `amount` между участниками пропорционально весам; остаток от округления —
/// по одному тем, у кого доля больше (tie-break по меньшему userId). Зеркало `splitByWeight`.
private func splitByWeight(_ amount: Int, _ weights: [(id: Int, weight: Int)]) -> [Int: Int]? {
    var out: [Int: Int] = [:]
    var totalWeight = 0
    for w in weights {
        let (sum, overflow) = totalWeight.addingReportingOverflow(w.weight)
        if overflow { return nil }
        totalWeight = sum
    }
    guard totalWeight > 0 else { return out }

    // Схлопываем дубли по id и отбрасываем нулевые веса ДО деления (зеркало
    // серверного splitByWeight): один участник может встретиться в shares дважды,
    // а у proportional-надбавки вес равен базовой доле и часто равен нулю.
    // Иначе остаток от округления уходил тому, кто ничего не ел, и дважды — дублю.
    var order: [(id: Int, weight: Int)] = []
    var indexById: [Int: Int] = [:]
    for w in weights where w.weight > 0 {
        if let i = indexById[w.id] {
            order[i].weight += w.weight
        } else {
            indexById[w.id] = order.count
            order.append(w)
        }
    }

    var given = 0
    for w in order {
        // Swift на переполнении не заворачивается, а падает: itemizedShares
        // пересчитывается на каждое нажатие клавиши, так что это крэш прямо
        // во время ввода. Сервер здесь возвращает ErrOverflow.
        let (product, overflow) = amount.multipliedReportingOverflow(by: w.weight)
        if overflow { return nil }
        let value = product / totalWeight
        out[w.id] = value
        given += value
    }
    let remainder = amount - given
    // При корректных входах остаток строго меньше числа участников; иначе это
    // признак переполнения, и цикл раздачи ниже стал бы неограниченным.
    guard remainder >= 0, remainder <= order.count else { return nil }
    guard remainder > 0 else { return out }

    let ranked = order.sorted { lhs, rhs in
        let lv = out[lhs.id] ?? 0
        let rv = out[rhs.id] ?? 0
        if lv != rv { return lv > rv }
        return lhs.id < rhs.id
    }
    guard !ranked.isEmpty else { return out }
    for i in 0..<remainder {
        out[ranked[i % ranked.count].id, default: 0] += 1
    }
    return out
}

/// Делит цену позиции: снимает фиксированные `amount`, остаток — по весам.
/// nil — фиксы превышают цену, отрицательный фикс или неразделённый остаток. Зеркало `SplitItem`.
private func splitItem(_ price: Int, _ shares: [ItemShare]) -> [Int: Int]? {
    var out: [Int: Int] = [:]
    var fixed = 0
    var weighted: [(id: Int, weight: Int)] = []
    for share in shares {
        if let amount = share.amount {
            if amount < 0 { return nil }
            out[share.userId, default: 0] += amount
            fixed += amount
            continue
        }
        if share.weight > 0 {
            weighted.append((id: share.userId, weight: share.weight))
        }
    }
    if fixed > price { return nil }
    let remainder = price - fixed
    if weighted.isEmpty {
        return remainder == 0 ? out : nil
    }
    guard let byWeight = splitByWeight(remainder, weighted) else { return nil }
    for (id, value) in byWeight {
        out[id, default: 0] += value
    }
    return out
}

/// Делит надбавку по базовым долям людей: proportional → вес = базовая доля,
/// иначе (или база нулевая) — поровну. Зеркало `SplitSurcharge`.
private func splitSurcharge(_ price: Int, _ rule: String?, _ base: [Int: Int]) -> [Int: Int]? {
    let ids = base.keys.sorted()
    let totalBase = base.values.reduce(0, +)
    var weights: [(id: Int, weight: Int)] = []
    for id in ids {
        var weight = 1
        if rule == OperationItem.splitProportional && totalBase > 0 {
            weight = base[id] ?? 0
        }
        weights.append((id: id, weight: weight))
    }
    return splitByWeight(price, weights)
}

/// Сумма в конкретной валюте. Суммы в разных валютах НЕ складываются между
/// собой — агрегируются только повалютно (см. `aggregateByCurrency`).
struct CurrencySum: Codable, Hashable {
    let currency: String
    let sum: Int
}

/// Валюта из справочника GET /currencies: код, символ и флаг для пикера.
struct CurrencyInfo: Codable, Identifiable, Hashable {
    let code: String
    let symbol: String
    let flag: String

    var id: String { code }
}

/// Строка списка групп.
struct RoomSummary: Codable, Identifiable, Hashable {
    let id: String
    let name: String
    let createdAt: Date
    let isArchived: Bool
    let members: [User]
    let memberCount: Int
    /// Валюта комнаты («RUB»/«USD»/«EUR»/«IDR») — в ней все суммы комнаты.
    let currency: String
    /// Сумма всех расходов комнаты (без погашений).
    let totalSpent: Int
    /// >0 — мне должны, <0 — я должен, 0 — расчёт.
    let myBalance: Int
    /// Долги группы неисчислимы (старые данные бота: доли не сходятся). Сервер
    /// шлёт `debtsUnavailable: true` с `myBalance=0` — без этого флага нулевой
    /// баланс читался бы как «все в расчёте», то есть ложное утверждение о деньгах.
    /// omitempty на сервере → у здоровых комнат ключа нет, отсюда default false.
    var debtsUnavailable: Bool = false
}

// init(from:) в extension, чтобы сохранить memberwise-инициализатор (объявление
// init прямо в теле структуры его подавляет). Ключ debtsUnavailable опциональный:
// сервер шлёт его с omitempty, у здоровых комнат его в ответе нет.
extension RoomSummary {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        createdAt = try c.decode(Date.self, forKey: .createdAt)
        isArchived = try c.decode(Bool.self, forKey: .isArchived)
        members = try c.decode([User].self, forKey: .members)
        memberCount = try c.decode(Int.self, forKey: .memberCount)
        currency = try c.decode(String.self, forKey: .currency)
        totalSpent = try c.decode(Int.self, forKey: .totalSpent)
        myBalance = try c.decode(Int.self, forKey: .myBalance)
        debtsUnavailable = try c.decodeIfPresent(Bool.self, forKey: .debtsUnavailable) ?? false
    }
}

/// Экран группы одним запросом.
struct RoomDetail: Codable, Identifiable, Hashable {
    let id: String
    let name: String
    let createdAt: Date
    let isArchived: Bool
    let members: [User]
    /// Валюта комнаты — в ней показываются ВСЕ суммы экрана группы.
    let currency: String
    let totalSpent: Int
    /// Моя доля расходов.
    let mySpent: Int
    let myBalance: Int
    /// Все долги комнаты.
    let debts: [Debt]
    /// Все операции, новые первыми.
    let operations: [Operation]
    /// Долги группы неисчислимы — см. `RoomSummary.debtsUnavailable`.
    var debtsUnavailable: Bool = false
}

extension RoomDetail {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        createdAt = try c.decode(Date.self, forKey: .createdAt)
        isArchived = try c.decode(Bool.self, forKey: .isArchived)
        members = try c.decode([User].self, forKey: .members)
        currency = try c.decode(String.self, forKey: .currency)
        totalSpent = try c.decode(Int.self, forKey: .totalSpent)
        mySpent = try c.decode(Int.self, forKey: .mySpent)
        myBalance = try c.decode(Int.self, forKey: .myBalance)
        debts = try c.decode([Debt].self, forKey: .debts)
        operations = try c.decode([Operation].self, forKey: .operations)
        debtsUnavailable = try c.decodeIfPresent(Bool.self, forKey: .debtsUnavailable) ?? false
    }
}

/// Баланс с другом по одной группе (только ненулевые) — в валюте этой группы.
struct FriendRoomBalance: Codable, Identifiable, Hashable {
    let roomId: String
    let roomName: String
    /// Валюта комнаты.
    let currency: String
    let balance: Int

    var id: String { roomId }
}

/// Друг и нетто-балансы с ним ПО ВАЛЮТАМ: >0 — друг должен мне, <0 — я должен.
/// Единого поля `total` нет: суммы разных валют не складываются.
struct FriendBalance: Codable, Identifiable, Hashable {
    let user: User
    /// Нетто по каждой валюте (сервер отдаёт как есть, без сортировки).
    let totalsByCurrency: [CurrencySum]
    let rooms: [FriendRoomBalance]

    var id: Int { user.id }

    /// Ненулевые итоги по убыванию |суммы| (первая валюта — «основная»);
    /// пусто — полный расчёт по всем валютам.
    var totals: [CurrencySum] { aggregateByCurrency(totalsByCurrency) }
}

/// Элемент ленты активности.
struct ActivityItem: Codable, Identifiable, Hashable {
    let roomId: String
    let roomName: String
    /// Валюта комнаты операции — в ней показываются суммы строки.
    let roomCurrency: String
    let operation: Operation

    var id: String { operation.id }
}

// MARK: - Статистика группы (дашборд «Итоги»)

/// Траты одного дня; `date` — «2026-07-05» (локальная дата, не RFC3339).
struct DailySum: Codable, Hashable {
    let date: String
    let sum: Int
}

extension DailySum {
    private static let dayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = .current
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    /// «2026-07-05» → Date (начало дня в текущем поясе); nil — битая строка.
    var day: Date? {
        Self.dayFormatter.date(from: date)
    }
}

/// Траты одного календарного месяца; `month` — «2026-02».
struct MonthlySum: Codable, Hashable {
    let month: String
    let sum: Int
}

/// Сумма участника («Кто платил» / «Чья доля»).
struct MemberSum: Codable, Hashable, Identifiable {
    let user: User
    let sum: Int

    var id: Int { user.id }
}

/// Строка «Топ расходов».
struct TopOperation: Codable, Hashable, Identifiable {
    let id: String
    let description: String
    let sum: Int
    let donor: User
    let createdAt: Date
}

/// Статистика группы GET /rooms/{id}/statistics — данные дашборда «Итоги».
/// Все суммы — в валюте комнаты `currency`.
struct Statistics: Codable, Hashable {
    let currency: String
    let totalSpent: Int
    /// Потрачено за текущий календарный месяц.
    let monthSpent: Int
    /// Траты по дням (дни без трат сервер может опускать — клиент дополняет нулями).
    let byDay: [DailySum]
    /// Траты по календарным месяцам: ровно 6 месяцев включая текущий,
    /// по возрастанию, месяцы без трат — нули (готовый ряд для графика).
    let byMonth: [MonthlySum]
    /// Количество расходов за всё время (active, без погашений).
    let operationCount: Int
    /// Кто сколько заплатил (донор операций).
    let paidByMember: [MemberSum]
    /// Чья какая доля (по хранимым долям получателей).
    let shareByMember: [MemberSum]
    /// Топ расходов по сумме, убывание.
    let topOperations: [TopOperation]
}

extension Statistics {
    private enum CodingKeys: String, CodingKey {
        case currency, totalSpent, monthSpent, byDay, byMonth, operationCount
        case paidByMember, shareByMember, topOperations
    }

    /// Лениво: `byMonth`/`operationCount` могут отсутствовать (старый офлайн-кеш,
    /// ещё не обновлённый бэкенд) — тогда дефолты `[]`/`0`, а не ошибка
    /// декодирования всего дашборда.
    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        currency = try container.decode(String.self, forKey: .currency)
        totalSpent = try container.decode(Int.self, forKey: .totalSpent)
        monthSpent = try container.decode(Int.self, forKey: .monthSpent)
        byDay = try container.decode([DailySum].self, forKey: .byDay)
        byMonth = try container.decodeIfPresent([MonthlySum].self, forKey: .byMonth) ?? []
        operationCount = try container.decodeIfPresent(Int.self, forKey: .operationCount) ?? 0
        paidByMember = try container.decode([MemberSum].self, forKey: .paidByMember)
        shareByMember = try container.decode([MemberSum].self, forKey: .shareByMember)
        topOperations = try container.decode([TopOperation].self, forKey: .topOperations)
    }
}

/// Ответ авторизации (`/auth/telegram`, `/auth/dev`).
struct AuthResponse: Codable {
    let token: String
    let user: Me
}

/// Ответ привязки/отвязки способа входа (`POST`/`DELETE /me/link/{provider}`).
///
/// `warning` приходит при отвязке Telegram: бот заведёт отдельный профиль,
/// если человек снова ему напишет, и привязать этот Telegram обратно уже не
/// получится. Сервер шлёт текст с `omitempty` — клиент обязан его показать.
struct LinkedProvidersResponse: Codable {
    let user: Me
    let warning: String?
}
