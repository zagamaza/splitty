package com.zagir.splitty.ui.components

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.sp
import com.zagir.splitty.R
import com.zagir.splitty.core.model.DataFreshness
import com.zagir.splitty.ui.activity.relativeTimeText
import com.zagir.splitty.ui.theme.Splitty

/**
 * Подпись о свежести показанных данных.
 *
 * Признак «данные из кеша» считался на всех экранах, но выводился только в
 * списке групп: на карточке группы, друзьях и в активности человек смотрел на
 * старые суммы, ничего об этом не зная, — и «неправильный» баланс выглядел
 * ошибкой расчёта, а не отсутствием связи. Текст один на все экраны: правило
 * одно, и узнавать его в разных формулировках человек не должен.
 */
@Composable
fun CacheNote(freshness: DataFreshness, modifier: Modifier = Modifier, tag: String = "cache_note") {
    if (!freshness.fromCache) return
    Text(
        // Времени обновления нет — данные из прошлого запуска, и врать
        // «обновлялись только что» нельзя.
        text = freshness.updatedAt?.let {
            stringResource(R.string.groups_cached_updated, relativeTimeText(it))
        } ?: stringResource(R.string.groups_cached_no_connection),
        fontSize = 12.5.sp,
        color = Splitty.colors.inkSecondary,
        modifier = modifier.testTag(tag),
    )
}
