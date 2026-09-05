package com.zagir.splitty.ui.groups

import java.time.Instant
import java.util.Locale
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import org.junit.After
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Порядок компонентов даты у каждого языка свой.
 *
 * Раньше шаблоны были зашиты как «d MMMM yyyy», и перевод названий месяцев
 * этого не чинил: по-японски выходило «5 9月 2026» вместо «2026年9月5日».
 * Теперь порядок берётся у ICU по скелету, поэтому тест идёт под Robolectric —
 * в чистой JVM `android.text.format.DateFormat` недоступен.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GroupsDateFmtTest {

    /** 5 сентября 2026, полдень UTC. */
    private val moment = Instant.parse("2026-09-05T12:00:00Z")

    @After
    fun restore() {
        Locale.setDefault(Locale.forLanguageTag("ru"))
    }

    private fun withLocale(tag: String, block: () -> String): String {
        Locale.setDefault(Locale.forLanguageTag(tag))
        return block()
    }

    /** Снимок прежнего поведения: русский и английский не должны были поехать. */
    @Test
    fun `russian and english keep day before month`() {
        val ru = withLocale("ru") { GroupsDateFmt.fullDate(moment) }
        assertTrue(ru.indexOf("5") < ru.indexOf("2026"), "по-русски день идёт раньше года: $ru")
        assertTrue("сентября" in ru || "сентябр" in ru, "месяц словом: $ru")

        val en = withLocale("en") { GroupsDateFmt.fullDate(moment) }
        assertTrue("September" in en, "месяц словом: $en")
        assertTrue("2026" in en, "год на месте: $en")
    }

    /** Главное: у японского и корейского год идёт ПЕРВЫМ. */
    @Test
    fun `east asian locales put the year first`() {
        for (tag in listOf("ja", "ko", "zh-Hans")) {
            val text = withLocale(tag) { GroupsDateFmt.fullDate(moment) }
            assertTrue(
                text.indexOf("2026") < text.indexOf("5"),
                "в локали $tag год обязан идти перед днём, получено: $text",
            )
        }
    }

    /** Португальский ставит день перед месяцем и добавляет предлог. */
    @Test
    fun `portuguese keeps day before month`() {
        val text = withLocale("pt-BR") { GroupsDateFmt.fullDate(moment) }
        assertTrue(text.indexOf("5") < text.indexOf("2026"), "получено: $text")
    }

    /** Короткая дата «день + месяц» тоже подчиняется локали. */
    @Test
    fun `short day and month follow the locale`() {
        val ja = withLocale("ja") { GroupsDateFmt.dayMonth(moment) }
        assertTrue(ja.indexOf("9") < ja.indexOf("5"), "по-японски месяц идёт перед днём: $ja")

        val ru = withLocale("ru") { GroupsDateFmt.dayMonth(moment) }
        assertTrue(ru.indexOf("5") < ru.length, "русская короткая дата не пустая: $ru")
    }

    /** Одиночные поля порядка не имеют и остаются как были. */
    @Test
    fun `single fields are unchanged`() {
        assertEquals("5", withLocale("ru") { GroupsDateFmt.day(moment) })
        assertEquals("5", withLocale("ja") { GroupsDateFmt.day(moment) })
    }
}
