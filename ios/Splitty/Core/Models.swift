import Foundation

// Codable DTO REST API — точно по контракту docs/API.md.
// Деньги — целые рубли (Int); id комнат/операций — hex-строки ObjectID;
// id пользователей — Telegram user id (Int).

/// Пользователь.
struct User: Codable, Identifiable, Hashable {
    let id: Int
    let username: String?
    let displayName: String
}

/// Профиль текущего пользователя.
struct Me: Codable, Identifiable, Hashable {
    let id: Int
    let username: String?
    let displayName: String
    let lang: String
    let notificationOn: Bool
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

    var hasFiles: Bool { !(files ?? []).isEmpty }
}

extension Operation {
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
