package com.zagir.splitty.data

import java.util.Locale
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Язык интерфейса уходит на сервер вместе с запросом разбора: поле `questions`
 * в ответе читает человек, и без языка модель задавала уточняющие вопросы
 * по-русски кому угодно. Регион добавляем только там, где он различает язык.
 */
class ParseLangTagTest {
    @Test
    fun `region is added only where it matters`() {
        assertEquals("pt-BR", parseLangTag(Locale.forLanguageTag("pt-BR")))
        assertEquals("pt-BR", parseLangTag(Locale.forLanguageTag("pt-PT")))
        assertEquals("zh-CN", parseLangTag(Locale.forLanguageTag("zh-Hans")))
        assertEquals("ja", parseLangTag(Locale.forLanguageTag("ja-JP")))
        assertEquals("ko", parseLangTag(Locale.forLanguageTag("ko-KR")))
        assertEquals("it", parseLangTag(Locale.forLanguageTag("it-IT")))
        assertEquals("ru", parseLangTag(Locale.forLanguageTag("ru-RU")))
    }
}
