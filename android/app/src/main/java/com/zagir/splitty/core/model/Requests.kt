package com.zagir.splitty.core.model

import com.zagir.splitty.core.money.MINOR_FACTOR
import com.zagir.splitty.core.money.minorToUnitsRounded
import kotlinx.serialization.Serializable

// Тела запросов REST API (контракт v2). null-поля НЕ сериализуются
// (SplittyJson: explicitNulls = false) — режим деления операции сервер
// определяет по наличию ровно одного из полей recipientIds/recipientSums.

/**
 * POST /auth/google — id-токен из Credential Manager. Сервер сам проверяет
 * подпись по JWKS Google и сверяет `aud` со списком GOOGLE_CLIENT_IDS,
 * поэтому клиент не разбирает токен и ничего кроме него не шлёт.
 */
@Serializable
data class GoogleLoginBody(val idToken: String)

/** POST /auth/register — регистрация по email и паролю. */
@Serializable
data class RegisterBody(
    val email: String,
    val password: String,
    val displayName: String,
)

/** POST /auth/login — вход по email и паролю. */
@Serializable
data class PasswordLoginBody(val email: String, val password: String)

/**
 * POST /me/password. [currentPassword] опускается (explicitNulls = false),
 * когда пароля ещё не было: пустая строка означала бы «текущий не сошёлся».
 */
@Serializable
data class SetPasswordBody(
    val currentPassword: String? = null,
    val newPassword: String,
)

/**
 * POST /auth/telegram — payload Telegram Login Widget КАК ЕГО ПОДПИСАЛ Telegram.
 * Сервер пересобирает из полей data-check-string и сверяет `hash`, поэтому
 * значения передаются без правок: любая обрезка или перекодировка ломает подпись.
 */
@Serializable
data class TelegramLoginBody(
    val id: Long,
    val firstName: String? = null,
    val lastName: String? = null,
    val username: String? = null,
    val photoUrl: String? = null,
    val authDate: Long,
    val hash: String,
)

/** POST/DELETE /me/devices — FCM-токен устройства для native-пушей. */
@Serializable
data class DeviceBody(
    val token: String,
    val platform: String = "android",
    /**
     * Язык интерфейса ЭТОГО устройства — на нём приходят пуши. Пустая строка
     * означает «не знаем»; бэкенд отвечает на неё прежним поведением.
     */
    val locale: String = "",
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

/** Считает ли группа копейки. Записанные суммы от этого не меняются. */
@Serializable
data class SetFractionalBody(val fractional: Boolean)

/**
 * Доля получателя в теле запроса by_exact_amount.
 *
 * [sum] — округлённая проекция, её читают сборки сервера без копеек; [sumMinor]
 * — точная величина. null у точной означает целую долю (и записи outbox прежних
 * сборок, где поля просто нет).
 */
@Serializable
data class RecipientSum(
    val userId: Long,
    val sum: Long,
    val sumMinor: Long? = null,
) {
    /** Точная доля: у записей прежних сборок точное значение — целое поле. */
    val exactMinor: Long get() = sumMinor ?: sum * MINOR_FACTOR

    companion object {
        /**
         * Из точной доли: целое поле — её округление ровно по правилу сервера.
         * Иначе пара полей разошлась бы и запрос вернул 400.
         */
        fun ofMinor(userId: Long, minor: Long): RecipientSum =
            RecipientSum(userId = userId, sum = minorToUnitsRounded(minor), sumMinor = minor)
    }
}

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
    val sum: Long,
    /**
     * Точная сумма в МИНОРНЫХ единицах. Сервер предпочитает её, а целое поле
     * выше оставляет для сборок, которые про копейки не знают.
     */
    val sumMinor: Long? = null,
    val donorId: Long,
    val recipientIds: List<Long>? = null,
    val recipientSums: List<RecipientSum>? = null,
    /**
     * Позиции чека itemized-операции (write-path). Passthrough: при правке
     * серверной операции проносятся НЕТРОНУТЫМИ, иначе плоский PUT затрёт чек.
     * null у обычных расходов (не сериализуется — explicitNulls = false).
     */
    val items: List<OperationItem>? = null,
    /**
     * Идемпотентный ключ создания (localId записи outbox): повторный POST
     * с тем же ключом вернёт существующую операцию (200 вместо 201) —
     * защита от дублей при досылке офлайн-операций. В PUT игнорируется.
     */
    val clientOpId: String? = null,
    /**
     * Версия расхода, которую видел редактирующий (только PUT). null — поле не
     * отправляется, и сервер правит безусловно, как до появления версий.
     */
    val version: Int? = null,
) {
    companion object {
        fun of(
            description: String,
            sum: Long,
            sumMinor: Long? = null,
            donorId: Long,
            split: ExpenseSplit,
            items: List<OperationItem>? = null,
            clientOpId: String? = null,
            version: Int? = null,
        ): OperationBody =
            when (split) {
                is ExpenseSplit.Equally -> OperationBody(
                    description = description,
                    sum = sum,
                    sumMinor = sumMinor,
                    donorId = donorId,
                    recipientIds = split.recipientIds,
                    items = items,
                    clientOpId = clientOpId,
                    version = version,
                )

                is ExpenseSplit.ByExactAmount -> OperationBody(
                    description = description,
                    sum = sum,
                    sumMinor = sumMinor,
                    donorId = donorId,
                    recipientSums = split.recipientSums,
                    items = items,
                    clientOpId = clientOpId,
                    version = version,
                )
            }
    }
}

/** POST /rooms/{id}/repayments. */
@Serializable
data class RepaymentBody(
    val debtorId: Long,
    val lenderId: Long,
    /**
     * Сумма погашения в МИНОРНЫХ единицах. Старое целое поле сервер выведет
     * сам; платёж на 20,80 в нём превратился бы в 21.
     */
    val sumMinor: Long,
    /**
     * Идемпотентный ключ: на повтор с тем же ключом сервер возвращает уже
     * созданное погашение. Без него потерянный ответ на ЧАСТИЧНОЕ погашение
     * списывал дважды — проверка переплаты ловит только возврат сверх долга,
     * а два частичных платежа долг не превышают.
     */
    val clientOpId: String,
)

/**
 * POST /users/{id}/aliases — дозапись прозвища участнику после сопоставления
 * нераспознанного имени. Best-effort: сервер нормализует (trim/lower) и пишет
 * только при общей комнате (403 иначе); ответ 204 без тела.
 */
@Serializable
data class AliasBody(val alias: String)
