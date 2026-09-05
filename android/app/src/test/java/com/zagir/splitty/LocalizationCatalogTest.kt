package com.zagir.splitty

import java.io.File
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * Набор ключей совпадает во всех локалях.
 *
 * Пропущенный перевод не ломает сборку — он молча падает обратно на английский,
 * и человек видит смесь языков на одном экране. Заметить это без теста можно
 * только глазами, на живом устройстве, с переключённым языком.
 *
 * Исключения — ключи, которые НАМЕРЕННО одни на все языки: имя приложения,
 * названия языков в списке выбора, технические подписи. Их отмечает
 * `translatable="false"` в базовом файле, и список читается ОТТУДА, а не из
 * копии в тесте: копия однажды разошлась с разметкой, и `lintRelease` падал на
 * шести строках, пока этот набор оставался зелёным.
 */
class LocalizationCatalogTest {

    private val locales = listOf(
        "values-ru", "values-de", "values-es", "values-fr",
        "values-ja", "values-zh-rCN", "values-ko", "values-pt-rBR", "values-it",
    )

    /** Полный список уходит в файл: обрезание до десяти строк делало вывод
     *  бесполезным ровно тогда, когда работы много. */
    private fun report(items: Collection<String>, label: String): String {
        val path = File(System.getProperty("java.io.tmpdir"), "splitty-i18n-$label.txt")
        path.writeText(items.sorted().joinToString("\n"))
        return "всего ${items.size}, полный список: $path\n" +
            items.sorted().take(40).joinToString("\n")
    }

    /** Подстановки в переводе: набор обязан совпадать с базой, иначе строка
     *  упадёт в runtime или потеряет число. Сравниваем МУЛЬТИМНОЖЕСТВО. */
    private fun placeholders(value: String): List<String> =
        Regex("""%(\d+\$)?[sdf]""").findAll(value).map { it.value }.sorted().toList()

    private fun values(folder: String): Map<String, String> =
        Regex("""<string name="([^"]+)"[^>]*>(.*?)</string>""", RegexOption.DOT_MATCHES_ALL)
            .findAll(text(folder))
            .associate { it.groupValues[1] to it.groupValues[2] }

    private fun text(folder: String): String {
        val file = File("src/main/res/$folder/strings.xml")
        assertTrue(file.exists(), "нет файла строк: ${file.path}")
        return file.readText()
    }

    private fun keys(folder: String): Set<String> =
        Regex("""name="([^"]+)"""").findAll(text(folder))
            .map { it.groupValues[1] }
            .toSet()

    /** Ключи, помеченные в базовом файле как непереводимые. */
    private fun sharedKeys(): Set<String> =
        Regex("""name="([^"]+)"\s+translatable="false"""").findAll(text("values"))
            .map { it.groupValues[1] }
            .toSet()

    @Test
    fun `every locale carries the same keys`() {
        val base = keys("values") - sharedKeys()
        for (locale in locales) {
            val missing = base - keys(locale)
            assertTrue(
                missing.isEmpty(),
                "в $locale не хватает строк — экран будет наполовину на английском: " +
                    report(missing, "missing-$locale"),
            )
        }
    }

    /**
     * Подстановки перевода совпадают с базой.
     *
     * Совпадения имён ключей мало: файл, где 677 строк скопированы с
     * английского и потеряли `%1$s`, проходил как валидный, а в runtime строка
     * теряет число или падает. Пустые значения ловим здесь же — их набор имён
     * тоже не отличает.
     */
    @Test
    fun `translations keep base placeholders`() {
        val base = values("values")
        val problems = mutableListOf<String>()
        for (locale in locales) {
            for ((key, value) in values(locale)) {
                val reference = base[key] ?: continue
                if (value.isBlank()) {
                    problems += "$locale/$key: пустое значение"
                    continue
                }
                if (placeholders(value) != placeholders(reference)) {
                    problems += "$locale/$key: подстановки ${placeholders(value)} вместо ${placeholders(reference)}"
                }
            }
        }
        assertTrue(problems.isEmpty(), report(problems, "placeholders"))
    }

    @Test
    fun `locales do not carry keys the base does not have`() {
        val base = keys("values")
        for (locale in locales) {
            val extra = keys(locale) - base
            assertTrue(
                extra.isEmpty(),
                "в $locale есть лишние ключи (переименовали и забыли убрать): " +
                    report(extra, "extra-$locale"),
            )
        }
    }
}
