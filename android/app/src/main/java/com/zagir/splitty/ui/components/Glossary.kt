package com.zagir.splitty.ui.components

import androidx.annotation.StringRes
import com.zagir.splitty.R
import com.zagir.splitty.core.model.CurrencySum
import kotlin.math.abs

// Порт ios/Splitty/Core/Components.swift (Glossary + pluralRu).
// Единая точка правды формулировок про долги: раньше «расчёт»/«в расчёте»/
// «Вы рассчитались»/«Все долги погашены» жили вразнобой по экранам — как
// MoneyText для цвета денег, Glossary для текста состояния баланса.
//
// Возвращаются ИДЕНТИФИКАТОРЫ ресурсов, а не готовые строки: сами тексты
// переведены на пять языков, а чистая логика выбора остаётся тестируемой
// без Android-контекста.

/** Глоссарий состояний баланса — одинаковые слова на всех экранах. */
object Glossary {
    /** Нулевой баланс — короткая подпись строки/карточки. */
    @StringRes
    val SETTLED = R.string.friends_settlement

    /** Нулевой баланс — заголовок/hero-состояние. */
    @StringRes
    val SETTLED_HERO = R.string.friends_all_settled

    /**
     * Подпись направления долга для одной суммы. Нулевая ветка ОБЯЗАТЕЛЬНА:
     * тернарник «>0 ? вам : вы» при нуле врал (показывал «вы должны» на расчёте).
     */
    @StringRes
    fun balanceCaption(sum: Long): Int = when {
        sum > 0 -> R.string.groups_row_owed
        sum < 0 -> R.string.groups_row_owes
        else -> SETTLED
    }

    /**
     * Подпись для друга/группы с несколькими валютами: суммы разных знаков по
     * валютам — «взаимные долги» (единая «должен вам»/«вы должны» тут врала бы),
     * иначе — по знаку основной валюты с обязательной нулевой веткой.
     * Порт FriendsListView.caption.
     */
    @StringRes
    fun balanceCaption(totals: List<CurrencySum>, primary: CurrencySum): Int {
        val hasPositive = totals.any { it.sum > 0 }
        val hasNegative = totals.any { it.sum < 0 }
        if (hasPositive && hasNegative) return R.string.glossary_mutual_debts
        if (primary.sum > 0) return R.string.friends_owes_you_short
        if (primary.sum < 0) return R.string.groups_row_owes
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
