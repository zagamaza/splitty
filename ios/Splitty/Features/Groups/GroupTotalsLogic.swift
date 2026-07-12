import Foundation

// Чистая логика дашборда «Итоги» (без SwiftUI) — покрывается юнит-тестами:
// назначение категориальных цветов участникам, нетто-балансы, агрегация
// по дням недели, сегменты доната «Кто платил», русские подписи месяцев.

// MARK: - Назначение цветов участникам

/// Правило категориальных цветов дашборда: участники комнаты сортируются по
/// `user.id` ASC и получают индексы 0…5 палитры `Color.chartCategorical`.
/// Больше 6 участников: 7-й и дальше цвета НЕ получают (рисуются
/// `inkSecondary`, в донате сворачиваются в «Прочие»). Палитра никогда
/// не циклится; один человек — один цвет во всех графиках дашборда.
enum MemberPalette {
    /// Размер фиксированной палитры (== `Color.chartCategorical.count`).
    static let colorCount = 6

    /// `user.id` → индекс цвета палитры. Дубликаты id схлопываются;
    /// id без записи в словаре — «прочие» (inkSecondary).
    static func colorIndices(memberIds: some Sequence<Int>) -> [Int: Int] {
        var map: [Int: Int] = [:]
        for (index, id) in Set(memberIds).sorted().enumerated() where index < colorCount {
            map[id] = index
        }
        return map
    }
}

// MARK: - Данные графиков

/// Нетто-баланс участника для «Баланса участников»: net = заплатил − его доля.
struct MemberNet: Hashable, Identifiable {
    let user: User
    let net: Int

    var id: Int { user.id }
}

/// Сегмент доната «Кто платил»: участник или собирательный «Прочие»
/// (`userId == nil` — сумма всех, кто не попал в топ).
struct DonutSlice: Hashable, Identifiable {
    let userId: Int?
    let label: String
    let sum: Int

    var id: Int { userId ?? .min }
}

/// Расчёты дашборда «Итоги» из `Statistics` (целочисленная арифметика).
enum DashboardMath {
    /// «Баланс участников»: net = paid − share для каждого, кто есть хоть
    /// в одном из списков (включая нулевые нетто). Сортировка по net
    /// убыванию, при равенстве — по user.id (стабильный порядок).
    static func netBalances(paid: [MemberSum], share: [MemberSum]) -> [MemberNet] {
        var users: [Int: User] = [:]
        var nets: [Int: Int] = [:]
        for member in paid {
            users[member.user.id] = member.user
            nets[member.user.id, default: 0] += member.sum
        }
        for member in share {
            users[member.user.id] = member.user
            nets[member.user.id, default: 0] -= member.sum
        }
        return nets
            .compactMap { id, net in users[id].map { MemberNet(user: $0, net: net) } }
            .sorted {
                if $0.net != $1.net { return $0.net > $1.net }
                return $0.user.id < $1.user.id
            }
    }

    /// «По дням недели»: суммы `byDay` по дню недели, 7 значений,
    /// индекс 0 — понедельник … 6 — воскресенье. Битые даты пропускаются.
    static func weekdayTotals(byDay: [DailySum]) -> [Int] {
        var totals = Array(repeating: 0, count: 7)
        let calendar = Calendar.current
        for daily in byDay {
            guard let day = daily.day else { continue }
            // Calendar.weekday: 1 = воскресенье … 7 = суббота → 0 = понедельник.
            let index = (calendar.component(.weekday, from: day) + 5) % 7
            totals[index] += daily.sum
        }
        return totals
    }

    /// Сегменты доната «Кто платил»: остаются только положительные платежи
    /// (углы сектора не бывают отрицательными), сортировка по сумме убыванию
    /// (при равенстве — по user.id). Больше `maxSegments` участников —
    /// топ-(maxSegments−1) + серый «Прочие» с суммой остальных.
    /// Тёзки различаются суффиксом « (2)». Пустой список — рисовать нечего.
    static func donutSlices(
        paid: [MemberSum],
        maxSegments: Int = MemberPalette.colorCount
    ) -> [DonutSlice] {
        let sorted = paid
            .filter { $0.sum > 0 }
            .sorted {
                if $0.sum != $1.sum { return $0.sum > $1.sum }
                return $0.user.id < $1.user.id
            }
        let (top, rest) = sorted.count > maxSegments
            ? (Array(sorted.prefix(maxSegments - 1)), Array(sorted.dropFirst(maxSegments - 1)))
            : (sorted, [])

        var seen: [String: Int] = [:]
        var slices = top.map { member in
            let name = member.user.displayName
            let count = (seen[name] ?? 0) + 1
            seen[name] = count
            return DonutSlice(
                userId: member.user.id,
                label: count > 1 ? "\(name) (\(count))" : name,
                sum: member.sum
            )
        }
        if !rest.isEmpty {
            slices.append(DonutSlice(
                userId: nil,
                label: "Прочие",
                sum: rest.reduce(0) { $0 + $1.sum }
            ))
        }
        return slices
    }

    /// Русские короткие подписи месяцев для «Динамики по месяцам».
    private static let monthNames = [
        "янв", "фев", "мар", "апр", "май", "июн",
        "июл", "авг", "сен", "окт", "ноя", "дек",
    ]
}
