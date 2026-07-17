package com.zagir.splitty.ui.components

import com.zagir.splitty.core.model.CurrencySum
import kotlin.math.abs

// Порт ios/Splitty/Core/Components.swift (Glossary + pluralRu).
// Единая точка правды формулировок про долги: раньше «расчёт»/«в расчёте»/
// «Вы рассчитались»/«Все долги погашены» жили вразнобой по экранам — как
// MoneyText для цвета денег, Glossary для текста состояния баланса.

/** Глоссарий состояний баланса — одинаковые слова на всех экранах. */
object Glossary {
    /** Нулевой баланс — короткая подпись строки/карточки. */
    const val SETTLED = "в расчёте"

    /** Нулевой баланс — заголовок/hero-состояние. */
    const val SETTLED_HERO = "Все долги погашены"

    /**
     * Подпись направления долга для одной суммы. Нулевая ветка ОБЯЗАТЕЛЬНА:
     * тернарник «>0 ? вам : вы» при нуле врал (показывал «вы должны» на расчёте).
     */
    fun balanceCaption(sum: Int): String = when {
        sum > 0 -> "вам должны"
        sum < 0 -> "вы должны"
        else -> SETTLED
    }

    /**
     * Подпись для друга/группы с несколькими валютами: суммы разных знаков по
     * валютам — «взаимные долги» (единая «должен вам»/«вы должны» тут врала бы),
     * иначе — по знаку основной валюты с обязательной нулевой веткой.
     * Порт FriendsListView.caption.
     */
    fun balanceCaption(totals: List<CurrencySum>, primary: CurrencySum): String {
        val hasPositive = totals.any { it.sum > 0 }
        val hasNegative = totals.any { it.sum < 0 }
        if (hasPositive && hasNegative) return "взаимные долги"
        if (primary.sum > 0) return "должен(на) вам"
        if (primary.sum < 0) return "вы должны"
        return SETTLED
    }
}

/**
 * Русская форма слова по числу: pluralRu(1, "операция", "операции", "операций").
 * Для чистой логики (не Android-ресурсы): тесты, подписи в компонентах.
 */
fun pluralRu(n: Int, one: String, few: String, many: String): String {
    val mod100 = abs(n) % 100
    if (mod100 in 11..14) return many
    return when (mod100 % 10) {
        1 -> one
        2, 3, 4 -> few
        else -> many
    }
}
