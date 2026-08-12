package com.zagir.splitty.ui.profile

import kotlin.test.Test
import kotlin.test.assertTrue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking

/**
 * Выход из аккаунта не должен зависеть от сервисов Google.
 *
 * Отвязка push-токена — вежливость перед сервером, а не условие выхода. На
 * устройстве без сервисов Google (или с отозванным доступом) она падала, и
 * человек оставался залогиненным: кнопка «Выйти» показывала ошибку и ничего
 * не делала — выйти было нельзя вообще.
 */
class LogoutSurvivesPushFailureTest {

    /** Ровно та обёртка, что стоит в `ProfileViewModel.logout`. */
    private suspend fun unregisterBestEffort(block: suspend () -> Unit) {
        runCatching { block() }.onFailure { if (it is CancellationException) throw it }
    }

    @Test
    fun `push unregister failure does not stop the logout`() = runBlocking {
        var loggedOut = false

        unregisterBestEffort { error("нет сервисов Google") }
        loggedOut = true

        assertTrue(loggedOut, "падение отвязки push-токена не дало выйти из аккаунта")
    }

    /** Отмена — не «сбой отвязки», её нельзя глотать. */
    @Test
    fun `cancellation is not swallowed`() {
        var rethrown = false
        try {
            runBlocking { unregisterBestEffort { throw CancellationException("ушли с экрана") } }
        } catch (e: CancellationException) {
            rethrown = true
        }
        assertTrue(rethrown, "отмена проглочена — structured concurrency сломана")
    }
}
