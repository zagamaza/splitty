package com.zagir.splitty.core.analytics

import android.content.Context
import java.util.UUID

/**
 * Идентификатор УСТАНОВКИ приложения для событий до входа.
 *
 * Интерфейс, а не класс с Context: JVM-тесты живого Context не поднимают, а
 * подменять там нужно ровно одну строку.
 */
fun interface DeviceIdSource {
    fun get(): String
}

/**
 * Значение живёт в SharedPreferences и переживает выход из аккаунта: оно про
 * приложение на телефоне, а не про человека. Переустановка даёт новое — так и
 * надо, это ровно та единица, которую считает воронка «поставил → вошёл».
 *
 * ANDROID_ID сюда не годится: он переживает переустановку и общий для всех
 * приложений с одной подписью, то есть это идентификатор устройства, а нам
 * нужен идентификатор установки.
 */
class InstallDeviceId(private val context: Context) : DeviceIdSource {

    override fun get(): String {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs.getString(KEY, null)?.let { return it }
        val fresh = UUID.randomUUID().toString()
        prefs.edit().putString(KEY, fresh).apply()
        return fresh
    }

    private companion object {
        const val PREFS = "splitty.analytics"
        const val KEY = "device"
    }
}

/**
 * Запущено ли приложение на тестовом устройстве Google, а не у человека.
 *
 * Play гоняет каждую выложенную сборку роботом (pre-launch report на Firebase
 * Test Lab): запускает, стирает данные, запускает снова. Каждый запуск давал
 * новый идентификатор установки и обезличенный app_open — к 28.09.2026 это
 * почти 8 тысяч «устройств» при нуле живых Android-пользователей, и верх
 * воронки на Android был мусором на 99%.
 *
 * Интерфейс, а не функция с Context, по той же причине, что и [DeviceIdSource]:
 * JVM-тесты живого Context не поднимают.
 */
fun interface RobotDeviceSource {
    fun isRobot(): Boolean
}

/**
 * Признак документирован Google: на устройствах Test Lab системная настройка
 * `firebase.test.lab` равна "true". Сбой чтения считаем «человеком» — лучше
 * пропустить робота, чем замолчать у живого.
 */
class TestLabRobotDevice(private val context: Context) : RobotDeviceSource {
    override fun isRobot(): Boolean = runCatching {
        android.provider.Settings.System.getString(context.contentResolver, "firebase.test.lab") == "true"
    }.getOrDefault(false)
}
