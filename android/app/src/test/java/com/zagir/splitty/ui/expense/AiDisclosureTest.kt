package com.zagir.splitty.ui.expense

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.session.TokenCipher
import com.zagir.splitty.ui.theme.SplittyTheme
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Rule
import org.junit.Test as JUnitTest
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Пояснение о том, что распознавание идёт на сервере.
 *
 * Куда уходят голос и фото чека, человек мог узнать только из политики, которую
 * никто не открывает. Показываем один раз перед первой отправкой: повторять на
 * каждом расходе — значит приучить закрывать не читая.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AiDisclosureTest {

    @get:Rule
    val composeRule = createComposeRule()

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
        dir = Files.createTempDirectory("ai-disclosure").toFile()
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

    @JUnitTest
    fun `disclosure is shown once and remembered`() = runBlocking {
        assertFalse(session.aiDisclosureSeen.first(), "флаг взведён до первого показа")

        session.markAiDisclosureSeen()

        assertTrue(
            session.aiDisclosureSeen.first(),
            "пояснение показывается снова и снова — человек привыкнет закрывать не читая",
        )
    }

    @JUnitTest
    fun `confirming the dialog proceeds to recognition`() {
        var confirmed = 0
        composeRule.setContent {
            SplittyTheme {
                AiDisclosureDialog(onConfirm = { confirmed++ }, onDismiss = {})
            }
        }

        assertEquals(0, confirmed, "распознавание началось до подтверждения")
        composeRule.onNodeWithTag("ai_disclosure_ok").performClick()
        assertEquals(1, confirmed, "после «Понятно» распознавание не началось")
    }
}
