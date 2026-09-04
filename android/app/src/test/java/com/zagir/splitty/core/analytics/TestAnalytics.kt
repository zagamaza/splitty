package com.zagir.splitty.core.analytics

import retrofit2.converter.kotlinx.serialization.asConverterFactory
import com.zagir.splitty.core.network.SplittyApi
import com.zagir.splitty.core.session.SessionStore
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import retrofit2.Retrofit

/**
 * Аналитика для тестов, которым она нужна только как зависимость конструктора.
 *
 * Адрес заведомо недоступный: ни один из этих тестов событий не проверяет, а
 * очередь и так живёт на диске. Самодостаточно, чтобы вызывающему не пришлось
 * тащить сюда свой Retrofit — с этого начиналась половина правок в тестах.
 */
fun testAnalytics(
    dir: File,
    json: Json,
    session: SessionStore,
    scope: CoroutineScope,
): Analytics {
    val retrofit = Retrofit.Builder()
        .baseUrl("http://localhost:1/")
        .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
        .build()
    return Analytics(
        AnalyticsQueue(File(dir, "analytics.json"), json),
        retrofit.create(SplittyApi::class.java),
        session,
        scope,
    )
}
