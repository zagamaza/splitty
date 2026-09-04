package com.zagir.splitty.core.analytics

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Имена и параметры совпадают с контрактом (docs/analytics-events.md).
 *
 * Событие — проводной договор с сервером и вторым клиентом: «почти то же имя»
 * означает потерянный шаг воронки, и заметно это будет только по неправильному
 * числу через месяц.
 */
class AnalyticsContractTest {

    @Test
    fun namesMatchContract() {
        assertEquals("app_open", AnalyticsEvent.AppOpen(true).name)
        assertEquals(mapOf("cold" to "true"), AnalyticsEvent.AppOpen(true).params)
        assertEquals("onboarding_step", AnalyticsEvent.OnboardingStep("who_paid").name)
        assertEquals("room_join_failed", AnalyticsEvent.RoomJoinFailed("not_found").name)
        assertEquals(mapOf("product" to "yearly"), AnalyticsEvent.PurchaseCompleted("yearly").params)
        assertEquals("paywall_dismissed", AnalyticsEvent.PaywallDismissed("quota").name)
    }

    /**
     * Причины — из закрытого множества, а не текст ошибки: свободный текст не
     * группируется в агрегатах и утаскивает наружу подробности.
     */
    @Test
    fun reasonsAreClosedSet() {
        assertEquals("not_found", analyticsJoinReason("not_found", 404))
        assertEquals("forbidden", analyticsJoinReason(null, 403))
        assertEquals("network", analyticsJoinReason(null, null))
        assertEquals("rate_limited", analyticsParseReason("rate_limited", 429))
        assertEquals("internal", analyticsParseReason(null, 500))
        assertEquals("validation", analyticsParseReason(null, 400))
    }

    /** Идентификаторы магазина в аналитику не уезжают: они платформенные. */
    @Test
    fun productNamesAreCrossPlatform() {
        assertEquals("yearly", analyticsProduct("com.zagir.splitty.plus.yearly"))
        assertEquals("monthly", analyticsProduct("com.zagir.splitty.plus.monthly"))
    }
}
