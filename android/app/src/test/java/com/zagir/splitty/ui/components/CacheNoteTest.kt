package com.zagir.splitty.ui.components

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import com.zagir.splitty.core.model.DataFreshness
import com.zagir.splitty.ui.theme.SplittyTheme
import java.time.Instant
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Подпись о свежести данных.
 *
 * До этого она была только в списке групп: на карточке группы, друзьях и в
 * активности признак считался, но человеку не доставался — он смотрел на
 * старые суммы, ничего об этом не зная.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CacheNoteTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `fresh data carries no note`() {
        composeRule.setContent {
            SplittyTheme { CacheNote(freshness = DataFreshness(fromCache = false, updatedAt = Instant.now())) }
        }

        composeRule.onNodeWithTag("cache_note").assertDoesNotExist()
    }

    @Test
    fun `cache with a known update time says when`() {
        composeRule.setContent {
            SplittyTheme {
                CacheNote(
                    freshness = DataFreshness(
                        fromCache = true,
                        updatedAt = Instant.now().minusSeconds(3600),
                    )
                )
            }
        }

        composeRule.onNodeWithTag("cache_note").assertIsDisplayed()
        composeRule.onNodeWithText(
            context.getString(R.string.groups_cached_no_connection)
        ).assertDoesNotExist()
    }

    /** Первый запуск офлайн: врать «обновлялось только что» нечем и незачем. */
    @Test
    fun `cold start offline says there is no connection`() {
        composeRule.setContent {
            SplittyTheme { CacheNote(freshness = DataFreshness(fromCache = true)) }
        }

        composeRule.onNodeWithText(
            context.getString(R.string.groups_cached_no_connection)
        ).assertIsDisplayed()
    }
}
