package com.zagir.splitty.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.dp
import com.github.takahirom.roborazzi.RobolectricDeviceQualifiers
import com.github.takahirom.roborazzi.captureRoboImage
import com.zagir.splitty.ui.theme.Splitty
import com.zagir.splitty.ui.theme.SplittyTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// Roborazzi-инфраструктура (Task 3): JVM-снапшоты дизайн-системы без устройства.
// Плоский `testDebugUnitTest` пишет PNG и проходит; `recordRoborazziDebug`
// перезаписывает эталоны, `verifyRoborazziDebug` сверяет. SDK 34 — под кэш
// Robolectric; NATIVE graphics — иначе Compose рисует пустоту.

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = RobolectricDeviceQualifiers.Pixel5)
class ComponentSnapshotTest {

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
    fun failedStateLight() = snapshot("failed_state_light", dark = false) {
        FailedState(message = "Нет соединения с интернетом. Проверьте сеть", onRetry = {})
    }

    @Test
    fun failedStateDark() = snapshot("failed_state_dark", dark = true) {
        FailedState(message = "Нет соединения с интернетом. Проверьте сеть", onRetry = {})
    }

    @Test
    fun toastLight() = snapshot("toast_light", dark = false) {
        AppToastPill(text = "Саня — это Александр. Запомнил")
    }

    @Test
    fun toastDark() = snapshot("toast_dark", dark = true) {
        AppToastPill(text = "Саня — это Александр. Запомнил")
    }
}
