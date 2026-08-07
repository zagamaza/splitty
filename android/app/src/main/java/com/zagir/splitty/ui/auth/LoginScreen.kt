package com.zagir.splitty.ui.auth

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
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
import androidx.browser.customtabs.CustomTabsIntent
import com.zagir.splitty.core.auth.TelegramWebAuth
import com.zagir.splitty.core.auth.rememberCredentialManagerHost
import com.zagir.splitty.ui.components.PrimaryPillButton
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Экран входа — паритет iOS LoginView: словомарка «Splitty», Google, форма
 * email + пароль и карточка входа по коду из @split_money_bot. Настройка
 * «Сервер» спрятана за жестом по логотипу (только DEBUG).
 */
@Composable
fun LoginScreen(viewModel: LoginViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    // Поле «Сервер» — инструмент разработки, а не настройка приложения: на
    // экране его нет, находит только тот, кто знает жест (паритет iOS).
    var logoTapCount by remember { mutableIntStateOf(0) }
    val isServerRevealed = logoTapCount >= SERVER_REVEAL_TAPS
    // Хост системного листа Credential Manager: активити плюс текст ошибки,
    // если её нет (общий с профилем, см. core/auth/ActivityContext.kt).
    val credentialHost = rememberCredentialManagerHost()
    // Возврат из Custom Tabs приходит интентом в активити, а не сюда:
    // MainActivity кладёт payload в шину, экран его и забирает.
    LaunchedEffect(Unit) {
        viewModel.telegramPayloads.collect(viewModel::onTelegramResult)
    }

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
            Logo(onTap = { if (BuildConfig.DEBUG && !isServerRevealed) logoTapCount++ })
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
            TelegramLoginButton(
                baseUrl = state.baseUrl,
                enabled = !state.isLoggingIn,
                onError = viewModel::showError,
            )
            OrDivider()
            EmailLoginCard(
                state = state,
                viewModel = viewModel,
            )
            // Только в DEBUG и только после жеста: в релизе поле «Сервер» —
            // способ увести Bearer-токен на чужой адрес (паритет iOS).
            if (BuildConfig.DEBUG && isServerRevealed) {
                ServerField(
                    baseUrl = state.baseUrl,
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

/** Тапов по логотипу, открывающих поле «Сервер» (только DEBUG). */
private const val SERVER_REVEAL_TAPS = 5

/**
 * Крупная словомарка: изумрудный логотип и тихий подзаголовок.
 * В DEBUG он же — тайная дверь к полю «Сервер» (см. SERVER_REVEAL_TAPS).
 */
@Composable
private fun Logo(onTap: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            // Без индикации: жест служебный, подсвечивать его нечего.
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                onClick = onTap,
            )
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

/**
 * Вход через Telegram Login Widget: `<baseUrl>/tg-auth` в Custom Tabs, оттуда
 * oauth.telegram.org и возврат в приложение по `splitty://tg-callback`
 * (см. core/auth/TelegramWebAuth, internal/rest/tg_callback.go).
 *
 * Кнопка, а не карточка с кодом: код надо было идти получать в бота, а виджет
 * логинит на месте — и уже вошедшему в Telegram в браузере не логиниться заново
 * (Custom Tabs делят cookie с Chrome, аналог iOS ASWebAuthenticationSession).
 */
@Composable
private fun TelegramLoginButton(baseUrl: String, enabled: Boolean, onError: (String) -> Unit) {
    val context = LocalContext.current
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(52.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(TelegramBlue)
            .clickable(enabled = enabled) { openTelegramWidget(context, baseUrl, onError) },
        contentAlignment = Alignment.Center,
    ) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.Send,
                contentDescription = null,
                tint = Color.White,
                modifier = Modifier.size(20.dp),
            )
            Text(
                text = stringResource(R.string.login_telegram_button),
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                color = Color.White,
            )
        }
    }
}

/** Фирменный синий Telegram — вне палитры темы: это чужой бренд. */
private val TelegramBlue = Color(0xFF2AABEE)

/**
 * Открывает страницу входа в Custom Tabs. Отдельного фолбэка не нужно:
 * без поддержки Custom Tabs библиотека сама уходит в обычный браузер.
 * ActivityNotFoundException остаётся возможен на устройстве вообще без
 * браузера — там честнее сказать об этом, чем молча ничего не сделать.
 */
private fun openTelegramWidget(context: Context, baseUrl: String, onError: (String) -> Unit) {
    val url = TelegramWebAuth.startUrl(baseUrl)
    try {
        CustomTabsIntent.Builder()
            .setShowTitle(false)
            .build()
            .launchUrl(context, Uri.parse(url))
    } catch (_: ActivityNotFoundException) {
        onError("Не нашли браузер, чтобы открыть вход через Telegram")
    }
}

/**
 * Адрес сервера. Заголовка-раскрывашки нет намеренно: поле появляется только
 * после жеста по логотипу и живёт до перезапуска экрана — это отладочный
 * тумблер, а не настройка, которую ищут глазами.
 */
@Composable
private fun ServerField(
    baseUrl: String,
    onBaseUrlChange: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 4.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(R.string.login_server_section),
            fontSize = 13.sp,
            color = Splitty.colors.inkSecondary,
        )
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
