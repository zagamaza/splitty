import Foundation

/// Форматтеры дат с русской локалью.
enum DateFmt {
    private static let ru = Locale(identifier: "ru_RU")

    private static let dayMonthFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = ru
        formatter.dateFormat = "d MMM"
        return formatter
    }()

    private static let monthYearFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = ru
        formatter.dateFormat = "LLLL yyyy"
        return formatter
    }()

    private static let relativeFormatter: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.locale = ru
        formatter.unitsStyle = .short
        return formatter
    }()

    /// «5 июл» — день и короткий месяц (для колонки даты операции).
    static func dayMonth(_ date: Date) -> String {
        dayMonthFormatter.string(from: date).replacingOccurrences(of: ".", with: "")
    }

    /// «Июль 2026» — заголовок секции месяца.
    static func monthYear(_ date: Date) -> String {
        let string = monthYearFormatter.string(from: date)
        return string.prefix(1).uppercased() + string.dropFirst()
    }

    /// Относительное время: «2 ч. назад» (для ленты активности).
    static func relative(_ date: Date, since reference: Date = Date()) -> String {
        relativeFormatter.localizedString(for: date, relativeTo: reference)
    }
}
