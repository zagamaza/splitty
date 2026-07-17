package com.zagir.splitty.ui.groups

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import com.github.takahirom.roborazzi.RobolectricDeviceQualifiers
import com.github.takahirom.roborazzi.captureRoboImage
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Roborazzi-снапшоты пустых состояний (Task 5): пустая карточка групп с
// кнопками «Создать»/«Присоединиться» и пустая группа с «Добавить расход».
// В пустом состоянии показываем первый шаг, а не только объяснение.

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class GroupsEmptyStateSnapshotTest {

    @get:Rule
    val composeRule = createComposeRule()

    private fun snapshot(name: String, dark: Boolean, content: @Composable () -> Unit) {
        composeRule.setContent {
            SplittyTheme(darkTheme = dark) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(Splitty.colors.bg)
                        .padding(24.dp),
                    contentAlignment = Alignment.Center,
                    content = { content() },
                )
            }
        }
        composeRule.onRoot().captureRoboImage("src/test/snapshots/$name.png")
    }

    @Test
    fun groupsEmptyLight() = snapshot("groups_empty_light", dark = false) {
        GroupsEmptyCard(
            icon = Icons.Outlined.Groups,
            title = "Пока нет групп",
            subtitle = "Создайте группу или присоединитесь по коду приглашения",
        ) {
            PrimaryPillButton(text = "Создать группу", onClick = {})
            Spacer(Modifier.height(10.dp))
            SoftChip(text = "Присоединиться по коду", onClick = {})
        }
    }

    @Test
    fun groupsEmptyDark() = snapshot("groups_empty_dark", dark = true) {
        GroupsEmptyCard(
            icon = Icons.Outlined.Groups,
            title = "Пока нет групп",
            subtitle = "Создайте группу или присоединитесь по коду приглашения",
        ) {
            PrimaryPillButton(text = "Создать группу", onClick = {})
            Spacer(Modifier.height(10.dp))
            SoftChip(text = "Присоединиться по коду", onClick = {})
        }
    }

    @Test
    fun groupOpsEmptyLight() = snapshot("group_ops_empty_light", dark = false) {
        GroupsEmptyCard(
            icon = Icons.Outlined.Description,
            title = "Пока нет расходов",
            subtitle = "Добавьте первый расход — он появится в этом списке",
        ) {
            PrimaryPillButton(text = "Добавить расход", onClick = {})
        }
    }
}
