package com.zagir.splitty.ui.components

import android.os.Build
import android.view.HapticFeedbackConstants
import android.view.View
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalView

// Порт ios/Splitty/Core/DesignSystem.swift → enum Haptics. iOS кэширует
// генераторы (создание на каждое нажатие задерживало первый кадр); в Android
// тактильный отклик идёт через View.performHapticFeedback — сам движок кэширует
// вибромотор, отдельная подготовка не нужна. Контракт применения тот же, что в
// iOS: tap на выбор чипов/фильтров, success на сохранение/платёж, warning на
// тап по заблокированной кнопке (нудж).

/**
 * Тактильный отклик. Интерфейс, а не класс: машина состояний голосового ввода
 * (VoiceController) проверяет контракт откликов юнит-тестами на JVM, где View нет.
 */
interface Haptics {
    /** Лёгкий отклик на выбор/переключение (чипы, radio, чекбоксы). */
    fun tap()

    /** Успешное сохранение/платёж/создание. */
    fun success()

    /** Действие недоступно/требует внимания (тап по заблокированной кнопке). */
    fun warning()
}

/** Боевая реализация: отклик через текущий [View] (он владеет вибромотором). */
class ViewHaptics(private val view: View) : Haptics {
    override fun tap() {
        view.performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP)
    }

    override fun success() {
        // CONFIRM (API 30+) — «успех»; на старых версиях лёгкий VIRTUAL_KEY.
        val constant = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            HapticFeedbackConstants.CONFIRM
        } else {
            HapticFeedbackConstants.VIRTUAL_KEY
        }
        view.performHapticFeedback(constant)
    }

    override fun warning() {
        // REJECT (API 30+) — «отказ»; на старых версиях LONG_PRESS как «стоп».
        val constant = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            HapticFeedbackConstants.REJECT
        } else {
            HapticFeedbackConstants.LONG_PRESS
        }
        view.performHapticFeedback(constant)
    }
}

/** Хелпер для доступа к [Haptics] из composable: `val haptics = rememberHaptics()`. */
@Composable
fun rememberHaptics(): Haptics {
    val view = LocalView.current
    return remember(view) { ViewHaptics(view) }
}
