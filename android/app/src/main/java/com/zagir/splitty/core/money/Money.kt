package com.zagir.splitty.core.money

import com.zagir.splitty.core.model.CurrencySum
import kotlin.math.abs

/**
 * Денежная арифметика и форматирование — порт канонических правил проекта
 * (ios/Splitty/Core/Money.swift, docs/API.md). Все суммы — ЦЕЛЫЕ (копеек нет),
 * float в денежных расчётах запрещён.
 */

/** Цифры суммы с пробелами-разделителями тысяч, без знака и валюты: 1234567 -> "1 234 567". */
private fun thousandsGrouped(sum: Int): String {
    val digits = abs(sum.toLong()).toString()
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
fun money(sum: Int, currency: String): String =
    (if (sum < 0) "-" else "") + thousandsGrouped(sum) + " " + currencySymbol(currency)

/** Диапазон сумм для неровного деления: moneyRange(333, 334, "RUB") -> "333–334 ₽". */
fun moneyRange(minSum: Int, maxSum: Int, currency: String): String =
    thousandsGrouped(minSum) + "–" + money(maxSum, currency)

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
        .map { CurrencySum(currency = it.key, sum = it.value.toInt()) }
        .sortedWith(
            compareByDescending<CurrencySum> { abs(it.sum) }.thenBy { it.currency }
        )
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
fun shares(sum: Int, count: Int): List<Int> {
    if (count <= 0) return emptyList()
    val base = sum / count
    val remainder = sum % count
    return List(count) { index -> if (index < remainder) base + 1 else base }
}
