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
    fun `release forbids cleartext globally and grants no exceptions`() {
        // Бэкенд под TLS (https://splitor.zagirnur.dev) — послаблений быть не
        // должно ни глобально, ни точечно. Разрешающий domain-config для
        // дев-IP убран; тест — предохранитель от его возвращения.
        val xml = resFile("src/release/res/xml/network_security_config.xml").readText()
        assertTrue(
            xml.contains("<base-config cleartextTrafficPermitted=\"false\""),
            "release base-config обязан глобально запрещать cleartext",
        )
        // Комментарии в файле упоминают cleartext словом — ищем именно
        // включающий атрибут в разметке, а не подстроку в тексте.
        val enablesCleartext = Regex("cleartextTrafficPermitted\\s*=\\s*\"true\"")
        assertFalse(
            enablesCleartext.containsMatchIn(xml),
            "release-конфиг не должен нигде включать cleartext",
        )
        assertFalse(
            xml.contains("<domain"),
            "release-конфиг не должен содержать domain-исключений (в т.ч. для дев-IP)",
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
        // datastore — КАТАЛОГОМ, а не файлом session.preferences_pb: DataStore
        // пишет через .tmp, и точечное исключение выпускало шифротекст токена
        // в бэкап во временном файле.
        val excludedPaths = listOf(
            "datastore/",
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
    fun `default base url is https`() {
        // Дефолтный адрес — всегда https, без исключений: release-вариант
        // cleartext вообще не пропустит, и http-дефолт означал бы неработающее
        // приложение (плюс Bearer-токен открытым текстом в debug).
        val src = resFile(
            "src/main/java/com/zagir/splitty/core/session/SessionStore.kt",
        ).readText()
        val url = Regex("DEFAULT_BASE_URL[\\s\\S]*?get\\(\\) = \"([^\"]+)\"")
            .find(src)?.groupValues?.get(1)
        assertNotNull(url, "DEFAULT_BASE_URL должен быть строковым литералом")
        assertTrue(
            url.startsWith("https://"),
            "DEFAULT_BASE_URL обязан быть https-адресом, а не $url",
        )
        assertFalse(
            url.contains("138.124.18.189"),
            "DEFAULT_BASE_URL не должен указывать на дев-IP по голому HTTP",
        )
    }
}
