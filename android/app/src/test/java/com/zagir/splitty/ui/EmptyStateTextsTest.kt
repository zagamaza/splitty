package com.zagir.splitty.ui

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.zagir.splitty.R
import kotlin.test.Test
import kotlin.test.assertFalse
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Пустые состояния объясняют, а не отрицают.
 *
 * Пустой экран — единственное место, где человек точно читает текст, и тратить
 * его на констатацию пустоты расточительно. Для новичка «Пока нет друзей» ещё и
 * первая фраза приложения, ссылающаяся на незнакомое ему понятие.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class EmptyStateTextsTest {

    private val context: Context = ApplicationProvider.getApplicationContext()

    private val titles = listOf(
        R.string.groups_empty_title,
        R.string.friends_empty_title,
        R.string.activity_empty_title,
        R.string.group_empty_ops_title,
    )

    @Test
    fun `no empty state starts with a denial`() {
        titles.forEach { res ->
            val text = context.getString(res)
            assertFalse(
                text.startsWith("Пока нет") || text.startsWith("Нет ") || text.startsWith("No "),
                "пустое состояние снова начинается с отрицания: «$text»",
            )
        }
    }
}
