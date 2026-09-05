package com.zagir.splitty

import java.io.File
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * В исходниках нет русского текста, который уедет на экран мимо ресурсов.
 *
 * Lint такое не ловит: `MissingTranslation` смотрит на strings.xml, а
 * `HardcodedText` — только на XML-разметку, которой в Compose нет. Литерал в
 * Composable или ViewModel выглядит работающим на русском устройстве и остаётся
 * русским на всех остальных — заметить это можно лишь глазами, переключив язык.
 *
 * Проверка грубая нарочно: любой кириллический литерал — повод либо завести
 * ресурс, либо внести строку в [allowed] с объяснением, почему она не текст для
 * человека. Список исключений — это и есть список известного долга.
 */
class HardcodedTextTest {

    /** Строки, которые человек не видит: диагностика и коды. */
    private val allowed = mapOf(
        "биллинг недоступен" to "причина отказа биллинга — уходит в аналитику, не на экран",
        "у продукта нет предложения" to "то же: PurchaseOutcome.Failed читает только аналитика",
        "нечитаемый ответ Telegram" to "текст исключения отбрасывается, экран показывает свой ресурс",
        "FriendDetail требует nav-аргумент userId" to "сообщение require() для разработчика",
        "Неподдерживаемая офлайн-операция" to
            "диагностика ApiException; человеку humanErrorText отдаёт ресурс по коду unsupported",
        "сум" to "русское написание сума: ветка ru в currencySymbol(), рядом лежат Sum и sum",
    )

    private val logCall = Regex("""Log\.[dwiev]\(|TAG,""")
    private val literal = Regex(""""([^"\\\n]*[А-Яа-яЁё][^"\\\n]*)"""")

    @Test
    fun `no cyrillic literals outside resources`() {
        val root = File("src/main/java/com/zagir/splitty")
        assertTrue(root.isDirectory, "нет исходников: ${root.absolutePath}")

        val found = mutableListOf<String>()
        root.walkTopDown().filter { it.extension == "kt" }.forEach { file ->
            file.readLines().forEachIndexed { index, line ->
                val trimmed = line.trimStart()
                if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*")) return@forEachIndexed
                if (logCall.containsMatchIn(line)) return@forEachIndexed
                for (match in literal.findAll(line)) {
                    val text = match.groupValues[1]
                    if (text in allowed) continue
                    found += "${file.name}:${index + 1}: «$text»"
                }
            }
        }
        assertTrue(
            found.isEmpty(),
            "русский текст в коде — на других языках он останется русским; " +
                "заведи строку в res/values и переведи её во всех локалях:\n" +
                found.take(15).joinToString("\n"),
        )
    }

    /** Список исключений не пухнет молча: устаревшая запись обязана бросаться в глаза. */
    @Test
    fun `allow list has no stale entries`() {
        val sources = File("src/main/java/com/zagir/splitty").walkTopDown()
            .filter { it.extension == "kt" }
            .joinToString("\n") { it.readText() }
        val stale = allowed.keys.filterNot { sources.contains("\"$it\"") }
        assertTrue(stale.isEmpty(), "исключения больше не встречаются в коде, убери их: $stale")
    }
}
