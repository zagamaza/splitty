package com.zagir.splitty.ui.groups

import android.content.Context
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
 * Вход по коду приглашения доступен всегда, а не только при пустом списке.
 *
 * Раньше кнопка жила исключительно в пустом состоянии: человек с одной группой
 * попасть в экран ввода кода не мог никак. А код присылают как раз тем, у кого
 * группы уже есть, — приглашение переставало работать после первой.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GroupsAddMenuTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `join by code is reachable from the plus menu`() {
        var joinTaps = 0
        composeRule.setContent {
            SplittyTheme {
                GroupsAddMenu(onCreate = {}, onJoinByCode = { joinTaps++ })
            }
        }

        composeRule.onNodeWithTag("groups_add_menu").performClick()
        composeRule.onNodeWithTag("groups_join_by_code").performClick()

        assertEquals(1, joinTaps, "вход по коду недостижим из списка групп")
    }

    @Test
    fun `create group stays in the menu`() {
        var createTaps = 0
        composeRule.setContent {
            SplittyTheme {
                GroupsAddMenu(onCreate = { createTaps++ }, onJoinByCode = {})
            }
        }

        composeRule.onNodeWithTag("groups_add_menu").performClick()
        composeRule.onNodeWithText(context.getString(R.string.groups_create_group)).performClick()

        assertEquals(1, createTaps, "создание группы потерялось при переезде в меню")
    }
}
