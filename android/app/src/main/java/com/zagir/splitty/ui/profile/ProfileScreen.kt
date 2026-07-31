package com.zagir.splitty.ui.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.zagir.splitty.BuildConfig
import com.zagir.splitty.R
import com.zagir.splitty.core.auth.findActivity
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.core.model.LoginProvider
import com.zagir.splitty.core.model.Me
import com.zagir.splitty.core.model.User
import com.zagir.splitty.ui.components.GradientAvatar
import com.zagir.splitty.ui.components.SectionHeader
import com.zagir.splitty.ui.components.SoftChip
import com.zagir.splitty.ui.components.SurfaceCard
import com.zagir.splitty.ui.theme.Splitty

/**
 * Вкладка «Профиль»: шапка с аватаром 88dp, карточка «Настройки»
 * (имя — правка диалогом, язык ru/en — меню, уведомления — Switch),
 * карточка «Сервер» (read-only, смена — на экране входа) и «Выйти».
 * Порт ios/Splitty/Features/Account/AccountView.swift.
 */
@Composable
fun ProfileScreen(
    onOpenNotifications: () -> Unit = {},
    viewModel: ProfileViewModel = hiltViewModel(),
) {
    val me by viewModel.me.collectAsStateWithLifecycle()
    val baseUrl by viewModel.baseUrl.collectAsStateWithLifecycle()
    val theme by viewModel.theme.collectAsStateWithLifecycle()
    val errorMessage by viewModel.errorMessage.collectAsStateWithLifecycle()
    val noticeMessage by viewModel.noticeMessage.collectAsStateWithLifecycle()
    val isSaving by viewModel.isSaving.collectAsStateWithLifecycle()
    val isIdentityBusy by viewModel.isIdentityBusy.collectAsStateWithLifecycle()
    val isDeleting by viewModel.isDeleting.collectAsStateWithLifecycle()
    val pendingOutboxCount by viewModel.pendingOutboxCount.collectAsStateWithLifecycle()

    var isEditNamePresented by remember { mutableStateOf(false) }
    var nameDraft by remember { mutableStateOf("") }
    var isLogoutConfirmPresented by remember { mutableStateOf(false) }
    var isDeleteConfirmPresented by remember { mutableStateOf(false) }
    var providerToUnlink by remember { mutableStateOf<LoginProvider?>(null) }
    // Credential Manager рисует системный лист поверх активити —
    // application-контекст ему не подходит (см. core/auth/ActivityContext.kt).
    val context = LocalContext.current
    val activity = remember(context) { context.findActivity() }

    // Локальная копия настройки языка: применяется оптимистично, PATCH /me —
    // фоном; ключ по значению профиля возвращает драфт к серверному значению.
    var langDraft by remember(me?.lang) { mutableStateOf(me?.lang ?: "ru") }
    // Ошибка PATCH — профиль не изменился, откатываем драфт к нему.
    LaunchedEffect(errorMessage) {
        if (errorMessage != null) {
            langDraft = me?.lang ?: "ru"
        }
    }

    val nameEmptyError = stringResource(R.string.profile_name_empty)

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Splitty.colors.bg)
            .verticalScroll(rememberScrollState()),
    ) {
        Text(
            text = stringResource(R.string.tab_account),
            modifier = Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp),
            fontSize = 32.sp,
            fontWeight = FontWeight.Bold,
            color = Splitty.colors.ink,
        )
        Column(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            HeaderSection(me)
            SettingsSection(
                me = me,
                lang = langDraft,
                theme = theme,
                onOpenNotifications = onOpenNotifications,
                enabled = !isSaving,
                onEditName = {
                    nameDraft = me?.displayName.orEmpty()
                    isEditNamePresented = true
                },
                onLangSelected = { newLang ->
                    if (newLang != langDraft) {
                        langDraft = newLang
                        viewModel.updateProfile(lang = newLang)
                    }
                },
                onThemeSelected = viewModel::onThemeSelected,
            )
            LoginMethodsSection(
                me = me,
                enabled = !isIdentityBusy && !isDeleting,
                onLink = { activity?.let(viewModel::linkGoogle) },
                onUnlinkRequest = { providerToUnlink = it },
            )
            // Адрес сервера — отладочная информация, в релизе пользователю не
            // нужна (менять его всё равно можно только в DEBUG на экране входа).
            if (BuildConfig.DEBUG) {
                ServerSection(baseUrl)
            }
            LogoutSection(onClick = { isLogoutConfirmPresented = true })
            // «Удалить аккаунт» — последним пунктом экрана: и Apple Guideline
            // 5.1.1(v), и Google Play требуют удаление в пару тапов от профиля.
            DeleteAccountSection(
                isDeleting = isDeleting,
                onClick = { isDeleteConfirmPresented = true },
            )
            // Запас снизу — под центральную кнопку «+» таб-бара.
            Spacer(Modifier.height(32.dp))
        }
    }

    if (isEditNamePresented) {
        AlertDialog(
            onDismissRequest = { isEditNamePresented = false },
            title = { Text(stringResource(R.string.profile_edit_name_title)) },
            text = {
                OutlinedTextField(
                    value = nameDraft,
                    onValueChange = { nameDraft = it },
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true,
                    label = { Text(stringResource(R.string.profile_name)) },
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        isEditNamePresented = false
                        val trimmed = nameDraft.trim()
                        when {
                            trimmed.isEmpty() -> viewModel.showError(nameEmptyError)
                            trimmed != me?.displayName ->
                                viewModel.updateProfile(displayName = trimmed)
                        }
                    },
                ) {
                    Text(stringResource(R.string.common_save))
                }
            },
            dismissButton = {
                TextButton(onClick = { isEditNamePresented = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    if (isLogoutConfirmPresented) {
        // При непустом outbox предупреждаем: выход удалит неотправленные
        // операции (офлайн-кеш и outbox чистятся при разлогине).
        val logoutTitle = if (pendingOutboxCount > 0) {
            pluralStringResource(
                R.plurals.profile_logout_outbox_confirm,
                pendingOutboxCount,
                pendingOutboxCount,
            )
        } else {
            stringResource(R.string.profile_logout_confirm)
        }
        AlertDialog(
            onDismissRequest = { isLogoutConfirmPresented = false },
            title = { Text(logoutTitle) },
            confirmButton = {
                TextButton(
                    onClick = {
                        isLogoutConfirmPresented = false
                        viewModel.logout()
                    },
                ) {
                    Text(
                        text = stringResource(R.string.common_logout),
                        color = Splitty.colors.negative,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { isLogoutConfirmPresented = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    val provider = providerToUnlink
    if (provider != null) {
        AlertDialog(
            onDismissRequest = { providerToUnlink = null },
            title = {
                Text(stringResource(R.string.profile_unlink_confirm_title, provider.title))
            },
            text = { Text(stringResource(R.string.profile_unlink_confirm_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        providerToUnlink = null
                        viewModel.unlink(provider)
                    },
                ) {
                    Text(
                        text = stringResource(R.string.profile_login_method_unlink),
                        color = Splitty.colors.negative,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { providerToUnlink = null }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    if (isDeleteConfirmPresented) {
        AlertDialog(
            onDismissRequest = { isDeleteConfirmPresented = false },
            title = { Text(stringResource(R.string.profile_delete_confirm_title)) },
            text = { Text(stringResource(R.string.profile_delete_confirm_message)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        isDeleteConfirmPresented = false
                        viewModel.deleteAccount()
                    },
                ) {
                    Text(
                        text = stringResource(R.string.profile_delete_account),
                        color = Splitty.colors.negative,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { isDeleteConfirmPresented = false }) {
                    Text(stringResource(R.string.common_cancel))
                }
            },
        )
    }

    // Предупреждение об отвязке Telegram — СВОЙ диалог, не «Ошибка»: отвязка
    // прошла, но человеку важно узнать, что бот заведёт ему отдельный профиль.
    val notice = noticeMessage
    if (notice != null) {
        AlertDialog(
            onDismissRequest = viewModel::dismissNotice,
            title = { Text(stringResource(R.string.profile_notice_title)) },
            text = { Text(notice) },
            confirmButton = {
                TextButton(onClick = viewModel::dismissNotice) {
                    Text(stringResource(R.string.profile_notice_ok))
                }
            },
        )
    }

    val message = errorMessage
    if (message != null) {
        AlertDialog(
            onDismissRequest = viewModel::dismissError,
            title = { Text(stringResource(R.string.common_error_title)) },
            text = { Text(message) },
            confirmButton = {
                TextButton(onClick = viewModel::dismissError) {
                    Text(stringResource(R.string.common_ok))
                }
            },
        )
    }
}

/**
 * Карточка «Способы входа»: по строке на провайдера — привязать/отвязать.
 * Источник истины — `me.linkedProviders` с сервера: локально список не
 * досочиняется, каждая мутация приходит ответом на запрос.
 *
 * Пока профиль не прочитан (`me == null`) секции нет вовсе: рисовать «Не
 * привязан» по пустому списку значило бы соврать про состояние аккаунта.
 */
@Composable
private fun LoginMethodsSection(
    me: Me?,
    enabled: Boolean,
    onLink: () -> Unit,
    onUnlinkRequest: (LoginProvider) -> Unit,
) {
    if (me == null) return
    // Google — всегда: его можно и привязать, и отвязать прямо здесь.
    // Telegram — ТОЛЬКО когда уже привязан: привязка требует Telegram Login
    // Widget (подписанные ботом id/auth_date/hash), которого в приложении нет,
    // и кнопка «Привязать» рядом с ним обещала бы несуществующее.
    // Apple на Android не показываем вовсе: Sign in with Apple здесь без
    // веб-редиректа не работает, а отвязать его можно с iPhone.
    val providers = listOf(LoginProvider.GOOGLE, LoginProvider.TELEGRAM)
        .filter { it != LoginProvider.TELEGRAM || me.isLinked(it) }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            text = stringResource(R.string.profile_login_methods_section),
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        SurfaceCard(
            modifier = Modifier.fillMaxWidth(),
            padding = 0.dp,
        ) {
            providers.forEachIndexed { index, provider ->
                if (index > 0) {
                    HairlineDivider()
                }
                LoginMethodRow(
                    provider = provider,
                    isLinked = me.isLinked(provider),
                    // Кнопка гаснет ДО запроса: сервер ответил бы 409
                    // last_identity, но узнавать о запрете из алерта уже после
                    // действия — плохо.
                    canUnlink = me.canUnlink(provider),
                    enabled = enabled,
                    onLink = onLink,
                    onUnlinkRequest = { onUnlinkRequest(provider) },
                )
            }
        }
        Text(
            // Когда способ входа остался один, объясняем, почему его кнопка
            // «Отвязать» неактивна — иначе она читается как поломка.
            text = if (me.linkedProviders.size <= 1) {
                stringResource(R.string.profile_login_methods_footer_last)
            } else {
                stringResource(R.string.profile_login_methods_footer)
            },
            modifier = Modifier.padding(horizontal = 4.dp),
            fontSize = 12.sp,
            color = Splitty.colors.inkSecondary,
        )
    }
}

/** Строка способа входа: название, статус привязки и действие справа. */
@Composable
private fun LoginMethodRow(
    provider: LoginProvider,
    isLinked: Boolean,
    canUnlink: Boolean,
    enabled: Boolean,
    onLink: () -> Unit,
    onUnlinkRequest: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = provider.title,
                fontSize = 16.sp,
                color = Splitty.colors.ink,
            )
            Text(
                text = if (isLinked) {
                    stringResource(R.string.profile_login_method_linked)
                } else {
                    stringResource(R.string.profile_login_method_not_linked)
                },
                modifier = Modifier.padding(top = 2.dp),
                fontSize = 12.sp,
                color = Splitty.colors.inkSecondary,
            )
        }
        if (isLinked) {
            SoftChip(
                text = stringResource(R.string.profile_login_method_unlink),
                onClick = onUnlinkRequest,
                enabled = enabled && canUnlink,
            )
        } else {
            SoftChip(
                text = stringResource(R.string.profile_login_method_link),
                onClick = onLink,
                enabled = enabled,
            )
        }
    }
}

/**
 * Карточка-кнопка «Удалить аккаунт»: negative-текст, подтверждение диалогом
 * и подпись о том, что расходы и долги в группах остаются.
 */
@Composable
private fun DeleteAccountSection(isDeleting: Boolean, onClick: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SurfaceCard(
            modifier = Modifier.fillMaxWidth(),
            padding = 0.dp,
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(enabled = !isDeleting, onClick = onClick)
                    .padding(16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (isDeleting) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = Splitty.colors.negative,
                    )
                }
                Text(
                    text = stringResource(R.string.profile_delete_account),
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = Splitty.colors.negative,
                    textAlign = TextAlign.Center,
                )
            }
        }
        Text(
            text = stringResource(R.string.profile_delete_account_caption),
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 4.dp),
            fontSize = 12.sp,
            color = Splitty.colors.inkSecondary,
            textAlign = TextAlign.Center,
        )
    }
}

/**
 * Профиль-шапка: аватар 88dp, имя, @username и ID. Пока профиль не загружен —
 * placeholder той же геометрии (круг + скелет имени), чтобы шапка не исчезала
 * и контент не прыгал (паритет iOS redacted-placeholder).
 */
@Composable
private fun HeaderSection(me: Me?) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        if (me != null) {
            GradientAvatar(
                user = User(id = me.id, username = me.username, displayName = me.displayName),
                size = 88.dp,
            )
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    text = me.displayName,
                    fontSize = 24.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = Splitty.colors.ink,
                )
                val username = me.username
                if (!username.isNullOrEmpty()) {
                    Text(
                        text = "@$username",
                        fontSize = 15.sp,
                        color = Splitty.colors.inkSecondary,
                    )
                }
                Text(
                    text = stringResource(R.string.profile_id, me.id),
                    fontSize = 12.sp,
                    color = Splitty.colors.inkSecondary,
                    style = TextStyle(fontFeatureSettings = "tnum"),
                )
            }
        } else {
            Box(
                modifier = Modifier
                    .size(88.dp)
                    .clip(CircleShape)
                    .background(Splitty.colors.hairline),
            )
            Box(
                modifier = Modifier
                    .width(160.dp)
                    .height(24.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(Splitty.colors.hairline),
            )
        }
    }
}

/**
 * Карточка «Настройки»: имя, язык, тема, ссылка на уведомления — с hairline-
 * разделителями. Мастер-тумблер уведомлений живёт на самом экране уведомлений
 * (здесь он дублировал строку-ссылку и путал — убран в паритете с iOS).
 */
@Composable
private fun SettingsSection(
    me: Me?,
    lang: String,
    theme: String,
    enabled: Boolean,
    onEditName: () -> Unit,
    onLangSelected: (String) -> Unit,
    onThemeSelected: (String) -> Unit,
    onOpenNotifications: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            text = stringResource(R.string.profile_settings_section),
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        SurfaceCard(
            modifier = Modifier.fillMaxWidth(),
            padding = 0.dp,
        ) {
            NameRow(name = me?.displayName.orEmpty(), onClick = onEditName)
            HairlineDivider()
            LangRow(lang = lang, enabled = enabled, onLangSelected = onLangSelected)
            HairlineDivider()
            ThemeRow(theme = theme, onThemeSelected = onThemeSelected)
            HairlineDivider()
            NotificationsLinkRow(onClick = onOpenNotifications)
        }
    }
}

/** Строка «Тема»: выпадающее меню системная/светлая/тёмная (DataStore). */
@Composable
private fun ThemeRow(theme: String, onThemeSelected: (String) -> Unit) {
    var isMenuExpanded by remember { mutableStateOf(false) }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { isMenuExpanded = true }
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.profile_theme),
            fontSize = 16.sp,
            color = Splitty.colors.ink,
        )
        Spacer(Modifier.weight(1f))
        Box {
            Text(
                text = stringResource(
                    when (theme) {
                        SessionStore.THEME_LIGHT -> R.string.profile_theme_light
                        SessionStore.THEME_DARK -> R.string.profile_theme_dark
                        else -> R.string.profile_theme_system
                    }
                ),
                fontSize = 16.sp,
                color = Splitty.colors.inkSecondary,
            )
            DropdownMenu(
                expanded = isMenuExpanded,
                onDismissRequest = { isMenuExpanded = false },
            ) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.profile_theme_system)) },
                    onClick = {
                        isMenuExpanded = false
                        onThemeSelected(SessionStore.THEME_SYSTEM)
                    },
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.profile_theme_light)) },
                    onClick = {
                        isMenuExpanded = false
                        onThemeSelected(SessionStore.THEME_LIGHT)
                    },
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.profile_theme_dark)) },
                    onClick = {
                        isMenuExpanded = false
                        onThemeSelected(SessionStore.THEME_DARK)
                    },
                )
            }
        }
        Icon(
            imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
        )
    }
}

