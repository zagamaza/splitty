package com.zagir.splitty.ui.auth

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.BuildConfig
import com.zagir.splitty.R
import com.zagir.splitty.core.auth.rememberCredentialManagerHost
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Экран входа — полный паритет iOS LoginView: словомарка «Splitty», карточка
 * «Вход через Telegram» (код из @split_money_bot по /login → POST /auth/code),
 * свёрнутый «Вход для разработки» (POST /auth/dev) и тихая настройка «Сервер».
 */
@Composable
fun LoginScreen(viewModel: LoginViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var isDevExpanded by remember { mutableStateOf(false) }
    var isServerExpanded by remember { mutableStateOf(false) }
    // Хост системного листа Credential Manager: активити плюс текст ошибки,
    // если её нет (общий с профилем, см. core/auth/ActivityContext.kt).
    val credentialHost = rememberCredentialManagerHost()

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .imePadding()
                .padding(horizontal = 20.dp)
                .padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            Logo()
            // Вход через Google — над блоком входа по коду: для человека без
            // Telegram это единственный путь внутрь, и он не должен искать его
            // под инструкцией про бота.
            GoogleSignInButton(
                enabled = !state.isLoggingIn,
                onClick = {
                    credentialHost.launch(onError = viewModel::showError) { host ->
                        viewModel.loginWithGoogle(host)
                    }
                },
            )
            OrDivider()
            EmailLoginCard(
                state = state,
                viewModel = viewModel,
            )
            TelegramLoginCard(
                code = state.code,
                isValid = state.isCodeValid,
                isLoggingIn = state.isLoggingIn,
                onCodeChange = viewModel::onCodeChange,
                onSubmit = viewModel::loginWithCode,
            )
            // Dev-вход и настройка сервера — только в DEBUG-сборках: в релизе
            // это бэкдор мимо авторизации через Telegram (паритет iOS #if DEBUG).
            if (BuildConfig.DEBUG) {
                DevLoginCard(
                    state = state,
                    isExpanded = isDevExpanded,
                    onToggle = { isDevExpanded = !isDevExpanded },
                    viewModel = viewModel,
                )
                ServerDisclosure(
                    baseUrl = state.baseUrl,
                    isExpanded = isServerExpanded,
                    onToggle = { isServerExpanded = !isServerExpanded },
                    onBaseUrlChange = viewModel::onBaseUrlChange,
                )
            }
        }

        if (state.isLoggingIn) {
            LoadingOverlay()
        }
    }

    state.errorMessage?.let { message ->
        AlertDialog(
            onDismissRequest = viewModel::dismissError,
            confirmButton = {
                TextButton(onClick = viewModel::dismissError) {
                    Text(stringResource(R.string.common_ok))
                }
            },
            title = { Text(stringResource(R.string.common_error_title)) },
            text = { Text(message) },
        )
    }
}

/** Крупная словомарка: изумрудный логотип и тихий подзаголовок. */
@Composable
private fun Logo() {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 72.dp, bottom = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = stringResource(R.string.app_name),
            fontSize = 46.sp,
            fontWeight = FontWeight.Bold,
            color = Splitty.colors.accent,
        )
        Text(
            text = stringResource(R.string.login_tagline),
            fontSize = 17.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.inkSecondary,
        )
    }
}

/**
 * Кнопка входа через Google: нейтральная подложка + hairline, геометрия
 * повторяет iOS (высота 52, радиус 14). Акцентную заливку (`PrimaryPillButton`)
 * не берём — она перетянула бы внимание с основного входа по коду, а логотип G
 * не рисуем: своей отрисовкой чужого бренда легко нарушить его гайдлайны.
 */
@Composable
private fun GoogleSignInButton(
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val colors = Splitty.colors
    val shape = RoundedCornerShape(14.dp)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(52.dp)
            .background(colors.surface, shape)
            .border(1.dp, colors.hairline, shape)
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = stringResource(R.string.login_google_button),
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
            color = if (enabled) colors.ink else colors.inkSecondary,
        )
    }
}

@Composable
private fun OrDivider() {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier
                .weight(1f)
                .height(1.dp)
                .background(Splitty.colors.hairline),
        )
        Text(
            text = stringResource(R.string.login_or),
            fontSize = 13.sp,
            color = Splitty.colors.inkSecondary,
        )
        Box(
            Modifier
                .weight(1f)
                .height(1.dp)
                .background(Splitty.colors.hairline),
        )
    }
}

/**
 * Вход и регистрация по email с паролем — для тех, у кого нет ни Google, ни
 * Telegram. Та же карточка переключается в регистрацию: добавляется поле имени.
 */
