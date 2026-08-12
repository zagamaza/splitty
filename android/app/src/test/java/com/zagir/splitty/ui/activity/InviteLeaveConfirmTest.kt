package com.zagir.splitty.ui.activity

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import com.zagir.splitty.core.model.InviteCard
import com.zagir.splitty.core.model.InviteStatus
import com.zagir.splitty.ui.theme.SplittyTheme
import java.time.Instant
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Выход из группы с карточки приглашения спрашивают.
 *
 * Кнопка «Выйти» стоит рядом с «Открыть», а сам выход необратим: вернуться
 * можно только по новому приглашению. Один промах — и человек вне группы,
 * причём вернуть его может только кто-то другой.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class InviteLeaveConfirmTest {

    @get:Rule
    val composeRule = createComposeRule()

    private val card = InviteCard(
        roomId = "room1",
        roomName = "Поездка",
        inviterName = "Загир",
        status = InviteStatus.ADDED,
        createdAt = Instant.parse("2026-08-12T10:00:00Z"),
    )

    @Test
    fun `leave asks first, other actions do not`() {
        assertTrue(inviteActionNeedsConfirm(InviteAction.LEAVE), "выход из группы происходит по одному тапу")
        assertFalse(inviteActionNeedsConfirm(InviteAction.ACCEPT))
        assertFalse(inviteActionNeedsConfirm(InviteAction.DECLINE))
    }

    @Test
    fun `leaving happens only after the confirmation`() {
        var left = 0
        composeRule.setContent {
            SplittyTheme {
                InviteLeaveConfirmDialog(card = card, onConfirm = { left++ }, onDismiss = {})
            }
        }

        assertEquals(0, left, "выход случился до подтверждения")
        composeRule.onNodeWithTag("invite_leave_confirm").performClick()
        assertEquals(1, left, "подтверждение не привело к выходу")
    }
}