/** Строка «Уведомления»: переход к настройкам категорий и каналов. */
@Composable
private fun NotificationsLinkRow(onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.notifications_title),
            fontSize = 16.sp,
            color = Splitty.colors.ink,
        )
        Spacer(Modifier.weight(1f))
        Icon(
            imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
        )
    }
}

/** Строка «Имя»: текущее displayName, тап открывает диалог редактирования. */
@Composable
private fun NameRow(name: String, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.profile_name),
            fontSize = 16.sp,
            color = Splitty.colors.ink,
        )
        Spacer(Modifier.weight(1f))
        Text(
            text = name,
            modifier = Modifier.padding(start = 8.dp),
            fontSize = 16.sp,
            color = Splitty.colors.inkSecondary,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Icon(
            imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
        )
    }
}

/**
 * Строка «Язык»: значение справа, тап — меню ru/en. Под строкой caption —
 * настройка меняет язык сообщений бота в Telegram (объясняем, зачем она здесь).
 */
@Composable
private fun LangRow(lang: String, enabled: Boolean, onLangSelected: (String) -> Unit) {
    var isMenuExpanded by remember { mutableStateOf(false) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(enabled = enabled) { isMenuExpanded = true }
            .padding(horizontal = 16.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.profile_language),
                fontSize = 16.sp,
                color = Splitty.colors.ink,
            )
            Spacer(Modifier.weight(1f))
            Box {
                Text(
                    text = if (lang == "en") {
                        stringResource(R.string.profile_language_en)
                    } else {
                        stringResource(R.string.profile_language_ru)
                    },
                    fontSize = 16.sp,
                    color = Splitty.colors.inkSecondary,
                )
                DropdownMenu(
                    expanded = isMenuExpanded,
                    onDismissRequest = { isMenuExpanded = false },
                ) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.profile_language_ru)) },
                        onClick = {
                            isMenuExpanded = false
                            onLangSelected("ru")
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.profile_language_en)) },
                        onClick = {
                            isMenuExpanded = false
                            onLangSelected("en")
                        },
                    )
                }
            }
            Icon(
                imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = Splitty.colors.inkSecondary.copy(alpha = 0.6f),
            )
        }
        Text(
            text = stringResource(R.string.profile_language_caption),
            modifier = Modifier.padding(top = 2.dp, bottom = 10.dp),
            fontSize = 12.sp,
            color = Splitty.colors.inkSecondary,
        )
    }
}

