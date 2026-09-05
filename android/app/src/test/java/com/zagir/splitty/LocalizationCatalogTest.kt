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

    private val locales = listOf("values-ru", "values-de", "values-es", "values-fr")

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
                "в $locale не хватает ${missing.size} строк — экран будет наполовину на английском: " +
                    missing.sorted().take(10),
            )
        }
    }

    @Test
    fun `locales do not carry keys the base does not have`() {
        val base = keys("values")
        for (locale in locales) {
            val extra = keys(locale) - base
            assertTrue(
                extra.isEmpty(),
                "в $locale есть лишние ключи (переименовали и забыли убрать): ${extra.sorted().take(10)}",
            )
        }
    }
}
