package com.zagir.splitty.ui.expense

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Плательщик в режиме чека: позиции решают, КАК делить, а не КТО дал деньги,
 * поэтому строка «Заплатил(а) …» обязана оставаться рядом с распознанным чеком.
 * Раньше её уносило вместе со всей карточкой деления — расход молча записывался
 * на текущего пользователя, и поменять плательщика было нечем.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ReceiptModePayerTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    private val members = listOf(User(id = 1, displayName = "Аня"), User(id = 2, displayName = "Боря"))

    private fun form(payerId: Long) = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        meId = 1L,
        payerId = payerId,
        description = "Ужин",
        sumText = "800",
        draftItems = listOf(
            OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1), ItemShare(2))),
        ),
        didRecognize = true,
    )

    private fun render(payerId: Long) {
        composeRule.setContent {
            SplittyTheme {
                ReceiptModeSection(
                    form = form(payerId),
                    onSelectPayer = {},
                    onEditItem = {},
                    onResolveUnknown = { _, _ -> },
                    onAddItem = {},
                    onToggleSurchargeRule = {},
                    onCollapseToEqual = {},
                    onHighlightsShown = {},
                )
            }
        }
    }

    @Test
    fun `payer row is shown next to the recognized receipt`() {
        render(payerId = 1L)

        composeRule.onNodeWithText(context.getString(R.string.expense_paid_by_you)).assertIsDisplayed()
        composeRule.onNodeWithText("${context.getString(R.string.expense_payer_you)} ▾").assertIsDisplayed()
        // именно режим чека, а не плоская карточка деления
        composeRule.onNodeWithText("Пицца").assertIsDisplayed()
    }

    @Test
    fun `payer row names another member when they paid`() {
        render(payerId = 2L)

        composeRule.onNodeWithText(context.getString(R.string.expense_paid_by_other)).assertIsDisplayed()
        composeRule.onNodeWithText("Боря ▾").assertIsDisplayed()
    }
}
