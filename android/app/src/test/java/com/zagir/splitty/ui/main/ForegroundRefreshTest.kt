package com.zagir.splitty.ui.main

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking

/**
 * Возврат из фона и приход пуша обновляют ДАННЫЕ, а не только бейдж.
 *
 * Пуш означает, что на сервере что-то изменилось: добавили расход, погасили
 * долг. Раньше по нему двигался только счётчик на колоколе, а открытый экран
 * продолжал показывать вчерашние суммы, пока человек не потянет список.
 * Сигнал перезагрузки для всех экранов один — версия данных.
 */
class ForegroundRefreshTest {

    private class FakeTokenCipher : TokenCipher {
        override fun encrypt(plainText: String): String = "enc:$plainText"
        override fun decrypt(cipherText: String): String? =
            cipherText.removePrefix("enc:").takeIf { cipherText.startsWith("enc:") }
        override fun clearKey() = Unit
    }

    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var session: SessionStore

    @BeforeTest
    fun setUp() {
        dir = Files.createTempDirectory("foreground-refresh").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        val dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
        session = SessionStore(dataStore, FakeTokenCipher(), scope)
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        dir.deleteRecursively()
    }

    @Test
    fun `data version moves so every screen reloads`() = runBlocking {
        val before = session.dataVersion.value

        session.noteDataChanged()

        assertTrue(
            session.dataVersion.value > before,
            "версия данных не сдвинулась — экраны останутся с прежними суммами",
        )
    }
}