@Composable
private fun EmailLoginCard(
    state: LoginUiState,
    viewModel: LoginViewModel,
) {
    SurfaceCard(
        modifier = Modifier
            .fillMaxWidth()
            .animateContentSize(),
        padding = 20.dp,
    ) {
        SectionHeader(
            stringResource(
                if (state.isRegistering) R.string.login_register_section else R.string.login_email_section
            )
        )
        Spacer(Modifier.height(12.dp))
        if (state.isRegistering) {
            LoginField(
                value = state.registerName,
                onValueChange = viewModel::onRegisterNameChange,
                placeholder = stringResource(R.string.login_name_placeholder),
            )
            Spacer(Modifier.height(12.dp))
        }
        LoginField(
            value = state.email,
            onValueChange = viewModel::onEmailChange,
            placeholder = stringResource(R.string.login_email_placeholder),
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Email,
                capitalization = KeyboardCapitalization.None,
                autoCorrectEnabled = false,
            ),
        )
        Spacer(Modifier.height(12.dp))
        LoginField(
            value = state.password,
            onValueChange = viewModel::onPasswordChange,
            placeholder = stringResource(R.string.login_password_placeholder),
            isPassword = true,
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Password,
                capitalization = KeyboardCapitalization.None,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Go,
            ),
            keyboardActions = KeyboardActions(onGo = { viewModel.submitEmailForm() }),
        )
        if (state.isRegistering && state.password.isNotEmpty() &&
            !EmailLoginForm.isValidPassword(state.password)
        ) {
            Spacer(Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.login_password_length_hint),
                fontSize = 13.sp,
                color = Splitty.colors.inkSecondary,
            )
        }
        Spacer(Modifier.height(16.dp))
        PrimaryPillButton(
            text = stringResource(
                if (state.isRegistering) R.string.login_register_button else R.string.login_email_button
            ),
            onClick = viewModel::submitEmailForm,
            enabled = state.isEmailFormValid && !state.isLoggingIn,
        )
        Spacer(Modifier.height(12.dp))
        Text(
            text = stringResource(
                if (state.isRegistering) R.string.login_switch_to_login else R.string.login_switch_to_register
            ),
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = Splitty.colors.accent,
            modifier = Modifier
                .fillMaxWidth()
                .clickable(enabled = !state.isLoggingIn, onClick = viewModel::toggleRegistering),
        )
    }
}

/** Основной вход: одноразовый код из Telegram-бота → POST /auth/code. */
@Composable
private fun TelegramLoginCard(
    code: String,
    isValid: Boolean,
    isLoggingIn: Boolean,
    onCodeChange: (String) -> Unit,
    onSubmit: () -> Unit,
) {
    val context = LocalContext.current
    SurfaceCard(modifier = Modifier.fillMaxWidth(), padding = 20.dp) {
        SectionHeader(stringResource(R.string.login_telegram_section))
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.Send,
                contentDescription = null,
                tint = Splitty.colors.accent,
                modifier = Modifier
                    .padding(top = 2.dp)
                    .size(16.dp),
            )
            Text(
                text = stringResource(R.string.login_telegram_hint),
                fontSize = 15.sp,
                color = Splitty.colors.inkSecondary,
            )
        }
        Spacer(Modifier.height(12.dp))
        // Прямой переход в бота: инструкция без кнопки заставляла руками
        // искать бота в Telegram (паритет iOS «Открыть бота»).
        SoftChip(
            text = stringResource(R.string.login_open_bot),
            onClick = { openLoginBot(context) },
        )
        Spacer(Modifier.height(12.dp))
        LoginField(
            value = code,
            onValueChange = onCodeChange,
            placeholder = stringResource(R.string.login_code_placeholder),
            monospaced = true,
            keyboardOptions = KeyboardOptions(
                capitalization = KeyboardCapitalization.Characters,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Go,
            ),
            keyboardActions = KeyboardActions(onGo = { onSubmit() }),
        )
        // Пока код короче минимума, объясняем, почему кнопка неактивна.
        if (!isValid) {
            Spacer(Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.login_code_length_hint),
                fontSize = 13.sp,
                color = Splitty.colors.inkSecondary,
            )
        }
        Spacer(Modifier.height(16.dp))
        PrimaryPillButton(
            text = stringResource(R.string.login_code_button),
            onClick = onSubmit,
            enabled = isValid && !isLoggingIn,
        )
    }
}

/**
 * Открывает Telegram-бота сразу с командой входа: `start=login` — бот понимает
 * «/start login» как /login и присылает код одним тапом. Пробуем deeplink в
 * приложение Telegram, при ActivityNotFoundException — https-фолбэк на t.me.
 */
