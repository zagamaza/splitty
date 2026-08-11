package com.zagir.splitty.ui.profile

import com.zagir.splitty.core.ui.resolve
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.R
import com.zagir.splitty.core.model.NotifySettings
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.groups.GroupsErrorState
import com.zagir.splitty.ui.theme.Splitty

/**
 * Настройки уведомлений: мастер-тумблер (верх экрана, PATCH /me) и категории
 * событий × канал доставки (PATCH /me/notifications). Мастер выключен —
 * категории задизейблены и притушены. Telegram работает сразу (шлёт бот),
 * «Приложение» — задел под пуши (APNs/FCM), пока выключен и подписан «скоро».
 * Порт iOS NotificationSettingsView.
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
        val settings = state.settings
        when {
            settings != null -> SettingsContent(
                state = state,
                settings = settings,
                onMasterChange = viewModel::setMaster,
                onChange = viewModel::saveCategories,
                modifier = Modifier.padding(innerPadding),
            )

            state.loadError != null -> Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                GroupsErrorState(message = state.loadError!!.resolve(), onRetry = viewModel::retry)
            }

            else -> Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(color = colors.accent)
            }
        }
    }

    // Ошибка сохранения — алерт поверх формы (не молчаливый откат).
    val alert = state.alertMessage
    if (alert != null) {
        AlertDialog(
            onDismissRequest = viewModel::dismissAlert,
            title = { Text(stringResource(R.string.common_error_title)) },
            text = { Text(alert.resolve()) },
            confirmButton = {
                TextButton(onClick = viewModel::dismissAlert) {
                    Text(stringResource(R.string.common_ok))
                }
            },
        )
    }
}

@Composable
private fun SettingsContent(
    state: NotifyScreenState,
    settings: NotifySettings,
    onMasterChange: (Boolean) -> Unit,
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
        MasterSection(
            masterOn = state.masterOn,
            enabled = !state.isSaving,
            onChange = onMasterChange,
        )
        // Мастер выключен — категории не действуют: блокируем и притушаем.
        Column(
            modifier = Modifier.alpha(if (state.masterOn) 1f else 0.5f),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            NotifySection(
                title = stringResource(R.string.notifications_operations),
                footer = stringResource(R.string.notifications_operations_footer),
                telegramOn = settings.operations.telegram,
                pushOn = settings.operations.push,
                enabled = state.categoriesEnabled,
                onTelegramChange = { on ->
                    onChange(settings.copy(operations = settings.operations.copy(telegram = on)))
                },
                onPushChange = { on ->
                    onChange(settings.copy(operations = settings.operations.copy(push = on)))
                },
            )
            NotifySection(
                title = stringResource(R.string.notifications_debts),
                footer = stringResource(R.string.notifications_debts_footer),
                telegramOn = settings.debts.telegram,
                pushOn = settings.debts.push,
                enabled = state.categoriesEnabled,
                onTelegramChange = { on ->
                    onChange(settings.copy(debts = settings.debts.copy(telegram = on)))
                },
                onPushChange = { on ->
                    onChange(settings.copy(debts = settings.debts.copy(push = on)))
                },
            )
            NotifySection(
                title = stringResource(R.string.notifications_invites),
                footer = stringResource(R.string.notifications_invites_footer),
                telegramOn = settings.invites.telegram,
                pushOn = settings.invites.push,
                enabled = state.categoriesEnabled,
                onTelegramChange = { on ->
                    onChange(settings.copy(invites = settings.invites.copy(telegram = on)))
                },
                onPushChange = { on ->
                    onChange(settings.copy(invites = settings.invites.copy(push = on)))
                },
            )
        }
    }
}

/** Первая секция — мастер-тумблер всех уведомлений (PATCH /me). */
@Composable
private fun MasterSection(
    masterOn: Boolean,
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
) {
    val colors = Splitty.colors
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.notifications_title),
                fontSize = 16.sp,
                color = colors.ink,
            )
            Spacer(Modifier.weight(1f))
            Switch(
                checked = masterOn,
                onCheckedChange = onChange,
                enabled = enabled,
                colors = SwitchDefaults.colors(checkedTrackColor = colors.accent),
            )
        }
    }
}

@Composable
private fun NotifySection(
    title: String,
    footer: String,
    telegramOn: Boolean,
    pushOn: Boolean,
    enabled: Boolean,
    onTelegramChange: (Boolean) -> Unit,
    onPushChange: (Boolean) -> Unit,
) {
    val colors = Splitty.colors
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(title, modifier = Modifier.padding(start = 4.dp))
        SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 0.dp) {
            ChannelRow(
                title = stringResource(R.string.notifications_channel_telegram),
                checked = telegramOn,
                enabled = enabled,
                onChange = onTelegramChange,
            )
            // Push-канал (FCM): значение с сервера, PATCH тем же saveCategories.
            ChannelRow(
                title = stringResource(R.string.notifications_channel_push),
                checked = pushOn,
                enabled = enabled,
                onChange = onPushChange,
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
        Spacer(Modifier.weight(1f))
        Switch(
            checked = checked,
            onCheckedChange = onChange,
            enabled = enabled,
            colors = SwitchDefaults.colors(checkedTrackColor = colors.accent),
        )
    }
}
