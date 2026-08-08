package com.zagir.splitty.core.auth

import com.zagir.splitty.core.ui.UiText
import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.zagir.splitty.R

/**
 * Активити из дерева Compose-контекстов: `LocalContext` внутри диалога или
 * кастомного `ContextWrapper` — не активити, а [GoogleIdTokenProvider] без неё
 * не покажет системный лист выбора аккаунта.
 *
 * Живёт рядом с провайдером, а не на экране: активити нужна и экрану входа
 * (Task 18), и секции «Способы входа» в профиле (Task 21) — второй копии
 * разворачивания обёрток заводить незачем.
 *
 * `LocalActivity` появился только в activity-compose 1.10 (у нас 1.9.3).
 */
fun Context.findActivity(): Activity? {
    var current: Context? = this
    while (current is ContextWrapper) {
        if (current is Activity) return current
        current = current.baseContext
    }
    return null
}

/**
 * Активити для системного листа Credential Manager вместе с готовым текстом на
 * случай её отсутствия — см. [rememberCredentialManagerHost].
 */
class CredentialManagerHost(
    private val activity: Activity?,
    private val noActivityMessage: UiText,
) {
    /**
     * Зовёт [action] с активити; её нет — отдаёт человеческий текст в [onError].
     *
     * Тихий no-op недопустим на обоих экранах: на входе не работала бы
     * единственная кнопка для человека без Telegram, в профиле — «Привязать»
     * (в iOS этому соответствует `GoogleSignInError.noPresenter`).
     */
    fun launch(onError: (UiText) -> Unit, action: (Activity) -> Unit) {
        if (activity != null) action(activity) else onError(noActivityMessage)
    }
}

/**
 * Хост системного листа для экрана: активити из дерева контекстов плюс строка
 * ошибки, вычитанная ЗАРАНЕЕ — `stringResource` внутри лямбды-обработчика не
 * вызвать (она не `@Composable`).
 *
 * Общий для экрана входа и профиля: у обоих Credential Manager рисует лист
 * поверх активити, и application-контекст ему не подходит.
 */
@Composable
fun rememberCredentialManagerHost(): CredentialManagerHost {
    val context = LocalContext.current
    val noActivityMessage = UiText.res(R.string.google_sign_in_no_activity)
    return remember(context, noActivityMessage) {
        CredentialManagerHost(context.findActivity(), noActivityMessage)
    }
}
