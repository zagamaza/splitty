package com.zagir.splitty.ui.expense

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Плашка распознавания говорит правду об источнике.
 *
 * Раньше после фото чека экран сообщал «Распознано голосом» и следом предлагал
 * добавить фото — то самое, которое человек только что добавил. Подсказка
 * «не то?» вела в тупик: единственный предложенный путь исправления был уже
 * пройден.
 */
class ParseSourceTest {

    @Test
    fun `form starts with voice as the default source`() {
        assertEquals(ParseSource.VOICE, AddExpenseForm(isEditing = false, showsRoomPicker = false).lastParseSource)
    }

    @Test
    fun `photo recognition switches the source`() {
        val form = AddExpenseForm(isEditing = false, showsRoomPicker = false)
            .copy(lastParseSource = ParseSource.PHOTO, didRecognize = true)

        assertEquals(
            ParseSource.PHOTO,
            form.lastParseSource,
            "после фото чека плашка снова скажет «распознано голосом»",
        )
    }
}
