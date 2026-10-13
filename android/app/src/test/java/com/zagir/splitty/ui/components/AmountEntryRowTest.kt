package com.zagir.splitty.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zagir.splitty.ui.theme.SplittyTheme
import kotlin.test.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Длинная сумма с копейками на узком листе и крупном шрифте обязана уместиться
 * в поле целиком: внутренняя прокрутка поля прятала бы часть подтверждаемой
 * суммы. Через настоящий fontScale и нативную отрисовку: крупный шрифт Android
 * растит кегли нелинейно, а без NATIVE Robolectric меряет текст фиктивно, и
 * тест проходил даже без всякой подгонки.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34])
class AmountEntryRowTest {

    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun `long fractional amount fits a narrow sheet at large font`() {
        RuntimeEnvironment.setFontScale(2f)
        var layout: TextLayoutResult? = null
        composeRule.setContent {
            SplittyTheme {
                Box(Modifier.width(260.dp).testTag("paper")) {
                    AmountEntryRow(symbol = "₽", text = "1234567,89", baseSize = 52.sp, weight = FontWeight.ExtraBold) { fontSize, width ->
                        BasicTextField(
                            value = "1234567,89",
                            onValueChange = {},
                            modifier = Modifier.width(width).testTag("field"),
                            textStyle = TextStyle(fontSize = fontSize, fontWeight = FontWeight.ExtraBold, fontFeatureSettings = "tnum"),
                            singleLine = true,
                            onTextLayout = { layout = it },
                        )
                    }
                }
            }
        }
        composeRule.waitForIdle()
        val paper = composeRule.onNodeWithTag("paper").fetchSemanticsNode().boundsInRoot
        val field = composeRule.onNodeWithTag("field").fetchSemanticsNode().boundsInRoot
        assertTrue(field.left >= paper.left && field.right <= paper.right, "поле вылезло за лист: $field в $paper")
        val text = checkNotNull(layout)
        val textWidth = text.getLineRight(0) - text.getLineLeft(0)
        assertTrue(textWidth <= field.width + 1f, "строка $textWidth шире поля ${field.width} — часть суммы уехала в прокрутку")
    }
}