/** Карточка «Сервер»: текущий base URL (read-only; смена — на экране входа). */
@Composable
private fun ServerSection(baseUrl: String) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SectionHeader(
            text = stringResource(R.string.profile_server_section),
            modifier = Modifier.padding(horizontal = 4.dp),
        )
        SurfaceCard(modifier = Modifier.fillMaxWidth()) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(
                    text = baseUrl,
                    fontSize = 13.sp,
                    color = Splitty.colors.inkSecondary,
                    style = TextStyle(fontFeatureSettings = "tnum"),
                )
                // Версия сборки — чтобы отличать билды на устройствах.
                Text(
                    text = stringResource(
                        R.string.profile_app_version,
                        BuildConfig.VERSION_NAME,
                        BuildConfig.VERSION_CODE,
                    ),
                    fontSize = 13.sp,
                    color = Splitty.colors.inkSecondary,
                    style = TextStyle(fontFeatureSettings = "tnum"),
                )
            }
        }
    }
}

/** Карточка-кнопка «Выйти»: negative-текст, подтверждение диалогом. */
@Composable
private fun LogoutSection(onClick: () -> Unit) {
    SurfaceCard(
        modifier = Modifier.fillMaxWidth(),
        padding = 0.dp,
    ) {
        Text(
            text = stringResource(R.string.common_logout),
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onClick)
                .padding(16.dp),
            fontSize = 16.sp,
            fontWeight = FontWeight.SemiBold,
            color = Splitty.colors.negative,
            textAlign = TextAlign.Center,
        )
    }
}

/** Hairline-разделитель между строками внутри карточки. */
@Composable
private fun HairlineDivider() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp)
            .height(1.dp)
            .background(Splitty.colors.hairline),
    )
}
