package com.zagir.splitty.core.money

import com.zagir.splitty.core.model.CurrencySum
import java.text.DecimalFormat
import java.text.NumberFormat
import java.util.Locale
import kotlin.math.abs

/**
 * Денежная арифметика и форматирование — порт канонических правил проекта
 * (ios/Splitty/Core/Money.swift, docs/API.md). Все суммы — ЦЕЛЫЕ (копеек нет),
 * float в денежных расчётах запрещён.
 */

/**
 * Форматтеры сумм по паре «локаль + валюта».
 *
 * Раньше разделитель тысяч и место символа валюты склеивались руками: всегда
 * пробел и всегда символ справа. Это русский формат — человек с английским
 * интерфейсом видел «1 234 567 $» вместо «$1,234,567».
 */
internal object MoneyFormat {
    private val cache = HashMap<String, NumberFormat>()

    /** Шов для тестов: подменяемая локаль. null — текущая локаль системы. */
    var localeOverride: Locale? = null

    val locale: Locale get() = localeOverride ?: Locale.getDefault()

    @Synchronized
    fun formatter(currency: String, fractional: Boolean = false): NumberFormat {
        val key = "${locale.toLanguageTag()}|$currency|$fractional"
        cache[key]?.let { return it }
        val formatter = NumberFormat.getCurrencyInstance(locale)
        // Копейки показываются только там, где туса их считает. В остальных
        // суммы целые: дробная часть у рублёвой поездки — визуальный шум.
        val digits = if (fractional) 2 else 0
        formatter.maximumFractionDigits = digits
        formatter.minimumFractionDigits = digits
        val symbols = (formatter as? DecimalFormat)?.decimalFormatSymbols
        if (symbols != null) {
            // Символ — свой: у системы для IDR это «IDR», для KZT «KZT», а
            // незнакомый код она подменяет символом чужой валюты. От системы
            // берём только разделитель тысяч и СТОРОНУ, с которой стоит символ
            symbols.currencySymbol = currencySymbol(currency)
            formatter.decimalFormatSymbols = symbols
        }
        cache[key] = formatter
        return formatter
    }
}

/** Локаль форматирования сумм (шов для тестов). */
object MoneyLocale {
    var override: Locale?
        get() = MoneyFormat.localeOverride
        set(value) {
            MoneyFormat.localeOverride = value
        }
}

/**
 * Символ валюты по коду контракта: RUB -> ₽, USD -> $, EUR -> €, JPY/CNY -> ¥,
 * KRW -> ₩, BRL -> R$, IDR -> Rp; незнакомый код показывается как есть ("GBP").
 *
 * Иена и юань делят «¥» намеренно: так их пишут дома, а в одной комнате валюта
 * всегда одна — спутать не с чем.
 */
fun currencySymbol(currency: String): String = when (currency) {
    "RUB" -> "₽"
    "USD" -> "$"
    "EUR" -> "€"
    "JPY", "CNY" -> "¥"
    "KRW" -> "₩"
    "BRL" -> "R$"
    "IDR" -> "Rp"
    "KZT" -> "₸"
    "UZS" -> uzbekSum(MoneyFormat.locale)
    else -> currency
}

/**
 * Сум — единственная валюта контракта, у которой нет знака: она пишется словом,
 * и слово у каждого языка своё. Ресурс сюда не дотянуть — функция чистая и
 * живёт вне Compose, — но локаль тут ровно та же, что форматирует саму сумму,
 * вместе с тестовым швом [MoneyLocale]. Раньше строка была русской всегда, и
 * немец с испанцем читали «1 000 сум» кириллицей. Значения — те же, что на iOS.
 */
private fun uzbekSum(locale: Locale): String = when (locale.language) {
    "ru" -> "сум"
    "de" -> "Sum"
    else -> "sum"
}

/**
 * Форматирует сумму в валюте: money(1234567, "USD") -> "1 234 567 $".
 * Разделитель тысяч — обычный пробел, символ валюты ПОСЛЕ суммы, суммы целые.
 */
/** Сотая доля единицы валюты: в этих единицах сервер хранит все суммы. */
const val MINOR_FACTOR = 100L

/**
 * Форматирует точную сумму в копейках, САМ решая, показывать ли дробную часть:
 * `2080` → «20,80 $», `2100` → «21 $».
 *
 * Точность выводится из значения, а не из настройки тусы: иначе признак
 * пришлось бы тянуть в два десятка мест показа. В тусе без копеек нецелых сумм
 * не возникает — там ничего и не меняется.
 */
fun moneyMinor(minor: Long, currency: String): String {
    val fractional = minor % MINOR_FACTOR != 0L
    val formatter = MoneyFormat.formatter(currency, fractional)
    return formatter.format(minor.toDouble() / MINOR_FACTOR)
}

/**
 * Разбирает введённую человеком сумму в МИНОРНЫЕ единицы: «20,80» и «20.80» →
 * 2080, «21» → 2100, null — это не сумма.
 *
 * Оба разделителя принимаются намеренно: запятую даёт русская клавиатура,
 * точку — цифровая панель, и человек не обязан гадать, какая правильная.
 */
