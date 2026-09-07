import SwiftUI

/// Форматтеры сумм по паре «локаль + валюта».
///
/// Раньше разделитель тысяч и место символа валюты склеивались руками: всегда
/// пробел и всегда символ справа. Это русский формат — человек с английским
/// интерфейсом видел «1 234 567 $» вместо «$1,234,567».
enum MoneyFormat {
    private static var cache: [String: NumberFormatter] = [:]
    private static let lock = NSLock()

    /// Шов для тестов: подменяемая локаль. nil — текущая локаль системы.
    static var localeOverride: Locale?

    static var locale: Locale { localeOverride ?? Locale.current }

    static func formatter(currency: String, fractional: Bool = false) -> NumberFormatter {
        let key = "\(locale.identifier)|\(currency)|\(fractional)"
        lock.lock()
        defer { lock.unlock() }
        if let cached = cache[key] { return cached }
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.locale = locale
        formatter.currencyCode = currency
        // Копейки показываются только там, где туса их считает. В остальных
        // суммы целые: дробная часть у рублёвой поездки — визуальный шум.
        let digits = fractional ? 2 : 0
        formatter.maximumFractionDigits = digits
        formatter.minimumFractionDigits = digits
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

/// Символ валюты по коду: RUB → «₽», USD → «$», EUR → «€», JPY/CNY → «¥»,
/// KRW → «₩», BRL → «R$», IDR → «Rp», KZT → «₸», UZS → «сум»; незнакомый код
/// показывается как есть («GBP»).
///
/// Иена и юань делят «¥» намеренно: так их пишут дома, а в одной комнате
/// валюта всегда одна — спутать не с чем.
func currencySymbol(_ currency: String) -> String {
    switch currency {
    case "RUB": return "₽"
    case "USD": return "$"
    case "EUR": return "€"
    case "JPY", "CNY": return "¥"
    case "KRW": return "₩"
    case "BRL": return "R$"
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

/// Форматирует сумму, ЗАДАННУЮ В КОПЕЙКАХ: `money(minor: 2080, currency: "USD",
/// fractional: true)` → `"20,80 $"`. В тусе без копеек значение округляется до
/// целого — там дробей не показывают вовсе.
///
/// Минорные единицы — точное значение суммы, старое целое поле лишь его
/// проекция. Показывать надо это, иначе 20,80 на экране превратится в 21.
func money(minor: Int, currency: String, fractional: Bool) -> String {
    let formatter = MoneyFormat.formatter(currency: currency, fractional: fractional)
    let value = Double(minor) / Double(minorFactor)
    return formatter.string(from: NSNumber(value: value))
        ?? "\(minor / minorFactor) \(currencySymbol(currency))"
}

/// Сотая доля единицы валюты: в этих единицах сервер хранит все суммы.
let minorFactor = 100

/// Форматирует точную сумму в копейках, САМ решая, показывать ли дробную часть:
/// `2080` → `"20,80 $"`, `2100` → `"21 $"`.
///
/// Точность выводится из значения, а не из настройки тусы, намеренно. Иначе
/// признак пришлось бы тянуть в два десятка мест показа, включая экраны позиций
/// чека, где комнаты под рукой нет вовсе. В тусе без копеек нецелых сумм не
/// возникает — там ничего и не изменится.
func money(minor: Int, currency: String) -> String {
    money(minor: minor, currency: currency, fractional: minor % minorFactor != 0)
}

/// Разбирает введённую человеком сумму в МИНОРНЫЕ единицы: `"20,80"` и
/// `"20.80"` → `2080`, `"21"` → `2100`.
///
/// Оба разделителя принимаются намеренно: запятую даёт русская клавиатура,
/// точку — цифровая панель и большинство раскладок, и человек не обязан гадать,
/// какая из них «правильная». Дробная часть длиннее двух знаков — не сумма.
func minorFromInput(_ text: String) -> Int? {
    let trimmed = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
    if trimmed.isEmpty { return nil }
    let parts = trimmed.split(separator: ".", omittingEmptySubsequences: false)
    guard parts.count <= 2, let units = Int(parts[0]), units >= 0 else { return nil }
    if parts.count == 1 { return units * minorFactor }
    let fraction = parts[1]
    guard !fraction.isEmpty, fraction.count <= 2, fraction.allSatisfy(\.isNumber),
          let value = Int(fraction) else { return nil }
    // «20.8» — это 80 копеек, а не 8.
    return units * minorFactor + (fraction.count == 1 ? value * 10 : value)
}

/// Готовит сумму для поля ввода: `2080` → `"20,80"`, `2100` → `"21"`.
/// Разделитель — из локали, чтобы человек увидел привычный ему знак.
func inputTextFromMinor(_ minor: Int) -> String {
    if minor % minorFactor == 0 { return String(minor / minorFactor) }
    let separator = MoneyFormat.locale.decimalSeparator ?? ","
    return String(format: "%d%@%02d", minor / minorFactor, separator, abs(minor % minorFactor))
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

