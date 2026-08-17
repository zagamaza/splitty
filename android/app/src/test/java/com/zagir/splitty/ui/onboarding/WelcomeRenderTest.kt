package com.zagir.splitty.ui.onboarding

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import com.github.takahirom.roborazzi.RobolectricDeviceQualifiers
import com.github.takahirom.roborazzi.captureRoboImage
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Рендер всех страниц приветствия в PNG — чтобы смотреть на экраны, не собирая
 * приложение. Не эталоны: файлы уходят в build/, сравнения нет.
 *
 * Часы остановлены (`autoAdvance = false`): иллюстрации крутят бесконечные
 * циклы, и с живыми часами тест никогда не дождался бы простоя.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class WelcomeRenderTest {

    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun renderEveryStep() {
        RuntimeEnvironment.setQualifiers("+ru")
        composeRule.mainClock.autoAdvance = false
        composeRule.setContent { SplittyTheme { WelcomeScreen(onFinish = {}) } }

        // Момент съёмки у каждой страницы свой: снимаем кадр, когда её анимация
        // договорила, но ещё не начала следующий круг.
        val settled = listOf(2_600L, 3_500L, 2_800L, 300L)

        settled.forEachIndexed { page, wait ->
            composeRule.mainClock.advanceTimeBy(wait)
            composeRule.onRoot().captureRoboImage("build/welcome-renders/welcome-$page.png")
            if (page == 1) {
                // У экрана записи три стадии, и все три надо увидеть.
                composeRule.mainClock.advanceTimeBy(2_400)
                composeRule.onRoot().captureRoboImage("build/welcome-renders/welcome-1-parsing.png")
                composeRule.mainClock.advanceTimeBy(2_000)
                composeRule.onRoot().captureRoboImage("build/welcome-renders/welcome-1-receipt.png")
            }
            if (page < 3) {
                composeRule.onNodeWithTag("welcome_primary").performClick()
                composeRule.mainClock.advanceTimeBy(1_000)
            }
        }

        // Второе состояние сравнения — то, ради чего последний экран и сделан.
        composeRule.mainClock.advanceTimeBy(2_500)
        composeRule.onRoot().captureRoboImage("build/welcome-renders/welcome-3-with.png")
    }
}
