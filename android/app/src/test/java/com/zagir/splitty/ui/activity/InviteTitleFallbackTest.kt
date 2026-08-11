package com.zagir.splitty.ui.activity

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import kotlin.test.Test
import kotlin.test.assertEquals
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Плейсхолдер имени пригласившего заполняет слот ПОДЛЕЖАЩЕГО в «%1$s добавил
 * вас в «%2$s»»: сервер оставляет `inviterName` пустым, если строку
 * пригласившего прочитать не удалось. Прежнее «Вас» давало «Вас добавил вас в
 * группу» — на обоих клиентах.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "ru")
class InviteTitleFallbackTest {

    @Test
    fun `russian fallback reads as a subject`() {
        val context: Context = ApplicationProvider.getApplicationContext()
        val who = context.getString(R.string.invite_someone)

        assertEquals(
            "Кто-то добавил вас в «Дача»",
            context.getString(R.string.invite_added_you, who, "Дача"),
        )
        assertEquals(
            "Кто-то приглашает вас вернуться в «Дача»",
            context.getString(R.string.invite_wants_you_back, who, "Дача"),
        )
    }
}
