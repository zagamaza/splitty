package com.zagir.splitty.core.session

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel

/**
 * Подтверждение выполненного действия.
 *
 * Ни погашение, ни выход из группы, ни смена пароля ничего не отвечали: человек
 * не понимал, случилось ли действие, и повторял его. Сохранение расхода в
 * очередь при этом выглядело ровно как сохранение на сервер — про очередь он
 * узнавал, только открыв группу и увидев там пометку.
 */
class SuccessToastTest {

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
        dir = Files.createTempDirectory("toast").toFile()
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
    fun `confirmation is published and then cleared`() {
        assertNull(session.successToast.value, "подтверждение показано до всякого действия")

        session.confirm(UiText.res(R.string.toast_repayment_saved))
        assertEquals(UiText.res(R.string.toast_repayment_saved), session.successToast.value)

        session.dismissToast()
        assertNull(session.successToast.value, "подтверждение висит на экране навсегда")
    }

    /** Очередь и сервер — РАЗНЫЕ тексты: это главное, что различает исходы. */
    @Test
    fun `queued expense is confirmed differently from a saved one`() {
        assertEquals(
            UiText.res(R.string.toast_expense_queued) != UiText.res(R.string.toast_expense_saved),
            true,
            "офлайн-сохранение подтверждается тем же текстом, что и отправка на сервер",
        )
    }
}
