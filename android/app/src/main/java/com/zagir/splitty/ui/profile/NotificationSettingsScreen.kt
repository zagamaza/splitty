package com.zagir.splitty.ui.profile

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.background
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.groups.GroupsErrorState
import com.zagir.splitty.ui.theme.Splitty

/**
 * Настройки уведомлений: категория событий × канал доставки.
 * Telegram работает сразу (шлёт бот), «Приложение» — задел под пуши
 * (APNs/FCM), пока выключен и подписан «скоро». Порт iOS
 * NotificationSettingsView.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NotificationSettingsScreen(
    onBack: () -> Unit,
    viewModel: NotificationSettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val colors = Splitty.colors

    Scaffold(
        containerColor = colors.bg,
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = stringResource(R.string.notifications_title),
                        fontWeight = FontWeight.Bold,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.common_back),
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = colors.bg,
                    titleContentColor = colors.ink,
                    navigationIconContentColor = colors.ink,
                ),
            )
        },
    ) { innerPadding ->
        when (val current = state) {
            UiState.Loading -> Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(color = colors.accent)
            }

            is UiState.Error -> Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                GroupsErrorState(message = current.message, onRetry = viewModel::retry)
            }

            is UiState.Content -> SettingsContent(
                settings = current.value,
                onChange = viewModel::save,
                modifier = Modifier.padding(innerPadding),
            )
        }
    }
}

@Composable
private fun SettingsContent(
    settings: NotifySettings,
    onChange: (NotifySettings) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        NotifySection(
            title = stringResource(R.string.notifications_operations),
            footer = stringResource(R.string.notifications_operations_footer),
            telegramOn = settings.operations.telegram,
            onTelegramChange = { on ->
                onChange(settings.copy(operations = settings.operations.copy(telegram = on)))
            },
        )
        NotifySection(
            title = stringResource(R.string.notifications_debts),
            footer = stringResource(R.string.notifications_debts_footer),
            telegramOn = settings.debts.telegram,
            onTelegramChange = { on ->
                onChange(settings.copy(debts = settings.debts.copy(telegram = on)))
            },
        )
    }
}

@Composable
private fun NotifySection(
    title: String,
    footer: String,
    telegramOn: Boolean,
    onTelegramChange: (Boolean) -> Unit,
) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(title, modifier = Modifier.padding(start = 4.dp))
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            ChannelRow(
                title = stringResource(R.string.notifications_channel_telegram),
                checked = telegramOn,
                enabled = true,
                onChange = onTelegramChange,
            )
            // Пуши появятся вместе с APNs/FCM — тумблер-задел, пока недоступен.
            ChannelRow(
                title = stringResource(R.string.notifications_channel_push),
                badge = stringResource(R.string.notifications_soon),
                checked = false,
                enabled = false,
                onChange = {},
            )
        }
        Text(
            text = footer,
            fontSize = 12.sp,
            color = colors.inkSecondary,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
    }
}

@Composable
private fun ChannelRow(
    title: String,
    checked: Boolean,
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
    badge: String? = null,
) {
    val colors = Splitty.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = title,
            fontSize = 16.sp,
            color = if (enabled) colors.ink else colors.inkSecondary,
        )
        if (badge != null) {
            Text(
                text = badge,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
                color = colors.inkSecondary,
                modifier = Modifier
                    .padding(start = 8.dp)
                    .background(colors.hairline, RoundedCornerShape(999.dp))
                    .padding(horizontal = 7.dp, vertical = 2.dp),
            )
        }
        Spacer(Modifier.weight(1f))
        Switch(
            checked = checked,
            onCheckedChange = onChange,
            enabled = enabled,
            colors = SwitchDefaults.colors(checkedTrackColor = colors.accent),
        )
    }
}
