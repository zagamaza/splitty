package com.zagir.splitty.di

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.preferencesDataStore
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.data.ApiCache
import com.zagir.splitty.data.OutboxStore
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.File
import javax.inject.Qualifier
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.serialization.json.Json

/** CoroutineScope времени жизни приложения (SessionStore, фоновые записи). */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApplicationScope

private val Context.sessionDataStore: DataStore<Preferences> by preferencesDataStore(name = "session")

@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideDataStore(@ApplicationContext context: Context): DataStore<Preferences> =
        context.sessionDataStore

    @Provides
    @Singleton
    @ApplicationScope
    fun provideApplicationScope(): CoroutineScope =
        CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @Provides
    @Singleton
    fun provideJson(): Json = SplittyJson

    /** Офлайн-кеш GET-ответов: JSON-файлы в filesDir/cache-api. */
    @Provides
    @Singleton
    fun provideApiCache(@ApplicationContext context: Context, json: Json): ApiCache =
        ApiCache(File(context.filesDir, "cache-api"), json)

    /** Outbox офлайн-операций: filesDir/outbox.json. */
    @Provides
    @Singleton
    fun provideOutboxStore(@ApplicationContext context: Context, json: Json): OutboxStore =
        OutboxStore(File(context.filesDir, "outbox.json"), json)
}
