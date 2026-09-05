import SwiftUI

/// Форматтеры сумм по паре «локаль + валюта».
///
/// Раньше разделитель тысяч и место символа валюты склеивались руками: всегда
/// пробел и всегда символ справа. Это русский формат — человек с английским
/// интерфейсом видел «1 234 567 $» вместо «$1,234,567».
private enum MoneyFormat {
    private static var cache: [String: NumberFormatter] = [:]
    private static let lock = NSLock()

    /// Шов для тестов: подменяемая локаль. nil — текущая локаль системы.
    static var localeOverride: Locale?

    static var locale: Locale { localeOverride ?? Locale.current }

    static func formatter(currency: String) -> NumberFormatter {
        let key = "\(locale.identifier)|\(currency)"
        lock.lock()
        defer { lock.unlock() }
        if let cached = cache[key] { return cached }
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.locale = locale
        formatter.currencyCode = currency
        // Копеек в продукте нет: суммы всегда целые
        formatter.maximumFractionDigits = 0
        formatter.minimumFractionDigits = 0
        // Символ — свой: у системы для IDR это «IDR», для KZT «KZT», а незнакомый
        // код она подменяет символом чужой валюты (GBP → «£»). От системы берём
        // только разделитель тысяч и СТОРОНУ, с которой стоит символ
        formatter.currencySymbol = currencySymbol(currency)
        cache[key] = formatter
        return formatter
    }
}

/// Язык интерфейса — им решается, годится ли текст ошибки от сервера.
///
/// Бэкенд отвечает только по-русски и `Accept-Language` не смотрит, поэтому
/// его `message` можно показывать, лишь пока интерфейс русский. Отдельный шов,
/// а не чтение `Bundle` по месту: набор тестов идёт под `ru`, и нерусскую
/// ветку иначе нечем было бы проверить.
enum ServerTextLocale {
    /// Подменённый язык (тесты). nil — язык, на котором собран интерфейс.
    static var override: String?

    static var isRussian: Bool {
        let language = override ?? Bundle.main.preferredLocalizations.first ?? "en"
        return language.hasPrefix("ru")
    }
}

/// Локаль форматирования сумм (шов для тестов).
enum MoneyLocale {
    static var override: Locale? {
        get { MoneyFormat.localeOverride }
        set { MoneyFormat.localeOverride = newValue }
    }
}

/// Символ валюты по коду: RUB → «₽», USD → «$», EUR → «€», IDR → «Rp»,
/// KZT → «₸», UZS → «сум»; незнакомый код показывается как есть («GBP»).
func currencySymbol(_ currency: String) -> String {
    switch currency {
    case "RUB": return "₽"
    case "USD": return "$"
    case "EUR": return "€"
    case "IDR": return "Rp"
    case "KZT": return "₸"
    case "UZS": return String(localized: "сум")
    default: return currency
    }
}

/// Форматирует сумму в валюте: `money(1234567, currency: "USD")` → `"1 234 567 $"`.
/// Формат единый с рублями: разделитель тысяч — обычный пробел,
/// символ валюты после суммы, суммы всегда целые.
func money(_ sum: Int, currency: String) -> String {
    let formatter = MoneyFormat.formatter(currency: currency)
    return formatter.string(from: NSNumber(value: sum))
        ?? "\(sum) \(currencySymbol(currency))"
}

/// Форматирует сумму в рублях: `1234567` → `"1 234 567 ₽"` (обёртка money(_, "RUB")).
func rubles(_ sum: Int) -> String {
    money(sum, currency: "RUB")
}

/// Диапазон сумм для неровного деления: `moneyRange(333, 334, currency: "RUB")` → `"333–334 ₽"`.
func moneyRange(_ minSum: Int, _ maxSum: Int, currency: String) -> String {
    // Нижняя граница — голое число в формате локали (без символа валюты):
    // символ печатается один раз, у верхней границы
    let plain = NumberFormatter()
    plain.numberStyle = .decimal
    plain.locale = MoneyFormat.locale
    plain.maximumFractionDigits = 0
    let lower = plain.string(from: NSNumber(value: minSum)) ?? "\(minSum)"
    return "\(lower)–\(money(maxSum, currency: currency))"
}

/// Рублёвый диапазон (обёртка moneyRange(_, _, "RUB") — для тестов и легаси).
func rublesRange(_ minSum: Int, _ maxSum: Int) -> String {
    moneyRange(minSum, maxSum, currency: "RUB")
}

// MARK: - Суммы по валютам

/// Складывает суммы по валютам: суммы в РАЗНЫХ валютах никогда не смешиваются.
/// Результат — без нулевых итогов, по убыванию |суммы| (первая — «основная»
/// для крупного показа), при равенстве — по коду валюты (стабильный порядок).
func aggregateByCurrency(_ amounts: [CurrencySum]) -> [CurrencySum] {
    var totals: [String: Int] = [:]
    for amount in amounts {
        totals[amount.currency, default: 0] += amount.sum
    }
    return totals
        .filter { $0.value != 0 }
        .map { CurrencySum(currency: $0.key, sum: $0.value) }
        .sorted {
            if abs($0.sum) != abs($1.sum) {
                return abs($0.sum) > abs($1.sum)
            }
            return $0.currency < $1.currency
        }
}

// MARK: - Каноническое правило деления расхода (единое с сервером)

/// Доли получателей расхода `sum` (целые рубли) на `count` человек
/// в порядке массива получателей: base = sum / count, r = sum % count;
/// получатель с индексом i платит base+1 при i < r, иначе base.
/// Сумма долей всегда равна `sum`. Арифметика целочисленная.
///
/// ВАЖНО: доли операций API отдаёт ГОТОВЫМИ (`Operation.recipients[].sum`) —
/// позиции пользователя считать из них (`Operation.recipientSum/netPosition`).
/// Этот хелпер — только для подсказки предпросмотра в форме добавления
/// расхода (`splitHint`), пока операция ещё не создана.
func shares(sum: Int, count: Int) -> [Int] {
    guard count > 0 else { return [] }
    let base = sum / count
    let remainder = sum % count
    // Swift усекает деление к нулю, поэтому у отрицательной суммы остаток
    // отрицательный и `$0 < remainder` не срабатывал ни разу — Σ долей != sum.
    if remainder < 0 {
        return (0..<count).map { $0 < -remainder ? base - 1 : base }
    }
    return (0..<count).map { $0 < remainder ? base + 1 : base }
}

