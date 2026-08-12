package com.zagir.splitty.core.money

import com.zagir.splitty.core.model.CurrencySum
import kotlin.math.abs

/**
 * Денежная арифметика и форматирование — порт канонических правил проекта
 * (ios/Splitty/Core/Money.swift, docs/API.md). Все суммы — ЦЕЛЫЕ (копеек нет),
 * float в денежных расчётах запрещён.
 */

/** Цифры суммы с пробелами-разделителями тысяч, без знака и валюты: 1234567 -> "1 234 567". */
private fun thousandsGrouped(sum: Long): String {
    // abs на Long без сужения: у Long.MIN_VALUE модуля нет, но такие суммы не
    // существуют — переполнение здесь означало бы битые данные, а не большую
    // покупку
    val digits = abs(sum).toString()
    val sb = StringBuilder()
    for ((index, char) in digits.reversed().withIndex()) {
        if (index > 0 && index % 3 == 0) sb.append(' ')
        sb.append(char)
    }
    return sb.reverse().toString()
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
    "UZS" -> "сум"
    else -> currency
}

/**
 * Форматирует сумму в валюте: money(1234567, "USD") -> "1 234 567 $".
 * Разделитель тысяч — обычный пробел, символ валюты ПОСЛЕ суммы, суммы целые.
 */
fun money(sum: Long, currency: String): String =
    (if (sum < 0) "-" else "") + thousandsGrouped(sum) + " " + currencySymbol(currency)

/** Диапазон сумм для неровного деления: moneyRange(333, 334, "RUB") -> "333–334 ₽". */
fun moneyRange(minSum: Long, maxSum: Long, currency: String): String =
    (if (minSum < 0) "-" else "") + thousandsGrouped(minSum) + "–" + money(maxSum, currency)

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
