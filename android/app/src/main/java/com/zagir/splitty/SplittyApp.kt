package com.zagir.splitty

import android.app.Application
import com.zagir.splitty.push.PushTokenRegistrar
import com.zagir.splitty.push.SplittyMessagingService
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject

@HiltAndroidApp
class SplittyApp : Application() {

    @Inject
    lateinit var pushTokenRegistrar: PushTokenRegistrar

    override fun onCreate() {
        super.onCreate()
        // Каналы уведомлений — заранее (нужны и для фоновых системных пушей).
        SplittyMessagingService.ensureChannels(this)
        // Регистрация FCM-токена на бэкенде: логин → register, ретрай при старте.
        pushTokenRegistrar.start()
    }
}
