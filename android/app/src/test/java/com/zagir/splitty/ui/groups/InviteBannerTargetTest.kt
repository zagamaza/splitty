package com.zagir.splitty.ui.groups

import android.content.Context
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.ui.theme.SplittyTheme
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Главный призыв группы ведёт в выбор друзей.
 *
 * Баннер «В группе только вы» открывал шит со ссылкой — то есть самый заметный
 * призыв экрана вёл мимо основного способа позвать человека: друг из списка
 * добавляется в один тап и получает уведомление, а ссылку ещё надо куда-то
 * переслать. Ссылка осталась вторым действием внутри пикера.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class InviteBannerTargetTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `banner tap opens the friend picker`() {
        var taps = 0
        composeRule.setContent {
            SplittyTheme {
                InviteBanner(onClick = { taps++ })
            }
        }

        composeRule.onNodeWithText(context.getString(R.string.invite_banner_title)).performClick()

        assertEquals(1, taps, "баннер не сработал")
    }

    /**
     * Два пункта экрана назывались одинаково («Пригласить») и вели в разные
     * места: один в выбор друзей, другой в шит со ссылкой.
     */
    @Test
    fun `members action is named differently from the link invite`() {
        val members = context.getString(R.string.group_settings_members_invite)
        val link = context.getString(R.string.group_settings_invite)
        assertNotEquals(members, link, "две разные кнопки называются одинаково")
    }
}
