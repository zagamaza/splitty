package com.zagir.splitty.ui.groups

import com.zagir.splitty.core.model.FriendBalance
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.model.User
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

/** Кого шит приглашения показывает, а кого нет. */
class InviteCandidatesTest {

    private fun friend(id: Long, deleted: Boolean = false) =
        FriendBalance(user = User(id = id, displayName = "Друг $id", deleted = deleted))

    @Test
    fun `members and deleted accounts are not offered`() {
        val candidates = inviteCandidates(
            friends = listOf(friend(1), friend(2), friend(3, deleted = true)),
            memberIds = setOf(2L),
        )
        // Удалённый аккаунт — обезличенная запись: приглашение ему вернулось бы
        // 404, а в списке друзей он остаётся ради общих расходов.
        assertEquals(listOf(1L), candidates.map { it.user.id })
    }

    @Test
    fun `user without the flag parses as not deleted`() {
        // Сервер шлёт поле только когда true — старые ответы обязаны читаться.
        val user = SplittyJson.decodeFromString<User>("""{"id":7,"displayName":"Аня"}""")
        assertFalse(user.deleted)
    }
}
