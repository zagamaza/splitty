package com.zagir.splitty.core.review

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Просьба оценить приложение.
 *
 * Ошибка здесь молчит в обе стороны: перекрутили отсечку — не спрашиваем
 * никогда и не узнаём об этом, недокрутили — спрашиваем на каждом расходе.
 * Порт `ReviewPromptTests` из iOS.
 */
class ReviewPromptTest {

    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>

    @BeforeTest
    fun setUp() {
        dir = Files.createTempDirectory("review-prompt").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        dir.deleteRecursively()
    }

    private fun prompt() = ReviewPrompt(dataStore, scope)

    /** Просьба заслужена? Ждём состояние, а не «сколько-нибудь миллисекунд». */
    private suspend fun ReviewPrompt.isEarnedWithin(ms: Long = 2_000): Boolean =
        withTimeoutOrNull(ms) { isEarned.first { it } } != null

    @Test
    fun `single expense does not earn the ask`() = runBlocking {
        val prompt = prompt()

        prompt.note(ReviewPrompt.Moment.EXPENSE_ADDED)

        assertFalse(prompt.isEarnedWithin(300), "просим после первого же расхода — человек ещё ничего не увидел")
    }

    @Test
    fun `three expenses earn the ask`() = runBlocking {
        val prompt = prompt()

        repeat(3) { prompt.note(ReviewPrompt.Moment.EXPENSE_ADDED) }

        assertTrue(prompt.isEarnedWithin(), "три расхода — человек пользуется, а просьба так и не заслужена")
    }

    @Test
    fun `settled debt earns the ask at once`() = runBlocking {
        val prompt = prompt()

        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)

        assertTrue(prompt.isEarnedWithin(), "погашение долга — пик, ради которого приложение и ставили")
    }

    @Test
    fun `same version is not asked twice`() = runBlocking {
        val prompt = prompt()
        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)
        assertTrue(prompt.isEarnedWithin())
        prompt.markAsked()

        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)

        assertFalse(prompt.isEarnedWithin(300), "в одной версии спрашиваем дважды")
    }

    @Test
    fun `counter survives process death`() = runBlocking {
        prompt().note(ReviewPrompt.Moment.EXPENSE_ADDED)
        prompt().note(ReviewPrompt.Moment.EXPENSE_ADDED)
        // Ждём, пока обе записи лягут в DataStore: иначе третий расход сложится
        // с пустым счётчиком и тест соврёт.
        dataStore.data.first { (it[intPreferencesKey("review_weight")] ?: 0) >= 2 }

        val afterRestart = prompt()
        afterRestart.note(ReviewPrompt.Moment.EXPENSE_ADDED)

        assertTrue(
            afterRestart.isEarnedWithin(),
            "счётчик обнуляется перезапуском — до трёх он не дорастёт никогда",
        )
    }

    @Test
    fun `quiet period holds across versions`() = runBlocking {
        val prompt = prompt()
        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)
        assertTrue(prompt.isEarnedWithin())
        prompt.markAsked()
        // Другая версия сборки: отсечка по версии больше не держит, держать
        // должна пауза.
        dataStore.edit { it[stringPreferencesKey("review_asked_version")] = "совсем другая версия" }

        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)

        assertFalse(
            prompt.isEarnedWithin(300),
            "новая версия сбрасывает паузу — просьба уходит в квоту Play впустую",
        )
    }

    @Test
    fun `ask returns after the quiet period`() = runBlocking {
        val prompt = prompt()
        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)
        assertTrue(prompt.isEarnedWithin())
        prompt.markAsked()
        dataStore.edit { prefs ->
            prefs[stringPreferencesKey("review_asked_version")] = "совсем другая версия"
            prefs[longPreferencesKey("review_asked_at")] =
                System.currentTimeMillis() - 121L * 24 * 60 * 60 * 1000
        }

        prompt.note(ReviewPrompt.Moment.DEBT_SETTLED)

        assertTrue(prompt.isEarnedWithin(), "пауза прошла, версия сменилась — а просьба всё равно не заслужена")
    }
}
