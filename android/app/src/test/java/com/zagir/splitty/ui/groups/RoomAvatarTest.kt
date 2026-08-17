package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SplittyJson
import kotlin.test.Test
import kotlin.test.assertEquals
import java.io.File
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Фото группы: разбор ссылки на картинку.
 *
 * Ключа `avatarFileId` в ответе может не быть вовсе (omitempty на сервере и
 * старые списки в офлайн-кеше) — разбор обязан это пережить, иначе падает весь
 * список групп, а не одна ава.
 */
class RoomAvatarTest {

    private val roomWithoutAvatar = """
        {"id":"1","name":"Стамбул","createdAt":"2026-08-17T10:00:00Z","isArchived":false,
         "members":[],"memberCount":0,"currency":"RUB","totalSpent":0,"myBalance":0}
    """.trimIndent()

    private val roomWithAvatar = """
        {"id":"1","name":"Стамбул","createdAt":"2026-08-17T10:00:00Z","isArchived":false,
         "members":[],"memberCount":0,"currency":"RUB","totalSpent":0,"myBalance":0,
         "avatarFileId":"65a0000000000000000000ff"}
    """.trimIndent()

    @Test
    fun `комната без фото разбирается`() {
        val room = SplittyJson.decodeFromString(RoomSummary.serializer(), roomWithoutAvatar)
        assertNull(room.avatarFileId)
    }

    @Test
    fun `ссылка на фото попадает в модель`() {
        val room = SplittyJson.decodeFromString(RoomSummary.serializer(), roomWithAvatar)
        assertEquals("65a0000000000000000000ff", room.avatarFileId)
    }

    /**
     * Аватар группы НЕ имеет права грузить фото профиля по своему id: id
     * группы — это хэш строки, он попадает в диапазон настоящих telegram id, и
     * при совпадении группа показывала бы фото ПОСТОРОННЕГО человека. На iOS от
     * этого стоит явная защита (`avatarUserId: nil`), на Android её не было.
     *
     * Проверяем исходник: поведение прячется в аргументе компонента, и
     * подсунуть сюда фейковое хранилище дороже, чем оно того стоит.
     */
    @Test
    fun `аватар группы не грузит фото по хэшу id`() {
        val source = File("src/main/java/com/zagir/splitty/ui/groups/GroupsCommon.kt").readText()
        val body = source.substringAfter("internal fun GroupAvatar(").substringBefore("\n}")
        assertTrue(
            body.contains("loadsPhoto = false"),
            "GroupAvatar снова грузит фото по id группы — это чужой аватар",
        )
    }
}
