package com.zagir.splitty.core.review

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import com.zagir.splitty.BuildConfig
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * Просьба оценить приложение в Play.
 *
 * Копит удачные моменты и решает, заслужена ли просьба; показывает
 * [com.zagir.splitty.MainActivity] — системному листу нужна Activity, а моменты
 * случаются во ViewModel. Порт iOS `ReviewPrompt`.
 *
 * Счётчик живёт в DataStore, а не в памяти: между вторым и третьим расходом
 * процесс успевает умереть, и in-memory счётчик не дорос бы никогда.
 */
@Singleton
class ReviewPrompt @Inject constructor(
    private val dataStore: DataStore<Preferences>,
    @ApplicationScope private val scope: CoroutineScope,
) {
    /** Что именно случилось хорошего. */
    enum class Moment(val weight: Int) {
        /** Долг погашен — ради этой минуты приложение и ставили. */
        DEBT_SETTLED(REQUIRED_WEIGHT),
        EXPENSE_ADDED(1),
    }

    private val _isEarned = MutableStateFlow(false)

    /**
     * Просьба заслужена и ждёт показа; гасится в [markAsked].
     *
     * Липкое состояние, а не событие: момент случается на закрывающемся экране,
     * и разовое событие, испущенное на миг раньше подписки Activity, пропало бы
     * вместе с уже потраченным моментом.
     */
    val isEarned: StateFlow<Boolean> = _isEarned.asStateFlow()

    /**
     * Пишем в скоупе приложения, а не вызывающей ViewModel: экран погашения
     * закрывается ровно в этот момент, и его скоуп отменил бы запись.
     */
    fun note(moment: Moment) {
        scope.launch {
            var isEarned = false
            dataStore.edit { prefs ->
                val weight = (prefs[KEY_WEIGHT] ?: 0) + moment.weight
                prefs[KEY_WEIGHT] = weight
                isEarned = weight >= REQUIRED_WEIGHT && canAsk(prefs)
            }
            if (isEarned) _isEarned.value = true
        }
    }

    /**
     * Вызывается ДО показа листа: если процесс умрёт между вызовом и показом,
     * человек не увидит просьбу — это лучше, чем показать её дважды.
     */
    suspend fun markAsked() {
        _isEarned.value = false
        dataStore.edit { prefs ->
            prefs[KEY_WEIGHT] = 0
            prefs[KEY_ASKED_VERSION] = BuildConfig.VERSION_NAME
            prefs[KEY_ASKED_AT] = System.currentTimeMillis()
        }
    }

    private fun canAsk(prefs: Preferences): Boolean {
        if (prefs[KEY_ASKED_VERSION] == BuildConfig.VERSION_NAME) return false
        val last = prefs[KEY_ASKED_AT] ?: return true
        return System.currentTimeMillis() - last >= QUIET_PERIOD_MS
    }

    companion object {
        private const val REQUIRED_WEIGHT = 3

        /**
         * Пауза между просьбами. Play режет показы квотой сам, но молча: без
         * своей отсечки мы бы считали показанным то, чего человек не видел.
         */
        private const val QUIET_PERIOD_MS = 120L * 24 * 60 * 60 * 1000

        private val KEY_WEIGHT = intPreferencesKey("review_weight")
        private val KEY_ASKED_VERSION = stringPreferencesKey("review_asked_version")
        private val KEY_ASKED_AT = longPreferencesKey("review_asked_at")
    }
}
