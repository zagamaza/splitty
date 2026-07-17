package com.zagir.splitty.ui.expense

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import com.github.takahirom.roborazzi.RobolectricDeviceQualifiers
import com.github.takahirom.roborazzi.captureRoboImage
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.PersonShare
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Roborazzi-снапшоты чек-карточки (Task 8): read-only / интерактив (тапабельные
// строки, пульс-чипы) / «цена?» / нераспознанное имя, плюс «С кого сколько».
// Эталон — ios/ReceiptView.swift и docs/prototype/splitty-ai-proto.html.
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class ReceiptCardSnapshotTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val members = listOf(
        User(id = 1, displayName = "Аня"),
        User(id = 2, displayName = "Боря"),
        User(id = 3, displayName = "Вова"),
    )

    // Обычный чек: две позиции + пропорциональный сбор.
    private val receipt = listOf(
        OperationItem(
            name = "Пицца",
            price = 800,
            shares = listOf(ItemShare(1), ItemShare(2)),
        ),
        OperationItem(
            name = "Пиво",
            price = 300,
            qty = 3,
            shares = listOf(ItemShare(1), ItemShare(2), ItemShare(3)),
        ),
        OperationItem(
            name = "Сервис",
            price = 110,
            kind = OperationItem.KIND_SURCHARGE,
            split = OperationItem.SPLIT_PROPORTIONAL,
            percent = 10,
        ),
    )

    private val pricelessReceipt = listOf(
        OperationItem(name = "Пицца", price = 800, shares = listOf(ItemShare(1), ItemShare(2))),
        OperationItem(name = "Салат", price = 0, shares = listOf(ItemShare(1), ItemShare(2))),
    )

    private val unknownReceipt = listOf(
        OperationItem(name = "Стейк", price = 500, shares = listOf(ItemShare(1))),
        OperationItem(
            name = "Вино",
            price = 400,
            shares = listOf(ItemShare(2)),
            unknown = listOf("Саня"),
        ),
    )

    private fun snapshot(name: String, dark: Boolean = false, content: @Composable () -> Unit) {
        composeRule.setContent {
            SplittyTheme(darkTheme = dark) {
                Box(
                    modifier = Modifier
                        .background(Splitty.colors.bg)
                        .padding(24.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Box(Modifier.width(360.dp), content = { content() })
                }
            }
        }
        composeRule.onRoot().captureRoboImage("src/test/snapshots/$name.png")
    }

    @Test fun receiptReadOnly() = snapshot("receipt_readonly_light") {
        ReceiptCard(items = receipt, members = members, currency = "RUB")
    }

    @Test fun receiptReadOnlyDark() = snapshot("receipt_readonly_dark", dark = true) {
        ReceiptCard(items = receipt, members = members, currency = "RUB")
    }

    @Test fun receiptInteractive() = snapshot("receipt_interactive_light") {
        ReceiptCard(
            items = receipt,
            members = members,
            currency = "RUB",
            onEditItem = {},
            onResolveUnknown = { _, _ -> },
            onToggleSurchargeRule = {},
            highlightedIndices = setOf(0),
        )
    }

    @Test fun receiptPriceless() = snapshot("receipt_priceless_light") {
        ReceiptCard(
            items = pricelessReceipt,
            members = members,
            currency = "RUB",
            onEditItem = {},
        )
    }

    @Test fun receiptUnknown() = snapshot("receipt_unknown_light") {
        ReceiptCard(
            items = unknownReceipt,
            members = members,
            currency = "RUB",
            onEditItem = {},
            onResolveUnknown = { _, _ -> },
        )
    }

    @Test fun personBreakdown() = snapshot("person_breakdown_light") {
        PersonBreakdownCard(
            shares = listOf(
                PersonShare(userId = 1, total = 605, surchargePart = 55),
                PersonShare(userId = 2, total = 605, surchargePart = 55),
                PersonShare(userId = 3, total = 100, surchargePart = 0),
            ),
            members = members,
            currency = "RUB",
            meId = 1,
        )
    }

    @Test fun personBreakdownDark() = snapshot("person_breakdown_dark", dark = true) {
        PersonBreakdownCard(
            shares = listOf(
                PersonShare(userId = 1, total = 605, surchargePart = 55),
                PersonShare(userId = 2, total = 605, surchargePart = 55),
                PersonShare(userId = 3, total = 100, surchargePart = 0),
            ),
            members = members,
            currency = "RUB",
            meId = 1,
        )
    }
}
