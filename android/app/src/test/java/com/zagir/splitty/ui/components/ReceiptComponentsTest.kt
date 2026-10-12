package com.zagir.splitty.ui.components

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.printToString
import com.zagir.splitty.ui.theme.SplittyTheme
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Поведение элементов чека, которое не видно на картинке: реквизит не капсит
 * языки без регистра, а декор не попадает в TalkBack.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ReceiptComponentsTest {

    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun `caps only for languages that have letter case`() {
        assertTrue(ReceiptCaps.allowsCaps("ru"))
        assertTrue(ReceiptCaps.allowsCaps("de"))
        assertTrue(ReceiptCaps.allowsCaps(null))
        assertFalse(ReceiptCaps.allowsCaps("ja"))
        assertFalse(ReceiptCaps.allowsCaps("zh"))
        assertFalse(ReceiptCaps.allowsCaps("KO"))
    }

    /** Штамп читается ровно один раз, пунктир и черта — ни разу. */
    @Test
    fun `stamp text is read once and dividers are silent`() {
        composeRule.setContent {
            SplittyTheme {
                Column {
                    ReceiptDivider()
                    ReceiptDivider(style = ReceiptDividerStyle.TOTAL)
                    StatusStamp("ВЫ В РАСЧЁТЕ")
                }
            }
        }
        assertEquals(1, composeRule.onAllNodesWithText("ВЫ В РАСЧЁТЕ").fetchSemanticsNodes().size)
        val tree = composeRule.onRoot(useUnmergedTree = true).printToString()
        assertEquals(1, Regex("Text = ").findAll(tree).count(), "в дереве лишние узлы: $tree")
    }
}
