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
private object MoneyFormat {
    private val cache = HashMap<String, NumberFormat>()

    /** Шов для тестов: подменяемая локаль. null — текущая локаль системы. */
    var localeOverride: Locale? = null

    val locale: Locale get() = localeOverride ?: Locale.getDefault()

    @Synchronized
    fun formatter(currency: String): NumberFormat {
        val key = "${locale.toLanguageTag()}|$currency"
        cache[key]?.let { return it }
        val formatter = NumberFormat.getCurrencyInstance(locale)
        // Копеек в продукте нет: суммы всегда целые
        formatter.maximumFractionDigits = 0
        formatter.minimumFractionDigits = 0
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
 * Символ валюты по коду контракта: RUB -> ₽, USD -> $, EUR -> €, IDR -> Rp;
 * незнакомый код показывается как есть ("GBP").
 */
fun currencySymbol(currency: String): String = when (currency) {
    "RUB" -> "₽"
    "USD" -> "$"
    "EUR" -> "€"
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
    val totals = LinkedHashMap<String, Long>()
    for (amount in amounts) {
        totals[amount.currency] = (totals[amount.currency] ?: 0L) + amount.sum
    }
    return totals.entries
        .filter { it.value != 0L }
        // Сужения больше нет: суммы 64-битные на всём пути. Раньше здесь стояло
        // насыщение, потому что итог в рупиях не помещался в Int и «должен»
        // превращался в «должны вам»
        .map { CurrencySum(currency = it.key, sum = it.value) }
        .sortedWith(compareByDescending<CurrencySum> { abs(it.sum) }.thenBy { it.currency })
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