private fun openLoginBot(context: Context) {
    val appIntent = Intent(
        Intent.ACTION_VIEW,
        Uri.parse("tg://resolve?domain=split_money_bot&start=login"),
    )
    try {
        context.startActivity(appIntent)
    } catch (_: ActivityNotFoundException) {
        val webIntent = Intent(
            Intent.ACTION_VIEW,
            Uri.parse("https://t.me/split_money_bot?start=login"),
        )
        context.startActivity(webIntent)
    }
}

/** Свёрнутый dev-вход: Telegram ID / Имя / Username / «Войти» (POST /auth/dev). */
@Composable
private fun DevLoginCard(
    state: LoginUiState,
    isExpanded: Boolean,
    onToggle: () -> Unit,
    viewModel: LoginViewModel,
) {
    SurfaceCard(
        modifier = Modifier
            .fillMaxWidth()
            .animateContentSize(),
        padding = 20.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onToggle),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionHeader(stringResource(R.string.login_dev_section))
            Spacer(Modifier.weight(1f))
            Icon(
                imageVector = if (isExpanded) Icons.Filled.ExpandLess else Icons.Filled.ExpandMore,
                contentDescription = null,
                tint = Splitty.colors.inkSecondary,
            )
        }
        if (isExpanded) {
            Spacer(Modifier.height(12.dp))
            LoginField(
                value = state.devTelegramId,
                onValueChange = viewModel::onDevTelegramIdChange,
                placeholder = stringResource(R.string.login_dev_telegram_id),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            )
            Spacer(Modifier.height(12.dp))
            LoginField(
                value = state.devDisplayName,
                onValueChange = viewModel::onDevDisplayNameChange,
                placeholder = stringResource(R.string.login_dev_name),
            )
            Spacer(Modifier.height(12.dp))
            LoginField(
                value = state.devUsername,
                onValueChange = viewModel::onDevUsernameChange,
                placeholder = stringResource(R.string.login_dev_username),
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                ),
            )
            Spacer(Modifier.height(16.dp))
            PrimaryPillButton(
                text = stringResource(R.string.login_dev_button),
                onClick = viewModel::loginDev,
                enabled = state.isDevFormValid && !state.isLoggingIn,
            )
        }
    }
}

/** Тихая свёрнутая настройка адреса сервера внизу экрана. */
@Composable
private fun ServerDisclosure(
    baseUrl: String,
    isExpanded: Boolean,
    onToggle: () -> Unit,
    onBaseUrlChange: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .animateContentSize()
            .padding(horizontal = 4.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onToggle),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.login_server_section),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                color = Splitty.colors.inkSecondary,
            )
            Spacer(Modifier.weight(1f))
            Icon(
                imageVector = if (isExpanded) Icons.Filled.ExpandLess else Icons.Filled.ExpandMore,
                contentDescription = null,
                tint = Splitty.colors.inkSecondary,
            )
        }
        if (isExpanded) {
            Spacer(Modifier.height(8.dp))
            LoginField(
                value = baseUrl,
                onValueChange = onBaseUrlChange,
                placeholder = stringResource(R.string.login_server_placeholder),
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Uri,
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                ),
            )
        }
    }
}

/**
 * Поле ввода экрана входа: подложка цвета фона экрана + hairline-бордер —
 * читается и внутри surface-карточки, и на фоне (поле «Сервер»).
 */
@Composable
private fun LoginField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    monospaced: Boolean = false,
    isPassword: Boolean = false,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
) {
    val colors = Splitty.colors
    val shape = RoundedCornerShape(12.dp)
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier
            .fillMaxWidth()
            .background(colors.bg, shape)
            .border(1.dp, colors.hairline, shape)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        textStyle = TextStyle(
            color = colors.ink,
            fontSize = 17.sp,
            fontWeight = if (monospaced) FontWeight.SemiBold else FontWeight.Normal,
            fontFamily = if (monospaced) FontFamily.Monospace else FontFamily.Default,
        ),
        singleLine = true,
        cursorBrush = SolidColor(colors.accent),
        visualTransformation = if (isPassword) PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        decorationBox = { innerTextField ->
            Box {
                if (value.isEmpty()) {
                    Text(
                        text = placeholder,
                        fontSize = 17.sp,
                        color = colors.inkSecondary,
                        maxLines = 1,
                    )
                }
                innerTextField()
            }
        },
    )
}

/** Оверлей загрузки на время сетевого входа. */
@Composable
private fun LoadingOverlay() {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg.copy(alpha = 0.6f))
            .clickable(enabled = false, onClick = {}),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(color = Splitty.colors.accent)
    }
}
