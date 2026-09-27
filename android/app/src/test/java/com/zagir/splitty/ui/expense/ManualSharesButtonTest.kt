package com.zagir.splitty.ui.expense

import android.content.Context
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.test.performSemanticsAction
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.theme.SplittyTheme
import kotlin.test.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Кнопка «Изменить суммы участников» в режиме чека — отрисовка и нажатие.
 *
 * Чистые функции формы проверяет [ManualSharesTest]; здесь — то, чего они не
 * видят: что кнопка вообще есть на экране, зовёт свой колбэк, а при
 * нераспознанном имени остаётся на месте неактивной и объясняет причину, а не
 * исчезает.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ManualSharesButtonTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    private val members = listOf(User(id = 1, displayName = "Аня"), User(id = 2, displayName = "Боря"))

    private fun form(item: OperationItem) = AddExpenseForm(
        isEditing = false,
        showsRoomPicker = false,
        selectedRoomId = "room1",
        members = members,
        currency = "RUB",
        meId = 1L,
        payerId = 1L,
        description = "Ужин",
        draftItems = listOf(item),
        didRecognize = true,
    )

    private fun render(form: AddExpenseForm, onEditAmounts: () -> Unit = {}) {
        composeRule.setContent {
            SplittyTheme {
                ReceiptModeSection(
                    form = form,
                    onSelectPayer = {},
                    onEditItem = {},
                    onResolveUnknown = { _, _ -> },
                    onAddItem = {},
                    onToggleSurchargeRule = {},
                    onCollapseToEqual = {},
                    onEditAmounts = onEditAmounts,
                    onHighlightsShown = {},
                )
            }
        }
    }

    @Test
    fun `button is shown under the receipt and calls back`() {
        var clicks = 0
        render(
            form(OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1), ItemShare(2)))),
            onEditAmounts = { clicks++ },
        )

        composeRule.onNodeWithTag("edit_participant_amounts").assertIsEnabled()
            // Семантическое нажатие: кнопка ниже видимой области маленького
            // экрана Robolectric, а жест требует видимости.
            .performSemanticsAction(SemanticsActions.OnClick)
        composeRule.onNodeWithText(context.getString(R.string.expense_edit_amounts_hint)).assertExists()
        assertEquals(1, clicks)
    }

    /** Нераспознанное имя: кнопка на месте, неактивна, и сказано почему. */
    @Test
    fun `unknown name keeps the button visible but disabled with a reason`() {
        render(
            form(
                OperationItem(
                    name = "Пицца", price = 800,
                    shares = listOf(ItemShare(1)), unknown = listOf("Саня"),
                ),
            ),
        )

        composeRule.onNodeWithTag("edit_participant_amounts").assertIsNotEnabled()
        composeRule.onNodeWithText(context.getString(R.string.expense_edit_amounts_unavailable_items))
            .assertExists()
    }
}
