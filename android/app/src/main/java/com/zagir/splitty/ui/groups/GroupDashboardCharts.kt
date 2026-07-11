package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.DailySum
import com.zagir.splitty.core.model.MemberSum
import com.zagir.splitty.core.model.MonthlySum
import java.time.LocalDate
import java.time.YearMonth

// Чистая подготовка данных дашборда «Итоги» v2 (без Compose) — юнит-тесты
// в ui/groups/GroupDashboardChartsTest. Правила дата-виза дашборда:
// личный цвет участника стабилен во всех графиках, палитра НЕ циклится.

/** Размер категориальной палитры участников (Splitty.colors.chartCategorical). */
internal const val MEMBER_COLOR_COUNT = 6

/**
 * Назначение личных цветов: участники сортируются по user.id ПО ВОЗРАСТАНИЮ,
 * позиция — индекс цвета в chartCategorical. Индексы >= [MEMBER_COLOR_COUNT]
 * означают «вне палитры» (рисуются inkSecondary) — цвета не циклятся, поэтому
 * один человек носит один и тот же цвет во всех графиках и при любом составе
 * секций. Зависит только от НАБОРА id — не от порядка списка.
 */
internal fun memberColorIndices(memberIds: Collection<Long>): Map<Long, Int> =
    memberIds.distinct().sorted()
        .mapIndexed { index, id -> id to index }
        .toMap()

/** Строка «Баланса участников»: подпись (уникальная) и net = paid − share. */
internal data class MemberNetBar(val id: Long, val label: String, val net: Int)

/**
 * Нетто-балансы участников: net = заплатил − его доля (>0 — вложил больше
 * своей доли, <0 — «проел» больше, чем платил). Участник из любого из двух
 * списков попадает в результат; сортировка по net по убыванию (при равенстве —
 * по id), тёзки получают « (2)» как в барах.
 */
internal fun memberNetBalances(
    paidByMember: List<MemberSum>,
    shareByMember: List<MemberSum>,
): List<MemberNetBar> {
    val names = HashMap<Long, String>()
    val nets = HashMap<Long, Int>()
    for (member in paidByMember) {
        names.putIfAbsent(member.user.id, member.user.displayName)
        nets[member.user.id] = (nets[member.user.id] ?: 0) + member.sum
    }
    for (member in shareByMember) {
        names.putIfAbsent(member.user.id, member.user.displayName)
        nets[member.user.id] = (nets[member.user.id] ?: 0) - member.sum
    }
    val sorted = nets.entries.sortedWith(
        compareByDescending<Map.Entry<Long, Int>> { it.value }.thenBy { it.key }
    )
    val seen = HashMap<String, Int>()
    return sorted.map { (id, net) ->
        val name = names[id] ?: id.toString()
        val count = (seen[name] ?: 0) + 1
        seen[name] = count
        MemberNetBar(id = id, label = if (count > 1) "$name ($count)" else name, net = net)
    }
}

/**
 * Агрегация трат по дням недели: индекс 0 — понедельник … 6 — воскресенье.
 * Даты «2026-07-05»; нераспознанные строки пропускаются.
 */
internal fun weekdayTotals(byDay: List<DailySum>): List<Int> {
    val totals = IntArray(7)
    for (daily in byDay) {
        val date = runCatching { LocalDate.parse(daily.date) }.getOrNull() ?: continue
        totals[date.dayOfWeek.value - 1] += daily.sum
    }
    return totals.toList()
}

/** Точка «Динамики по месяцам». */
internal data class MonthPoint(val month: YearMonth, val sum: Int)

/**
 * Ровно 6 календарных месяцев по текущий (ascending) — контрактный ряд
 * byMonth, нормализованный клиентом: недостающие месяцы = 0 (старый сервер
 * без byMonth), лишние/нераспознанные элементы игнорируются, дубли месяцев
 * складываются.
 */
internal fun lastSixMonths(
    byMonth: List<MonthlySum>,
    currentMonth: YearMonth = YearMonth.now(),
): List<MonthPoint> {
    val sums = HashMap<YearMonth, Int>()
    for (monthly in byMonth) {
        val month = runCatching { YearMonth.parse(monthly.month) }.getOrNull() ?: continue
        sums[month] = (sums[month] ?: 0) + monthly.sum
    }
    return (5 downTo 0).map { offset ->
        val month = currentMonth.minusMonths(offset.toLong())
        MonthPoint(month = month, sum = sums[month] ?: 0)
    }
}

/**
 * Донат «Кто платил»: больше [maxVisible] сегментов не рисуем — топ-5 против
 * серых «Прочих». Вход — уже подготовленные бары (ненулевые, по убыванию);
 * возвращает видимые сегменты и сумму «Прочих» (0 — сегмент не нужен).
 */
internal fun foldDonutBars(
    bars: List<MemberBar>,
    maxVisible: Int = MEMBER_COLOR_COUNT,
): Pair<List<MemberBar>, Int> =
    if (bars.size <= maxVisible) {
        bars to 0
    } else {
        bars.take(maxVisible - 1) to bars.drop(maxVisible - 1).sumOf { it.sum }
    }

/** Средний чек: totalSpent / operationCount целочисленно; 0 без операций. */
internal fun averageCheck(totalSpent: Int, operationCount: Int): Int =
    if (operationCount > 0) totalSpent / operationCount else 0

/** Процент доли для легенды доната (целочисленное округление). */
internal fun percentOf(sum: Int, total: Int): Int =
    if (total > 0) ((sum * 100L + total / 2) / total).toInt() else 0
