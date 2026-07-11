package com.zagir.splitty.ui.expense

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Политика офлайн-редактирования расходов (фиксированный дизайн v1):
 * создание и правка неотправленных записей outbox доступны всегда,
 * правка синхронизированной (серверной) операции — только онлайн.
 */
class OfflineEditPolicyTest {

    @Test
    fun `creating expense works offline - goes to outbox`() {
        assertTrue(canSaveExpenseOffline(isEditingSyncedOperation = false, isOnline = false))
    }

    @Test
    fun `editing local outbox entry works offline`() {
        // Локальная правка — это не синхронизированная операция.
        assertTrue(canSaveExpenseOffline(isEditingSyncedOperation = false, isOnline = false))
    }

    @Test
    fun `editing synced operation is blocked offline`() {
        assertFalse(canSaveExpenseOffline(isEditingSyncedOperation = true, isOnline = false))
    }

    @Test
    fun `everything works online`() {
        assertTrue(canSaveExpenseOffline(isEditingSyncedOperation = true, isOnline = true))
        assertTrue(canSaveExpenseOffline(isEditingSyncedOperation = false, isOnline = true))
    }
}
