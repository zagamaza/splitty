package com.zagir.splitty.ui.auth

import android.content.Context
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Density
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Что обещает экран входа.
 *
 * История в две ступени. Сначала была одна строка «Делите расходы с друзьями»
 * — она описывает любое приложение категории. Её сменили три пункта: что я
 * записываю, что это даёт, переводит ли приложение деньги.
 *
 * Теперь пункт снова один, и это не откат. Приветствие переехало ПЕРЕД входом
 * и объясняет первые два вопроса четырьмя страницами с арифметикой —
 * подробнее, чем строка списка. А «оно спишет деньги?» приветствие не
 * снимает, и ответ обязан стоять там, где человека просят войти.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class LoginValuePropsTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `login screen does not repeat what the intro already said`() {
        composeRule.setContent { SplittyTheme { ValueProps() } }

        composeRule.onNodeWithText(context.getString(R.string.login_prop_money_title))
            .assertIsDisplayed()
        listOf(R.string.login_prop_split_title, R.string.login_prop_once_title).forEach { res ->
            composeRule.onNodeWithText(context.getString(res)).assertDoesNotExist()
        }
    }

    /**
     * Крупный системный шрифт: блок обязан пережить его, иначе три пункта
     * вытолкнут кнопки входа за нижнюю кромку на маленьком экране.
     */
    @Test
    fun `points survive large system font`() {
        composeRule.setContent {
            val base = LocalDensity.current
            CompositionLocalProvider(
                LocalDensity provides Density(density = base.density, fontScale = 1.3f)
            ) {
                SplittyTheme { ValueProps() }
            }
        }

        composeRule.onNodeWithText(context.getString(R.string.login_prop_money_title)).assertIsDisplayed()
    }

    /** Самый частый страх — «оно спишет деньги?». Ответ обязан быть до входа. */
    @Test
    fun `money disclaimer is on the login screen`() {
        composeRule.setContent { SplittyTheme { ValueProps() } }

        composeRule.onNodeWithText(context.getString(R.string.login_prop_money_body)).assertIsDisplayed()
    }
}
