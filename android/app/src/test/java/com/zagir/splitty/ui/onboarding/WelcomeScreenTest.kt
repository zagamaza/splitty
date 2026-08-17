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
    fun `skip closes without creating a group`() {
        var finished: Boolean? = null
        composeRule.setContent { SplittyTheme { WelcomeScreen(onFinish = { finished = it }) } }

        composeRule.onNodeWithTag("welcome_skip").performClick()

        assertEquals(false, finished, "«Пропустить» не закрыл приветствие или увёл в создание группы")
    }
}
