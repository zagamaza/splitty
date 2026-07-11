package com.zagir.splitty.core.model

import kotlinx.serialization.Serializable

// Тела запросов REST API (контракт v2). null-поля НЕ сериализуются
// (SplittyJson: explicitNulls = false) — режим деления операции сервер
// определяет по наличию ровно одного из полей recipientIds/recipientSums.

/** POST /auth/code. */
@Serializable
data class CodeLoginBody(val code: String)

/** POST /auth/dev (только при API_DEV_AUTH=true на сервере). */
@Serializable
data class DevLoginBody(
    val userId: Long,
    val displayName: String,
    val username: String? = null,
)

/** PATCH /me — только изменяемые поля. */
@Serializable
data class UpdateMeBody(
    val displayName: String? = null,
    val lang: String? = null,
    val notificationOn: Boolean? = null,
)

/** POST /rooms. */
@Serializable
data class CreateRoomBody(val name: String)

/** PUT /rooms/{id}/currency. */
@Serializable
data class SetCurrencyBody(val currency: String)

/** Доля получателя в теле запроса by_exact_amount (целые единицы валюты). */
@Serializable
data class RecipientSum(
    val userId: Long,
    val sum: Int,
)

/** Способ деления расхода в теле POST/PUT операции. */
sealed interface ExpenseSplit {
    /**
     * Поровну между [recipientIds]: сервер раскладывает доли канонически
     * (base = S/n, остаток по единице первым получателям массива).
     */
    data class Equally(val recipientIds: List<Long>) : ExpenseSplit

    /** Точными суммами: сервер валидирует Σ сумм == сумме операции (400 иначе). */
    data class ByExactAmount(val recipientSums: List<RecipientSum>) : ExpenseSplit
}

/**
 * Тело POST/PUT операции: ровно ОДНО из полей recipientIds/recipientSums
 * (null-поле не сериализуется — режим определяет сервер по наличию поля).
 */
@Serializable
data class OperationBody(
    val description: String,
    val sum: Int,
    val donorId: Long,
    val recipientIds: List<Long>? = null,
    val recipientSums: List<RecipientSum>? = null,
    /**
     * Идемпотентный ключ создания (localId записи outbox): повторный POST
     * с тем же ключом вернёт существующую операцию (200 вместо 201) —
     * защита от дублей при досылке офлайн-операций. В PUT игнорируется.
     */
    val clientOpId: String? = null,
) {
    companion object {
        fun of(
            description: String,
            sum: Int,
            donorId: Long,
            split: ExpenseSplit,
            clientOpId: String? = null,
        ): OperationBody =
            when (split) {
                is ExpenseSplit.Equally -> OperationBody(
                    description = description,
                    sum = sum,
                    donorId = donorId,
                    recipientIds = split.recipientIds,
                    clientOpId = clientOpId,
                )

                is ExpenseSplit.ByExactAmount -> OperationBody(
                    description = description,
                    sum = sum,
                    donorId = donorId,
                    recipientSums = split.recipientSums,
                    clientOpId = clientOpId,
                )
            }
    }
}

/** POST /rooms/{id}/repayments. */
@Serializable
data class RepaymentBody(
    val debtorId: Long,
    val lenderId: Long,
    val sum: Int,
)
