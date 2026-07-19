package com.zagir.splitty.di

import android.content.Context
import android.util.Log
import androidx.datastore.core.DataStore
import androidx.datastore.core.handlers.ReplaceFileCorruptionHandler
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.preferencesDataStore
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.session.KeystoreTokenCipher
import com.zagir.splitty.core.session.TokenCipher
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
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.serialization.json.Json

/** CoroutineScope времени жизни приложения (SessionStore, фоновые записи). */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApplicationScope

// corruptionHandler: без него битый session.preferences_pb даёт CorruptionException
// на КАЖДОМ старте — приложение навсегда остаётся разлогиненным без пути к
// восстановлению. Замена на пустые настройки = разлогин один раз, дальше файл цел.
private val Context.sessionDataStore: DataStore<Preferences> by preferencesDataStore(
    name = "session",
    corruptionHandler = ReplaceFileCorruptionHandler { emptyPreferences() },
)

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
        // CoroutineExceptionHandler обязателен: SupervisorJob защищает соседние
        // корутины от отмены, но НЕ глотает исключения — любой промах в
        // фоновой работе (Keystore, DataStore, декодирование аватара) иначе
        // доходит до дефолтного обработчика потока и убивает процесс.
        CoroutineScope(
            SupervisorJob() + Dispatchers.Default +
                CoroutineExceptionHandler { _, e ->
                    Log.e("Splitty", "необработанная ошибка в application-скоупе", e)
                },
        )

    @Provides
    @Singleton
    fun provideJson(): Json = SplittyJson

    /** Шифрование токена сессии поверх Android Keystore (AES-256-GCM). */
    @Provides
    @Singleton
    fun provideTokenCipher(): TokenCipher = KeystoreTokenCipher()

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
