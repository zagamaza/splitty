package com.zagir.splitty.ui.expense

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
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
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Roborazzi-снапшоты шита позиции (Task 10): режимы «Долями» (веса-степперы) и
// «Суммами» (поля точных сумм). Эталон — ios/.../ItemSheetView и docs/prototype.
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class ItemSheetSnapshotTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val members = listOf(
        User(id = 1, displayName = "Аня"),
        User(id = 2, displayName = "Боря"),
        User(id = 3, displayName = "Вова"),
    )

    private fun snapshot(name: String, dark: Boolean = false, content: @Composable () -> Unit) {
        composeRule.setContent {
            SplittyTheme(darkTheme = dark) {
                Box(modifier = Modifier.background(Splitty.colors.bg), contentAlignment = Alignment.Center) {
                    Box(Modifier.width(360.dp), content = { content() })
                }
            }
        }
        composeRule.onRoot().captureRoboImage("src/test/snapshots/$name.png")
    }

    // «Долями»: у Ани две доли, у остальных по одной — степперы и живые суммы.
    @Test fun itemSheetWeights() = snapshot("item_sheet_weights_light") {
        ItemSheetBody(
            item = OperationItem(
                name = "Пицца",
                price = 1200,
                shares = listOf(ItemShare(1, weight = 2), ItemShare(2), ItemShare(3)),
            ),
            members = members,
            currency = "RUB",
            meId = 1,
            onCommit = {},
            onDelete = {},
            onDismiss = {},
        )
    }

    // «Суммами»: у Ани фикс 400, остальные «авто» — поля сумм и остаток.
    @Test fun itemSheetAmounts() = snapshot("item_sheet_amounts_light") {
        ItemSheetBody(
            item = OperationItem(
                name = "Пицца",
                price = 1200,
                shares = listOf(ItemShare(1, amount = 400), ItemShare(2), ItemShare(3)),
            ),
            members = members,
            currency = "RUB",
            meId = 1,
            onCommit = {},
            onDelete = {},
            onDismiss = {},
        )
    }
}
