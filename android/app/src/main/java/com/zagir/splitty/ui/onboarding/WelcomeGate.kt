package com.zagir.splitty.ui.onboarding

/**
 * Когда показывать разовое приветствие.
 *
 * Три условия, и каждое взято из живой ситуации:
 * — аккаунт его ещё не видел (ключ по номеру аккаунта, не по устройству);
 * — список групп пуст: у того, кто уже в группах, объяснять нечего;
 * — нет диплинка: человек пришёл по ссылке приглашения в конкретную группу,
 *   и показать ему вместо неё рассказ о продукте — значит потерять переход.
 */
fun shouldShowWelcome(hasSeen: Boolean, groupCount: Int, hasPendingDeeplink: Boolean): Boolean {
    if (hasSeen) return false
    if (groupCount != 0) return false
    return !hasPendingDeeplink
}
