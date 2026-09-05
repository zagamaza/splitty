package com.zagir.splitty

import com.zagir.splitty.core.analytics.AnalyticsEvent
import com.zagir.splitty.core.analytics.Analytics
import android.app.Application
import android.content.res.Configuration
import com.zagir.splitty.push.PushTokenRegistrar
import com.zagir.splitty.push.SplittyMessagingService
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject

@HiltAndroidApp
class SplittyApp : Application() {

    @Inject
    lateinit var pushTokenRegistrar: PushTokenRegistrar

    @Inject
    lateinit var analytics: Analytics

    override fun onCreate() {
        super.onCreate()
        // Каналы уведомлений — заранее (нужны и для фоновых системных пушей).
        SplittyMessagingService.ensureChannels(this)
        // Регистрация FCM-токена на бэкенде: логин → register, ретрай при старте.
        pushTokenRegistrar.start()
        // Холодный старт — новая сессия событий.
        analytics.startSession()
        analytics.track(AnalyticsEvent.AppOpen(cold = true))
    }

    /**
     * Смена языка системы. Процесс при ней не умирает — активити пересоздаются,
     * а Application живёт дальше, — поэтому без этого места токен оставался
     * зарегистрированным со старым языком и пуши продолжали приходить на нём до
     * следующего убийства процесса.
     */
    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        pushTokenRegistrar.onLocaleMaybeChanged()
    }
}