fun minorFromInput(text: String): Long? {
    val trimmed = text.trim().replace(',', '.')
    if (trimmed.isEmpty()) return null
    val parts = trimmed.split('.')
    if (parts.size > 2) return null
    val units = parts[0].toLongOrNull() ?: return null
    if (units < 0) return null
    if (parts.size == 1) return units * MINOR_FACTOR
    val fraction = parts[1]
    if (fraction.isEmpty() || fraction.length > 2 || !fraction.all { it.isDigit() }) return null
    val value = fraction.toLong()
    // «20.8» — это 80 копеек, а не 8.
    return units * MINOR_FACTOR + if (fraction.length == 1) value * 10 else value
}

/**
 * Фильтр поля суммы: в тусе без копеек только цифры, в тусе с копейками — один
 * разделитель и не больше двух знаков после него.
 */
fun filterAmountInput(raw: String, fractional: Boolean): String {
    if (!fractional) return raw.filter { it.isDigit() }.take(9)
    val out = StringBuilder()
    var separatorSeen = false
    var afterSeparator = 0
    for (ch in raw) {
        when {
            ch.isDigit() -> {
                if (separatorSeen) {
                    if (afterSeparator == 2) continue
                    afterSeparator++
                }
                out.append(ch)
            }
            (ch == ',' || ch == '.') && !separatorSeen && out.isNotEmpty() -> {
                separatorSeen = true
                out.append(ch)
            }
        }
    }
    return out.take(12).toString()
}

/** Округляет минорные единицы до целых — половина от нуля, как на сервере. */
fun minorToUnitsRounded(minor: Long): Long {
    val q = minor / MINOR_FACTOR
    val r = minor % MINOR_FACTOR
    return when {
        r >= (MINOR_FACTOR + 1) / 2 -> q + 1
        -r >= (MINOR_FACTOR + 1) / 2 -> q - 1
        else -> q
    }
}

/** Готовит сумму для поля ввода: `2080` → «20,80», `2100` → «21». */
fun inputTextFromMinor(minor: Long): String {
    if (minor % MINOR_FACTOR == 0L) return (minor / MINOR_FACTOR).toString()
    val separator = (NumberFormat.getInstance(MoneyFormat.locale) as? DecimalFormat)
        ?.decimalFormatSymbols?.decimalSeparator ?: ','
    return "%d%s%02d".format(minor / MINOR_FACTOR, separator, abs(minor % MINOR_FACTOR))
}

fun money(sum: Long, currency: String): String =
    MoneyFormat.formatter(currency).format(sum)

/** Диапазон сумм для неровного деления: moneyRange(333, 334, "RUB") -> "333–334 ₽". */
fun moneyRange(minSum: Long, maxSum: Long, currency: String): String {
    // Нижняя граница — голое число в формате локали (без символа валюты):
    // символ печатается один раз, у верхней границы
    val plain = NumberFormat.getIntegerInstance(MoneyFormat.locale)
    return plain.format(minSum) + "–" + money(maxSum, currency)
}

/**
 * Складывает суммы ПОВАЛЮТНО: суммы в разных валютах никогда не смешиваются.
 * Результат — без нулевых итогов, по убыванию |суммы| (первая — «основная»
 * для крупного показа), при равенстве — по коду валюты (стабильный порядок).
 */
fun aggregateByCurrency(amounts: List<CurrencySum>): List<CurrencySum> {
    // ⚠️ Копим ТОЧНЫЕ величины: сумма округлений не равна округлению суммы, и
    // два баланса по 0,25 давали бы 0 вместо честных 0,50 — валюта пропадала бы
    // из списка целиком.
    val totals = LinkedHashMap<String, Long>()
    for (amount in amounts) {
        totals[amount.currency] = (totals[amount.currency] ?: 0L) + amount.exactMinor
    }
    return totals.entries
        .filter { it.value != 0L }
        // Сужения больше нет: суммы 64-битные на всём пути. Раньше здесь стояло
        // насыщение, потому что итог в рупиях не помещался в Int и «должен»
        // превращался в «должны вам»
        .map { CurrencySum(currency = it.key, sum = minorToUnitsRounded(it.value), sumMinor = it.value) }
        .sortedWith(compareByDescending<CurrencySum> { abs(it.exactMinor) }.thenBy { it.currency })
}

/**
 * Каноническое правило деления расхода (единое с сервером, docs/API.md):
 * base = sum / count, r = sum % count; получатель с индексом i платит base+1
 * при i < r, иначе base. Сумма долей всегда равна [sum].
 *
 * ВАЖНО: доли существующих операций API отдаёт ГОТОВЫМИ
 * (Operation.recipients[].sum) — позиции пользователя считать из них
 * (Operation.recipientSum/netPosition). Этот хелпер — только для подсказки
 * предпросмотра в форме добавления расхода, пока операция ещё не создана.
 */
fun shares(sum: Long, count: Int): List<Long> {
    if (count <= 0) return emptyList()
    val base = sum / count
    val remainder = sum % count
    // Kotlin усекает деление к нулю, поэтому для отрицательных сумм остаток
    // отрицательный и его надо раздавать вниз — иначе Σ долей != sum
    if (remainder < 0) return List(count) { index -> if (index < -remainder) base - 1 else base }
    return List(count) { index -> if (index < remainder) base + 1 else base }
}
