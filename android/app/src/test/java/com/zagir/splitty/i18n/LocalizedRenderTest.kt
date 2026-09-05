package com.zagir.splitty.i18n

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.SemanticsConfiguration
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsNodeInteractionCollection
import com.zagir.splitty.R
import com.zagir.splitty.core.money.money
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Экраны отрисовываются на каждом языке, и на них не остаётся «%d».
 *
 * Каталожные тесты сверяют НАБОРЫ строк, а этот — то, что получается на экране.
 * Ломается это иначе: перевод с потерянной подстановкой или множественная форма,
 * которой у языка нет, до экрана доезжают буквальным «%d» или падением
 * форматирования. Ни один из наборов ключей такого не видит, а человек видит
 * сразу.
 *
 * Количества подобраны по границам CLDR: 1 (one), 2 (few у русского), 5 (many),
 * 11 и 21 — там русский снова меняет форму, а японский с корейским не меняют
 * никогда.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LocalizedRenderTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val counts = listOf(0, 1, 2, 5, 11, 21, 100)

    /** Рисует всё, что собирается из ресурсов с подстановками, и отдаёт тексты. */
    private fun render(locale: String): List<String> {
        RuntimeEnvironment.setQualifiers("+$locale")
        composeRule.setContent {
            Column {
                for (n in counts) {
                    Text(pluralStringResource(R.plurals.groups_member_count, n, n), Modifier.testTag("i18n"))
                    Text(pluralStringResource(R.plurals.group_hero_pending_ops, n, n), Modifier.testTag("i18n"))
                    Text(pluralStringResource(R.plurals.profile_logout_outbox_confirm, n, n), Modifier.testTag("i18n"))
                    Text(pluralStringResource(R.plurals.time_years_ago, n, n), Modifier.testTag("i18n"))
                    Text(stringResource(R.string.time_minutes_ago, n), Modifier.testTag("i18n"))
                    Text(stringResource(R.string.profile_id, n), Modifier.testTag("i18n"))
                }
                // Деньги идут мимо ресурсов, но на экране стоят рядом.
                for (currency in listOf("RUB", "USD", "EUR", "JPY", "CNY", "KRW", "BRL")) {
                    Text(money(12_345, currency), Modifier.testTag("i18n"))
                }
            }
        }
        return composeRule.onAllNodesWithTag("i18n").texts()
    }

    private fun SemanticsNodeInteractionCollection.texts(): List<String> =
        fetchSemanticsNodes().flatMap { node ->
            node.config.getOrNull(SemanticsProperties.Text).orEmpty().map { it.toString() }
        }

    /**
     * Слово, которое обязано появиться на экране этого языка. Без такой метки
     * тест был бы бесполезен молча: сломайся переключение локали в Robolectric,
     * все десять прогонов рисовали бы английский и оставались зелёными.
     */
    private val marker = mapOf(
        "en" to "member", "ru" to "участник", "de" to "Mitglied", "es" to "miembro",
        "fr" to "membre", "it" to "partecipant", "ja" to "人", "ko" to "명",
        "pt-rBR" to "participante", "zh-rCN" to "不含",
    )

    private fun check(locale: String) {
        val texts = render(locale)
        assertTrue(texts.isNotEmpty(), "$locale: не отрисовалось ни одной строки")
        val want = marker.getValue(locale)
        assertTrue(
            texts.any { it.contains(want) },
            "$locale: на экране нет «$want» — язык не переключился, отрисовался чужой:\n"
                + texts.take(5).joinToString("\n"),
        )
        val broken = texts.filter { it.isBlank() || it.contains("%") }
        assertFalse(
            broken.isNotEmpty(),
            "$locale: подстановка не сработала — на экране остаётся сырой шаблон:\n"
                + broken.joinToString("\n"),
        )
    }

    @Test fun `en renders`() = check("en")
    @Test fun `ru renders`() = check("ru")
    @Test fun `de renders`() = check("de")
    @Test fun `es renders`() = check("es")
    @Test fun `fr renders`() = check("fr")
    @Test fun `it renders`() = check("it")
    @Test fun `ja renders`() = check("ja")
    @Test fun `ko renders`() = check("ko")
    @Test fun `pt-BR renders`() = check("pt-rBR")
    @Test fun `zh-Hans renders`() = check("zh-rCN")
}
