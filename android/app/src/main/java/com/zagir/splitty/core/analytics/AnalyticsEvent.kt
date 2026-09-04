package com.zagir.splitty.core.analytics

/**
 * Продуктовое событие.
 *
 * Запечатанный тип, а не строки на месте вызова: имя события — проводной
 * контракт с сервером и вторым клиентом, и опечатка в нём ничего не уронит, а
 * молча уведёт шаг воронки в никуда.
 *
 * Набор и допустимые значения — `docs/analytics-events.md`. Он источник правды;
 * новое значение сначала попадает туда, потом сюда и в iOS.
 */
sealed class AnalyticsEvent(val name: String, val params: Map<String, String> = emptyMap()) {
    class AppOpen(cold: Boolean) : AnalyticsEvent("app_open", mapOf("cold" to cold.toString()))
    class LoginCompleted(method: String) : AnalyticsEvent("login_completed", mapOf("method" to method))
    data object OnboardingStarted : AnalyticsEvent("onboarding_started")
    class OnboardingStep(step: String) : AnalyticsEvent("onboarding_step", mapOf("step" to step))
    data object OnboardingCompleted : AnalyticsEvent("onboarding_completed")
    data object OnboardingSkipped : AnalyticsEvent("onboarding_skipped")
    data object RoomCreated : AnalyticsEvent("room_created")
    class RoomJoined(via: String) : AnalyticsEvent("room_joined", mapOf("via" to via))
    class RoomJoinFailed(reason: String) : AnalyticsEvent("room_join_failed", mapOf("reason" to reason))
    class ExpenseAdded(method: String) : AnalyticsEvent("expense_added", mapOf("method" to method))
    class ExpenseParseFailed(reason: String) : AnalyticsEvent("expense_parse_failed", mapOf("reason" to reason))
    data object SettleUpOpened : AnalyticsEvent("settle_up_opened")
    data object SettleUpDone : AnalyticsEvent("settle_up_done")
    class PaywallShown(from: String) : AnalyticsEvent("paywall_shown", mapOf("from" to from))
    class PaywallDismissed(from: String) : AnalyticsEvent("paywall_dismissed", mapOf("from" to from))
    class PurchaseStarted(product: String) : AnalyticsEvent("purchase_started", mapOf("product" to product))
    class PurchaseCompleted(product: String) : AnalyticsEvent("purchase_completed", mapOf("product" to product))
    class PurchaseFailed(reason: String) : AnalyticsEvent("purchase_failed", mapOf("reason" to reason))
    class InviteSent(channel: String) : AnalyticsEvent("invite_sent", mapOf("channel" to channel))
}

/**
 * Причина неудачного входа в тусу — из закрытого множества контракта.
 *
 * Свободный текст ошибки сюда попасть не должен: он не группируется в
 * агрегатах и утаскивает наружу подробности, которых в аналитике быть не может.
 */
fun analyticsJoinReason(code: String?, status: Int?): String = when {
    code == "not_found" -> "not_found"
    code == "room_deleted" || code == "deleted" -> "deleted"
    code == "forbidden" -> "forbidden"
    status == 404 -> "not_found"
    status == 403 -> "forbidden"
    else -> "network"
}

/** Причина неудачного распознавания — тоже из закрытого множества. */
fun analyticsParseReason(code: String?, status: Int?): String = when (code) {
    "quota", "rate_limited", "unsupported_media", "too_large", "validation", "internal" -> code
    else -> if (status != null && status >= 500) "internal" else "validation"
}

/**
 * Продукт подписки в термине контракта. Идентификаторы магазина в аналитику не
 * уезжают: они длинные, платформенные и разъедутся между App Store и Play.
 */
fun analyticsProduct(productId: String): String =
    if (productId.contains("year")) "yearly" else "monthly"
