package com.zagir.splitty.ui.onboarding

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.ui.theme.SplittyTheme
import kotlin.test.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Приветствие: что человек видит и чем оно кончается.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WelcomeScreenTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `first screen explains what a group is`() {
        composeRule.setContent { SplittyTheme { WelcomeScreen(onFinish = {}) } }

        composeRule.onNodeWithText(context.getString(R.string.welcome_1_title)).assertIsDisplayed()
    }

    /**
     * «Пропустить» обязан быть виден сразу и означать «больше не показывать»,
     * иначе приветствие превращается в ловушку.
     */
    @Test
    fun `skip closes the intro`() {
        var finished = 0
        composeRule.setContent { SplittyTheme { WelcomeScreen(onFinish = { finished++ }) } }

        composeRule.onNodeWithTag("welcome_skip").performClick()

        assertEquals(1, finished, "«Пропустить» не закрыл приветствие")
    }

    /**
     * Пропуск — это ответ «не показывай больше», а не «покажи потом»: в
     * событиях он обязан отличаться от дочитанного до конца, иначе шаг воронки
     * перестаёт быть счётным.
     */
    @Test
    fun `skip reports onboarding_skipped`() {
        val names = mutableListOf<String>()
        composeRule.setContent {
            SplittyTheme { WelcomeScreen(onFinish = {}, onEvent = { names += it.name }) }
        }

        composeRule.onNodeWithTag("welcome_skip").performClick()

        assertEquals(
            listOf("onboarding_started", "onboarding_step", "onboarding_skipped"),
            names,
            "события приветствия разошлись с контрактом",
        )
    }
}
