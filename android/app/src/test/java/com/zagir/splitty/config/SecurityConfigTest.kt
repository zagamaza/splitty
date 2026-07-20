package com.zagir.splitty.config

import java.io.File
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

/**
 * Проверяет платформенные конфиги как ФАЙЛЫ (не через Android runtime):
 * release-вариант не должен допускать cleartext, а backup-правила обязаны
 * исключать секреты. Это дешёвый unit-предохранитель — Task 2c checklist.
 * Рабочая директория JVM-тестов Gradle — корень модуля (app/); подстраховка
 * ищет файлы и относительно app/ на случай другого cwd.
 */
class SecurityConfigTest {

    private fun resFile(rel: String): File {
        val direct = File(rel)
        if (direct.exists()) return direct
        val underApp = File("app/$rel")
        if (underApp.exists()) return underApp
        error("Не найден конфиг-файл: $rel (cwd=${File(".").absolutePath})")
    }

    @Test
    fun `release network config forbids cleartext`() {
        val xml = resFile("src/release/res/xml/network_security_config.xml").readText()
        assertFalse(
            xml.contains("cleartextTrafficPermitted=\"true\""),
            "release-конфиг НЕ должен разрешать cleartext",
        )
        assertTrue(
            xml.contains("cleartextTrafficPermitted=\"false\""),
            "release-конфиг должен явно запрещать cleartext",
        )
    }

    @Test
    fun `debug network config permits cleartext for the http dev server`() {
        val xml = resFile("src/debug/res/xml/network_security_config.xml").readText()
        assertTrue(
            xml.contains("cleartextTrafficPermitted=\"true\""),
            "debug-конфиг должен разрешать cleartext (дев-сервер по HTTP)",
        )
    }

    @Test
    fun `backup rules exclude token store, outbox and api cache by exact path`() {
        // Список расширяется вместе с каждым новым потребителем filesDir: квота
        // автобэкапа ~25 МБ, и один забытый каталог (модель Vosk — 40-50 МБ)
        // молча отключает бэкап ВСЕГО остального.
        val excludedPaths = listOf(
            "datastore/session.preferences_pb",
            "outbox.json",
            "cache-api/",
            "vosk-model-ru/",
        )
        for (rel in listOf(
            "src/main/res/xml/data_extraction_rules.xml",
            "src/main/res/xml/backup_rules.xml",
        )) {
            val xml = resFile(rel).readText()
            for (path in excludedPaths) {
                assertTrue(
                    xml.contains("path=\"$path\""),
                    "$rel обязан исключать $path из бэкапа",
                )
            }
        }
    }

    @Test
    fun `no global network config leaks cleartext into all variants`() {
        // Глобальный src/main конфиг удалён — cleartext гейтится ТОЛЬКО по варианту.
        val leaked = File("src/main/res/xml/network_security_config.xml").exists() ||
            File("app/src/main/res/xml/network_security_config.xml").exists()
        assertFalse(leaked, "src/main network_security_config.xml должен быть удалён (variant-specific)")
    }

    @Test
    fun `default base url is https in a real production host`() {
        // Плейсхолдер прод-домена обязан быть HTTPS; боевой HTTP-IP — только debug.
        val src = resFile(
            "src/main/java/com/zagir/splitty/core/session/SessionStore.kt",
        ).readText()
        val releaseUrl = Regex("else \"(https://[^\"]+)\"").find(src)?.groupValues?.get(1)
        assertNotNull(releaseUrl, "release DEFAULT_BASE_URL должен быть https-строкой")
        assertTrue(releaseUrl.startsWith("https://"), "release-адрес обязан быть HTTPS")
    }
}
