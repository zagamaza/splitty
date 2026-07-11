package com.zagir.splitty.ui.activity

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import com.zagir.splitty.R
import java.time.Duration
import java.time.Instant

/**
 * Относительное время для ленты активности — аналог iOS
 * RelativeDateTimeFormatter (unitsStyle .short, ru): «только что»,
 * «5 мин. назад», «2 ч. назад», «3 дн. назад», «2 нед. назад»,
 * «4 мес. назад», «2 года назад».
 */
@Composable
fun relativeTimeText(instant: Instant, now: Instant = Instant.now()): String {
    val seconds = Duration.between(instant, now).seconds.coerceAtLeast(0)
    val minutes = seconds / 60
    val hours = minutes / 60
    val days = hours / 24
    return when {
        minutes < 1 -> stringResource(R.string.time_just_now)
        hours < 1 -> stringResource(R.string.time_minutes_ago, minutes)
        days < 1 -> stringResource(R.string.time_hours_ago, hours)
        days < 7 -> stringResource(R.string.time_days_ago, days)
        days < 30 -> stringResource(R.string.time_weeks_ago, days / 7)
        days < 365 -> stringResource(R.string.time_months_ago, days / 30)
        else -> {
            val years = (days / 365).toInt()
            pluralStringResource(R.plurals.time_years_ago, years, years)
        }
    }
}
