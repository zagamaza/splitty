package com.zagir.splitty.ui

import android.content.Context
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.expense.ItemSheetBody
import com.zagir.splitty.ui.expense.UnknownPickerBody
import com.zagir.splitty.ui.groups.InviteFriendsSheetBody
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Достижимость нижней части шитов при длинном списке людей.
 *
 * Шиты росли вниз без прокрутки: при десятке друзей кнопка «Добавить» уезжала
 * за нижнюю кромку, а в пикере нераспознанного имени за неё уходил нужный
 * человек — и выйти из этого было нельзя, потому что сохранение до сопоставления
 * заблокировано. Тест ставит содержимое в заведомо низкий контейнер: без
 * прокрутки `performScrollTo` не находит прокручиваемого родителя и падает.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class SheetScrollTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    private val many = (1L..12L).map { User(id = it, displayName = "Человек $it") }

    // Высота меньше содержимого: на полном экране Robolectric двенадцать строк
    // могли бы поместиться, и тест ничего бы не проверял.
    private fun inShortWindow(content: @Composable () -> Unit) {
        composeRule.setContent {
            SplittyTheme {
                Box(Modifier.height(240.dp)) { content() }
            }
        }
    }

    @Test
    fun `invite sheet send button is reachable with a long friend list`() {
        inShortWindow {
            InviteFriendsSheetBody(
                candidates = many.map { FriendBalance(user = it) },
                onInvite = {},
                onLink = {},
            )
        }

        composeRule.onNodeWithText(context.getString(R.string.invite_friends_send))
            .performScrollTo()
            .assertIsDisplayed()
    }

    @Test
    fun `invite sheet link chip stays reachable below the list`() {
        inShortWindow {
            InviteFriendsSheetBody(
                candidates = many.map { FriendBalance(user = it) },
                onInvite = {},
                onLink = {},
            )
        }

        composeRule.onNodeWithText(context.getString(R.string.invite_friends_link))
            .performScrollTo()
            .assertIsDisplayed()
    }

    @Test
    fun `unknown name picker reaches the last member`() {
        inShortWindow {
            UnknownPickerBody(name = "Вася", members = many, onPick = {})
        }

        composeRule.onNodeWithText(many.last().displayName)
            .performScrollTo()
            .assertIsDisplayed()
    }

    @Test
    fun `item sheet reaches the last participant`() {
        inShortWindow {
            ItemSheetBody(
                item = OperationItem(
                    name = "Пицца",
                    price = 1200,
                    shares = many.map { ItemShare(it.id) },
                ),
                members = many,
                currency = "RUB",
                meId = 1,
                onCommit = {},
                onDelete = {},
                onDismiss = {},
            )
        }

        composeRule.onNodeWithText(many.last().displayName)
            .performScrollTo()
            .assertIsDisplayed()
    }
}
