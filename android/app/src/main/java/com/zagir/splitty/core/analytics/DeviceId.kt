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
