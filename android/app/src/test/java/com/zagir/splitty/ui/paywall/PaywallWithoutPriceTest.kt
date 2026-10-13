package com.zagir.splitty.ui.paywall

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

// Тарифы ещё не пришли из Play (или не придут — эмулятор без Play): кнопка
// печатала «Оформить за», а условие — « за . Продлевается автоматически».
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "ru")
class PaywallWithoutPriceTest {

    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun noPricePlaceholdersBeforeProductsLoad() {
        composeRule.setContent {
            SplittyTheme {
                PaywallSheet(
                    quota = null,
                    products = emptyList(),
                    isPurchasing = false,
                    isLoadingProducts = true,
                    errorMessage = null,
                    onPurchase = { _, _ -> },
                    onRestore = {},
                    onOpenUrl = {},
                    onDismiss = {},
                )
            }
        }
        composeRule.onAllNodesWithText("Оформить за", substring = true).assertCountEquals(0)
        composeRule.onAllNodesWithText("Продлевается автоматически", substring = true).assertCountEquals(0)
        composeRule.onAllNodesWithText("Splitor Plus").assertCountEquals(2)
    }
}
