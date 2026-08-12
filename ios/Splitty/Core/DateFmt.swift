import Foundation

/// Форматтеры дат.
///
/// Локаль берётся ТЕКУЩАЯ, а не русская: приложение переведено на пять языков,
/// и человек, выбравший английский, видел даты по-русски.
enum DateFmt {
    /// Кеш форматтеров по паре «локаль + шаблон». Ключ включает локаль, поэтому
    /// смена языка на лету сама даёт другой форматтер — сбрасывать нечего.
    private static var cache: [String: DateFormatter] = [:]
    private static var relativeCache: [String: RelativeDateTimeFormatter] = [:]
    private static let lock = NSLock()

    /// Шов для тестов: подменяемая локаль. nil — текущая локаль системы.
    static var localeOverride: Locale?

    static var locale: Locale { localeOverride ?? Locale.current }

    private static func formatter(_ template: String) -> DateFormatter {
        let key = "\(locale.identifier)|\(template)"
        lock.lock()
        defer { lock.unlock() }
        if let cached = cache[key] { return cached }
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.setLocalizedDateFormatFromTemplate(template)
        cache[key] = formatter
        return formatter
    }

    /// «5» — день месяца отдельной строкой (колонка даты в списке операций).
    ///
    /// День и месяц отдельными функциями, а не разбором «5 июл» по пробелу:
    /// в английском порядок обратный («Jul 5»), и колонка показывала месяц
    /// вместо числа. Разбор строки предполагал русский порядок молча.
    static func day(_ date: Date) -> String {
        formatter("d").string(from: date)
    }

    /// «июл» — короткий месяц отдельной строкой.
    static func month(_ date: Date) -> String {
        formatter("MMM").string(from: date).replacingOccurrences(of: ".", with: "")
    }

    /// «5 июл» — день и короткий месяц одной строкой, в порядке локали.
    static func dayMonth(_ date: Date) -> String {
        formatter("d MMM").string(from: date).replacingOccurrences(of: ".", with: "")
    }

    /// «Июль 2026» — заголовок секции месяца.
    static func monthYear(_ date: Date) -> String {
        let string = formatter("LLLL yyyy").string(from: date)
        return string.prefix(1).uppercased() + string.dropFirst()
    }

    /// Относительное время: «2 ч. назад» (для ленты активности).
    static func relative(_ date: Date, since reference: Date = Date()) -> String {
        let key = locale.identifier
        lock.lock()
        let cached = relativeCache[key]
        lock.unlock()
        if let cached {
            return cached.localizedString(for: date, relativeTo: reference)
        }
        let formatter = RelativeDateTimeFormatter()
        formatter.locale = locale
        formatter.unitsStyle = .short
        lock.lock()
        relativeCache[key] = formatter
        lock.unlock()
        return formatter.localizedString(for: date, relativeTo: reference)
    }
}
