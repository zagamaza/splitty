package com.zagir.splitty.ui.onboarding

import android.content.Context

/**
 * Видела ли ЭТА УСТАНОВКА приветствие.
 *
 * Интерфейс, а не класс с Context: JVM-тесты живого Context не поднимают, а
 * подменять здесь нужно ровно две строки.
 */
interface IntroSeenSource {
    fun hasSeen(): Boolean
    fun markSeen()
}

/**
 * Отметка в SharedPreferences — СИНХРОННО, и это главное её свойство.
 *
 * Прежняя отметка жила в DataStore, и оттуда росла гонка: флаг читается с
 * диска, запись туда асинхронна, и пока запись в пути, гейт отдаёт ещё старое
 * «не видел». Под входом это давало всплывающее поверх списка приветствие, а в
 * корне дало бы то же самое поверх экрана входа — человек дочитал, нажал
 * «Начать», и рассказ открылся заново.
 *
 * Гасить такую гонку отдельной защёлкой в памяти можно, но защёлка живёт ровно
 * до пересоздания владельца, а синхронная запись не оставляет окна вовсе.
 * Соседний [com.zagir.splitty.core.analytics.InstallDeviceId] хранится так же и
 * по той же причине.
 *
 * Ключ по установке, а не по аккаунту: приветствие идёт ДО входа, и аккаунта в
 * этот момент не существует.
 */
class SharedPrefsIntroSeen(private val context: Context) : IntroSeenSource {

    override fun hasSeen(): Boolean = prefs().getBoolean(KEY, false)

    override fun markSeen() {
        prefs().edit().putBoolean(KEY, true).apply()
    }

    private fun prefs() = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    private companion object {
        const val PREFS = "splitty.onboarding"
        const val KEY = "intro_seen"
    }
}
