package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.DailySum
import com.zagir.splitty.core.model.MemberSum
import com.zagir.splitty.core.model.MonthlySum
import com.zagir.splitty.core.model.User
import java.time.YearMonth
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Подготовка данных дашборда «Итоги» v2 (GroupDashboardCharts.kt):
 * стабильные личные цвета по user.id, net-балансы, агрегация по дням недели,
 * нормализация byMonth, донат топ-5 + «Прочие», средний чек.
 */
class GroupDashboardChartsTest {

    private fun user(id: Long, name: String = "U$id") =
        User(id = id, username = null, displayName = name)

    private fun member(id: Long, sum: Long, name: String = "U$id") =
        MemberSum(user = user(id, name), sum = sum)

    // MARK: Назначение цветов

    @Test
    fun `color indices follow ascending user id`() {
        val indices = memberColorIndices(listOf(500L, 7L, 42L))
        assertEquals(0, indices[7L])
        assertEquals(1, indices[42L])
        assertEquals(2, indices[500L])
    }

    @Test
    fun `color indices are stable across input order and duplicates`() {
        val a = memberColorIndices(listOf(3L, 1L, 2L))
        val b = memberColorIndices(listOf(2L, 2L, 1L, 3L, 1L))
        assertEquals(a, b)
        // Один человек — один цвет: назначение зависит только от набора id.
        assertEquals(0, a[1L])
        assertEquals(1, a[2L])
        assertEquals(2, a[3L])
    }

    @Test
    fun `seventh member and beyond fall out of palette without cycling`() {
        val ids = (1L..8L).toList()
        val indices = memberColorIndices(ids)
        // Первые шесть — в палитре.
        assertTrue((1L..6L).all { indices[it]!! < MEMBER_COLOR_COUNT })
        // Седьмой и восьмой — вне палитры (рисуются inkSecondary), НЕ циклим:
        // getOrNull по chartCategorical (6 цветов) для них вернёт null.
        assertEquals(6, indices[7L])
        assertEquals(7, indices[8L])
        val palette = List(MEMBER_COLOR_COUNT) { "цвет$it" }
        assertNull(palette.getOrNull(indices[7L]!!))
        assertNull(palette.getOrNull(indices[8L]!!))
    }

    // MARK: Net-балансы

    @Test
    fun `net balances are paid minus share sorted descending`() {
        val paid = listOf(member(1, 4000), member(2, 1000), member(3, 1000))
        val share = listOf(member(1, 2000), member(2, 2000), member(3, 2000))
        val nets = memberNetBalances(paid, share)
        assertEquals(listOf(1L, 2L, 3L), nets.map { it.id })
        assertEquals(listOf(2000L, -1000L, -1000L), nets.map { it.net })
        // Ничья по net (участники 2 и 3) — стабильный порядок по id.
        assertEquals("U2", nets[1].label)
    }

    @Test
    fun `net balances include members from either list and dedupe names`() {
        // Участник 2 только платил, участник 3 только потреблял; 1 и 3 — тёзки.
        val paid = listOf(member(1, 500, "Алмаз"), member(2, 300, "Загир"))
        val share = listOf(member(1, 100, "Алмаз"), member(3, 700, "Алмаз"))
        val nets = memberNetBalances(paid, share)
        assertEquals(3, nets.size)
        assertEquals(400, nets.first { it.id == 1L }.net)
        assertEquals(300, nets.first { it.id == 2L }.net)
        assertEquals(-700, nets.first { it.id == 3L }.net)
        // Тёзки различимы: второй «Алмаз» по сортировке получает « (2)».
        assertEquals(
            listOf("Алмаз", "Алмаз (2)"),
            nets.filter { it.id != 2L }.map { it.label },
        )
    }

    // MARK: По дням недели

    @Test
    fun `weekday totals aggregate monday to sunday`() {
        // 2026-01-05 — понедельник, 2026-01-06 — вторник, 2026-01-11 — воскресенье.
        val totals = weekdayTotals(
            listOf(
                DailySum("2026-01-05", 100),
                DailySum("2026-01-12", 40), // тоже понедельник — складывается
                DailySum("2026-01-06", 7),
                DailySum("2026-01-11", 300),
                DailySum("не дата", 999_999), // нераспознанное — пропускается
            )
        )
        assertEquals(7, totals.size)
        assertEquals(140L, totals[0]) // пн
        assertEquals(7L, totals[1]) // вт
        assertEquals(300L, totals[6]) // вс
        assertEquals(listOf(0L, 0L, 0L, 0L), totals.subList(2, 6)) // ср–сб пустые
    }

    // MARK: Динамика по месяцам

    
    
    // MARK: Донат «Кто платил»

    @Test
    fun `donut keeps six or fewer bars as is`() {
        val bars = (1L..6L).map { MemberBar(id = it, label = "U$it", sum = 100 * it) }
        val (visible, othersSum) = foldDonutBars(bars)
        assertEquals(bars, visible)
        assertEquals(0L, othersSum)
    }

    @Test
    fun `donut folds more than six bars into top five plus others`() {
        // Уже по убыванию (как из preparedMemberBars): 800, 700, … 100.
        val bars = (8 downTo 1).map { MemberBar(id = it.toLong(), label = "U$it", sum = it * 100L) }
        val (visible, othersSum) = foldDonutBars(bars)
        assertEquals(5, visible.size)
        assertEquals(listOf(800L, 700L, 600L, 500L, 400L), visible.map { it.sum })
        assertEquals(300L + 200L + 100L, othersSum)
    }

    // MARK: Плитки

    @Test
    fun `average check is integer division and zero without operations`() {
        assertEquals(0, averageCheck(totalSpent = 0, operationCount = 0))
        assertEquals(0, averageCheck(totalSpent = 9400, operationCount = 0))
        assertEquals(783, averageCheck(totalSpent = 9400, operationCount = 12))
    }

    @Test
    fun `percent of donut segment rounds to whole percent`() {
        assertEquals(48, percentOf(4500, 9400))
        assertEquals(100, percentOf(9400, 9400))
        assertEquals(0, percentOf(1, 0))
    }
}
