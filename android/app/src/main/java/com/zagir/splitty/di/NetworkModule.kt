package com.zagir.splitty.di

import com.zagir.splitty.BuildConfig
import com.zagir.splitty.core.network.AuthInterceptor
import com.zagir.splitty.core.network.BaseUrlInterceptor
import com.zagir.splitty.core.network.ParseApi
import com.zagir.splitty.core.network.SplittyApi
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import java.util.concurrent.TimeUnit
import javax.inject.Singleton
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.logging.HttpLoggingInterceptor
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {

    /**
     * Плейсхолдер: реальный адрес подставляет [BaseUrlInterceptor] на каждый
     * запрос (сервер настраивается в рантайме на экране входа).
     */
    private const val PLACEHOLDER_BASE_URL = "http://placeholder.invalid/"

    @Provides
    @Singleton
    fun provideOkHttpClient(
        baseUrlInterceptor: BaseUrlInterceptor,
        authInterceptor: AuthInterceptor,
    ): OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .addInterceptor(baseUrlInterceptor)
        .addInterceptor(authInterceptor)
        .apply {
            if (BuildConfig.DEBUG) {
                addInterceptor(
                    HttpLoggingInterceptor().apply {
                        level = HttpLoggingInterceptor.Level.BASIC
                    }
                )
            }
        }
        .build()

    @Provides
    @Singleton
    fun provideRetrofit(client: OkHttpClient, json: Json): Retrofit = Retrofit.Builder()
        .baseUrl(PLACEHOLDER_BASE_URL)
        .client(client)
        .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
        .build()

    @Provides
    @Singleton
    fun provideSplittyApi(retrofit: Retrofit): SplittyApi = retrofit.create(SplittyApi::class.java)

    /**
     * AI-распознавание держит СВОЙ клиент с таймаутом 90с: Gemini на фото/аудио
     * отвечает дольше 30с singleton-клиента, а поднимать общий таймаут нельзя —
     * обычные запросы должны падать быстро. Интерцепторы (адрес сервера + Bearer)
     * те же. writeTimeout тоже 90с — на аплоад крупного фото чека.
     */
    @Provides
    @Singleton
    fun provideParseApi(
        baseUrlInterceptor: BaseUrlInterceptor,
        authInterceptor: AuthInterceptor,
        json: Json,
    ): ParseApi {
        val client = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(90, TimeUnit.SECONDS)
            .writeTimeout(90, TimeUnit.SECONDS)
            .addInterceptor(baseUrlInterceptor)
            .addInterceptor(authInterceptor)
            .apply {
                if (BuildConfig.DEBUG) {
                    addInterceptor(
                        HttpLoggingInterceptor().apply {
                            level = HttpLoggingInterceptor.Level.BASIC
                        }
                    )
                }
            }
            .build()
        return Retrofit.Builder()
            .baseUrl(PLACEHOLDER_BASE_URL)
            .client(client)
            .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
            .build()
            .create(ParseApi::class.java)
    }
}
