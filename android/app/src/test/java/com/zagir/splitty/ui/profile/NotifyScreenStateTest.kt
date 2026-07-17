package com.zagir.splitty.ui.profile

import com.zagir.splitty.core.model.ChannelPrefs
import com.zagir.splitty.core.model.NotifySettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Чистые переходы состояния экрана «Уведомления» (NotifyScreenState):
 * каскад мастер-тумблера (категории задизейблены при выключенном/сохранении)
 * и откат с алертом при ошибке PATCH. Логика VM без Android/сети.
 */
class NotifyScreenStateTest {

    private val loaded = NotifyScreenState(
        settings = NotifySettings(
            operations = ChannelPrefs(telegram = true),
            debts = ChannelPrefs(telegram = true),
        ),
        masterOn = true,
    )

    // --- Каскад мастера ---

    @Test
    fun `categories enabled only when master on and not saving`() {
        assertTrue(loaded.categoriesEnabled)
        assertFalse(loaded.copy(masterOn = false).categoriesEnabled)
        assertFalse(loaded.copy(isSaving = true).categoriesEnabled)
    }

    @Test
    fun `applying master off disables categories and enters saving`() {
        val next = loaded.applyMaster(false)
        assertFalse(next.masterOn)
        assertTrue(next.isSaving)
        assertFalse(next.categoriesEnabled)
    }

    @Test
    fun `master saved leaves saving and keeps value`() {
        val next = loaded.applyMaster(false).masterSaved(false)
        assertFalse(next.masterOn)
        assertFalse(next.isSaving)
    }

    // --- Откат с алертом ---

    @Test
    fun `master failure reverts value and raises alert`() {
        val next = loaded.applyMaster(false).masterFailed(previous = true, message = "Нет сети")
        assertTrue(next.masterOn) // откат к прежнему
        assertFalse(next.isSaving)
        assertEquals("Нет сети", next.alertMessage)
    }

    @Test
    fun `categories failure reverts settings and raises alert`() {
        val previous = loaded.settings
        val changed = loaded.settings!!.copy(debts = ChannelPrefs(telegram = false))
        val next = loaded.applyCategories(changed).categoriesFailed(previous, "Не удалось")
        assertEquals(previous, next.settings) // откат к прежним категориям
        assertFalse(next.isSaving)
        assertEquals("Не удалось", next.alertMessage)
    }

    @Test
    fun `categories saved leaves saving with server value`() {
        val changed = loaded.settings!!.copy(debts = ChannelPrefs(telegram = false))
        val next = loaded.applyCategories(changed).categoriesSaved(changed)
        assertEquals(changed, next.settings)
        assertFalse(next.isSaving)
    }

    // --- Служебные производные ---

    @Test
    fun `loading only while empty and no error`() {
        assertTrue(NotifyScreenState().isLoading)
        assertFalse(loaded.isLoading)
        assertFalse(NotifyScreenState(loadError = "boom").isLoading)
    }

    @Test
    fun `no alert on freshly loaded state`() {
        assertNull(loaded.alertMessage)
    }
}
