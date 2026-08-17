package com.zagir.splitty.ui.groups

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Правило «позвать можно только того, с кем уже была общая группа» человек
 * узнавал, только не найдя нужного человека в списке. Подсказка стоит там же,
 * где в правило упираются.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class InviteHintTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `picker explains who can be invited`() {
        composeRule.setContent {
            SplittyTheme {
                InviteFriendsSheetBody(
                    candidates = listOf(FriendBalance(user = User(id = 1, displayName = "Аня"))),
                    onInvite = {},
                    onLink = {},
                )
            }
        }

        composeRule.onNodeWithTag("invite_friends_hint").assertIsDisplayed()
    }
}
