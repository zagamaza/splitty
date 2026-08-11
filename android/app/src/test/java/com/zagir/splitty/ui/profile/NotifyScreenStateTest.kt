package com.zagir.splitty.ui.profile

import com.zagir.splitty.core.ui.UiText
import com.zagir.splitty.R
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
            invites = ChannelPrefs(telegram = true),
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
        assertEquals(false, next.masterOn)
        assertTrue(next.isSaving)
        assertFalse(next.categoriesEnabled)
    }

    @Test
    fun `master saved leaves saving and keeps value`() {
        val next = loaded.applyMaster(false).masterSaved(false)
        assertEquals(false, next.masterOn)
        assertFalse(next.isSaving)
    }

    // --- Откат с алертом ---

    @Test
    fun `master failure reverts value and raises alert`() {
        val next = loaded.applyMaster(false).masterFailed(previous = true, message = UiText.Raw("Нет сети"))
        assertEquals(true, next.masterOn) // откат к прежнему
        assertFalse(next.isSaving)
        assertEquals(UiText.Raw("Нет сети"), next.alertMessage)
    }

    @Test
    fun `categories failure reverts settings and raises alert`() {
        val previous = loaded.settings
        val changed = loaded.settings!!.copy(debts = ChannelPrefs(telegram = false))
        val next = loaded.applyCategories(changed).categoriesFailed(previous, UiText.Raw("Не удалось"))
        assertEquals(previous, next.settings) // откат к прежним категориям
        assertFalse(next.isSaving)
        assertEquals(UiText.Raw("Не удалось"), next.alertMessage)
    }

    @Test
    fun `invites toggle changes only its own category`() {
        val changed = loaded.settings!!.copy(invites = ChannelPrefs(telegram = false, push = true))
        val next = loaded.applyCategories(changed).categoriesSaved(changed)
        assertEquals(ChannelPrefs(telegram = false, push = true), next.settings!!.invites)
        assertEquals(loaded.settings!!.operations, next.settings!!.operations)
        assertEquals(loaded.settings!!.debts, next.settings!!.debts)
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
        assertFalse(NotifyScreenState(loadError = UiText.Raw("boom")).isLoading)
    }

    @Test
    fun `no alert on freshly loaded state`() {
        assertNull(loaded.alertMessage)
    }

    @Test
    fun `master is untouchable while the profile is unknown`() {
        val unknown = loaded.copy(masterOn = null)

        // Утверждать «включено», не прочитав профиль, нельзя: у человека с
        // notificationOn = false экран показывал бы обратное и пускал бы
        // трогать категории.
        assertNull(unknown.masterOn)
        assertFalse(unknown.masterEnabled, "тумблер активен до загрузки профиля")
        assertFalse(unknown.categoriesEnabled, "категории активны при неизвестном мастере")
    }
}
