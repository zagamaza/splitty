import SwiftUI

/// Цифры суммы с пробелами-разделителями тысяч, без знака и «₽»: `1234567` → `"1 234 567"`.
private func thousandsGrouped(_ sum: Int) -> String {
    let digits = Array(String(sum.magnitude))
    var reversed: [Character] = []
    for (index, char) in digits.reversed().enumerated() {
        if index > 0 && index % 3 == 0 {
            reversed.append(" ")
        }
        reversed.append(char)
    }
    return String(reversed.reversed())
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
    case "UZS": return "сум"
    default: return currency
    }
}

/// Форматирует сумму в валюте: `money(1234567, currency: "USD")` → `"1 234 567 $"`.
/// Формат единый с рублями: разделитель тысяч — обычный пробел,
/// символ валюты после суммы, суммы всегда целые.
func money(_ sum: Int, currency: String) -> String {
    (sum < 0 ? "-" : "") + thousandsGrouped(sum) + " " + currencySymbol(currency)
}

/// Форматирует сумму в рублях: `1234567` → `"1 234 567 ₽"` (обёртка money(_, "RUB")).
func rubles(_ sum: Int) -> String {
    money(sum, currency: "RUB")
}

/// Диапазон сумм для неровного деления: `moneyRange(333, 334, currency: "RUB")` → `"333–334 ₽"`.
func moneyRange(_ minSum: Int, _ maxSum: Int, currency: String) -> String {
    "\(thousandsGrouped(minSum))–\(money(maxSum, currency: currency))"
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
    return (0..<count).map { $0 < remainder ? base + 1 : base }
}

