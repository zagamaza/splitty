package com.zagir.splitty.core.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import com.zagir.splitty.IO_WAIT_MS
import com.zagir.splitty.TEARDOWN_WAIT_MS
import com.zagir.splitty.core.model.Me
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout

/**
 * SessionStore: миграция plaintext-токена в шифротекст без разлогина, чистая
 * установка и logout. Реальный Keystore в JVM недоступен, поэтому шифрование
 * подменяет [FakeTokenCipher] — проверяется именно логика dual-read/миграции,
 * а не крипто-примитив.
 */
class SessionStoreTest {

    /** Фейк: encrypt = префикс `enc:`, decrypt снимает его; чужой формат → null. */
    private class FakeTokenCipher : TokenCipher {
        var keyCleared = false
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() { keyCleared = true }
    }

    private val tokenKey = stringPreferencesKey("token")
    private val tokenEncKey = stringPreferencesKey("token_enc")

    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>

    private val me = Me(id = 1L, username = "zagir", displayName = "Загир")

    @BeforeTest
    fun setUp() {
        dir = Files.createTempDirectory("session-test").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
    }

    @AfterTest
    fun tearDown() {
        runBlocking { withTimeout(TEARDOWN_WAIT_MS) { scope.coroutineContext[Job]!!.cancelAndJoin() } }
        dir.deleteRecursively()
    }

    /**
     * Тело теста с пределом: `first { … }` на DataStore без него при потерянном
     * обновлении ждёт вечно и держит весь прогон — так однажды завис тест
     * миграции токена.
     */
    private fun bounded(block: suspend CoroutineScope.() -> Unit) = runBlocking {
        withTimeout(IO_WAIT_MS) { block() }
    }

    @Test
    fun `clean install has no token`() = bounded {
        val store = SessionStore(dataStore, FakeTokenCipher(), scope)
        assertNull(store.state.filterNotNull().first().token)
    }

    @Test
    fun `plain token migrates to encrypted without logout`() = bounded {
        // Формат ДО этой задачи: plaintext-токен под ключом "token".
        dataStore.edit { it[tokenKey] = "jwt-123" }

        val cipher = FakeTokenCipher()
        val store = SessionStore(dataStore, cipher, scope)

        // Токен читается сразу (dual-read plain), тестер не разлогинен.
        val token = store.state.filterNotNull().first { it.token != null }.token
        assertEquals("jwt-123", token)

        // Миграция завершилась: шифротекст записан, plain удалён.
        val prefs = dataStore.data.first { it[tokenEncKey] != null && it[tokenKey] == null }
        assertEquals("enc:jwt-123", prefs[tokenEncKey])

        // И после миграции токен по-прежнему тот же.
        assertEquals("jwt-123", store.state.filterNotNull().first().token)
    }

    @Test
    fun `signIn stores encrypted token not plain`() = bounded {
        val store = SessionStore(dataStore, FakeTokenCipher(), scope)
        store.signIn("jwt-xyz", me)

        val prefs = dataStore.data.first { it[tokenEncKey] != null }
        assertEquals("enc:jwt-xyz", prefs[tokenEncKey])
        assertNull(prefs[tokenKey])
        assertEquals("jwt-xyz", store.state.filterNotNull().first { it.token != null }.token)
    }

    @Test
    fun `logout clears both token formats and keystore key`() = bounded {
        val cipher = FakeTokenCipher()
        val store = SessionStore(dataStore, cipher, scope)
        store.signIn("jwt", me)
        store.state.filterNotNull().first { it.token != null }

        store.logout()

        val prefs = dataStore.data.first { it[tokenEncKey] == null }
        assertNull(prefs[tokenEncKey])
        assertNull(prefs[tokenKey])
        assertTrue(cipher.keyCleared)
        assertNull(store.state.filterNotNull().first { it.token == null }.token)
    }

    @Test
    fun `unreadable ciphertext yields no token`() = bounded {
        // Ключ Keystore пропал (сброс/перенос устройства): шифротекст не читается.
        dataStore.edit { it[tokenEncKey] = "garbage-not-enc" }

        val store = SessionStore(dataStore, FakeTokenCipher(), scope)
        assertNull(store.state.filterNotNull().first().token)
    }
}
