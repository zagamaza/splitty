package com.zagir.splitty.core.auth

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper

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
