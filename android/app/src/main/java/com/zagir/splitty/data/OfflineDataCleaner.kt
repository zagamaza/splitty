package com.zagir.splitty.data

import android.util.Log
import com.zagir.splitty.core.analytics.Analytics
import com.zagir.splitty.core.analytics.AnalyticsQueue
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionEndReason
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

private const val TAG = "OfflineDataCleaner"

/**
 * Очистка офлайн-данных при выходе: наблюдает за сессией и на переходе
 * «токен был → токена нет» (кнопка «Выйти» и глобальный разлогин по 401)
 * стирает офлайн-кеш GET-ответов и outbox — данные аккаунта не должны
 * пережить сессию. Создаётся при старте (инжектится в MainActivity).
 *
 * Единственное исключение — отложенное вступление по ссылке-приглашению: оно
 * переживает ПРОТУХШУЮ сессию (401), потому что человек вернётся тем же
 * аккаунтом. См. [SessionEndReason]. Что вернётся ИМЕННО ТОТ ЖЕ, проверяется по
 * персистентному владельцу намерения ([PendingJoinStore.reconcileOwner]): любой
 * признак в памяти теряется вместе с процессом, а между протуханием сессии и
 * следующим входом процесс умирает штатно (вход уводит в системный лист).
 *
 * Приглашение переживает и СМЕНУ аккаунта — но только своё: там оно не
 * стирается скопом, а сводится с вошедшим ([PendingJoinStore.reconcileOwner]).
 * Слепая чистка убивала приглашение самого гостя, потому что известный владелец
 * (`lastUserId`) переживает явный выход, и «A вышел → гость B открыл ссылку →
 * B вошёл» ничем не отличалось от прихода чужого человека.
 *
 * Второй триггер — СМЕНА аккаунта: если id профиля изменился, а перехода
 * «был → нет» мы не увидели (нерасшифровываемый шифротокен держит
 * hasStoredToken=true вечно), данные предыдущего пользователя иначе
 * достались бы новому. Проверка живёт здесь, а не в SessionStore: слой
 * сессии не должен знать про data-слой (иначе цикл в Hilt), а всё нужное —
 * `session.me?.id` — и так есть в наблюдаемом состоянии.
 */
@Singleton
class OfflineDataCleaner @Inject constructor(
    private val sessionStore: SessionStore,
    private val cache: ApiCache,
    private val outbox: OutboxStore,
    private val analyticsQueue: AnalyticsQueue,
    private val analytics: Analytics,
    private val avatars: AvatarStore,
    private val pendingJoin: PendingJoinStore,
    @ApplicationScope scope: CoroutineScope,
) {
    init {
        scope.launch {
            var hadToken = false
            var lastUserId: Long? = null
            // Кому уже сводили отложенное вступление в этом процессе. Нужен,
            // чтобы не дёргать транзакцию DataStore на каждой эмиссии сессии:
            // владелец намерения меняется только при входе, а на холодном
            // старте первая же эмиссия сведёт его заново.
            var reconciledUserId: Long? = null
            // Свести отложенное вступление с вошедшим: чужое (владелец другой)
            // выбрасывается, ничьё (ссылку открыл гость) достаётся ему.
            suspend fun reconcileJoinOwner(userId: Long) {
                if (reconciledUserId == userId) return
                val ok = runCatching { pendingJoin.reconcileOwner(userId) }
                    .onFailure { Log.e(TAG, "не удалось свести приглашение с владельцем", it) }
                    .isSuccess
                // Только при успехе: иначе повторим на следующей эмиссии.
                if (ok) reconciledUserId = userId
            }
            sessionStore.state.collect { session ->
                if (session == null) return@collect // DataStore ещё читается
                // Сбой чтения DataStore отдаёт пустую сессию-заглушку, неотличимую
                // от разлогина. Пропускаем её целиком (в т.ч. не трогаем hadToken):
                // иначе один IO-сбой или CorruptionException необратимо стирал
                // очередь неотправленных расходов при живых ключах на диске.
                if (session.readFailed) return@collect
                // Признак — наличие учётных данных на диске, а не расшифрованный
                // токен: transient-сбой Keystore даёт token = null при живых
                // ключах и стирал бы всю очередь неотправленных расходов.
                val hasToken = session.hasStoredToken
                val userId = session.me?.id
                // Владелец очереди событий меняется здесь же: этот цикл —
                // единственное место, где смена аккаунта видна целиком.
                analytics.onOwnerChanged(if (hasToken) userId else null)
                // Сменился владелец учётных данных — данные прошлого стираем даже
                // без промежуточного «токена нет». null (профиль не прочитался)
                // сменой НЕ считается: это тот же transient-сбой, а не другой вход.
                val switchedAccount =
                    hasToken && userId != null && lastUserId != null && userId != lastUserId

                if ((hadToken && !hasToken) || switchedAccount) {
                    // Протухшая сессия (401) — НЕ выход: человек вернётся тем же
                    // аккаунтом, и отложенное вступление по ссылке обязано дожить
                    // до переавторизации. Без этой развилки 401 прямо во время
                    // вступления гонялся с `AppRoot`, который кладёт намерение
                    // обратно, и чистка почти всегда выигрывала — приглашение
                    // терялось молча.
                    //
                    // Смена аккаунта намерение тоже НЕ стирает: право на него
                    // решает персистентный владелец (reconcileJoinOwner ниже),
                    // а не сам факт смены. Слепая чистка тут убивала СВОЁ
                    // приглашение гостя: lastUserId переживает явный выход
                    // (профиль вычищен вместе с токеном, и затирать известного
                    // владельца null-ом нельзя — см. ниже), поэтому «A вышел →
                    // гость B открыл ссылку → B вошёл» выглядело сменой
                    // аккаунта, и приглашение самого B удалялось.
                    val expired = sessionStore.lastSessionEndReason.value == SessionEndReason.EXPIRED
                    val keepPendingJoin = switchedAccount || expired
                    // Очередь неотправленных расходов переживает ПРОТУХАНИЕ
                    // сессии: человек добавил расходы офлайн, 90-дневный токен
                    // истёк, он перелогинился — и очередь исчезала молча,
                    // так и не доехав до сервера. При явном выходе и при смене
                    // аккаунта чистим: там очередь принадлежит уходящему.
                    if (clearAll(clearPendingJoin = !keepPendingJoin, clearOutbox = !expired || switchedAccount)) {
                        hadToken = hasToken
                        // Известного владельца НЕ затираем null-ом: у протухшей
                        // сессии профиль вычищен вместе с токеном (endSession
                        // убирает KEY_ME), и присваивание отдавало lastUserId в
                        // null — после чего вход ДРУГИМ аккаунтом переставал
                        // считаться сменой, и сохранённое приглашение уезжало
                        // чужому человеку.
                        if (userId != null) {
                            lastUserId = userId
                            // Единственная ветка, где userId != null, — смена
                            // аккаунта: чужое намерение (владелец — предыдущий
                            // человек) выбрасывается на той же эмиссии,
                            // ничьё достаётся вошедшему.
                            reconcileJoinOwner(userId)
                        }
                    }
                    // Хотя бы одна чистка не удалась — переход НЕ считаем
                    // обработанным: hadToken/lastUserId остаются прежними, и
                    // следующая эмиссия сессии повторит попытку. Иначе данные
                    // предыдущего аккаунта оставались на диске до переустановки.
                    return@collect
                }
                hadToken = hasToken
                if (userId != null) {
                    lastUserId = userId
                    // Отложенное вступление привязываем к вошедшему (или
                    // выбрасываем чужое) — единственная защита, переживающая
                    // смерть процесса между протуханием сессии и входом.
                    reconcileJoinOwner(userId)
                }
            }
        }
    }

    /**
     * Стирает офлайн-данные аккаунта. Каждая чистка изолирована: без runCatching
     * ошибка записи в одной из них вылетала из collect и НАВСЕГДА убивала
     * подписку — все последующие разлогины переставали чистить что-либо.
     * true — все прошли успешно.
     *
     * [clearPendingJoin] — стирать ли отложенное вступление по ссылке; при
     * протухшей сессии оно обязано выжить (см. вызов выше).
     * [clearOutbox] — стирать ли очередь неотправленных расходов; при
     * протухшей сессии она тоже обязана выжить.
     */
    private suspend fun clearAll(
        clearPendingJoin: Boolean = true,
        clearOutbox: Boolean = true,
    ): Boolean {
        var ok = true
        suspend fun step(name: String, block: suspend () -> Unit) {
            runCatching { block() }.onFailure {
                // Отмену не глотаем: чистку прерывает уход процесса или закрытие
                // области, и превращать это в «шаг не удался» нельзя — иначе
                // прерванная чистка ещё и логируется как сбой, а сам лог в
                // отменённой корутине рвёт structured concurrency
                if (it is CancellationException) throw it
                ok = false
                Log.e(TAG, "не удалось очистить $name", it)
            }
        }
        step("api-cache") { cache.clear() }
        // При протухшей сессии очередь оставляем: она принадлежит ТОМУ ЖЕ
        // человеку, который сейчас перелогинится (iOS делает так же).
        if (clearOutbox) {
            step("outbox") { outbox.clear() }
            // Очередь событий уходит вместе с очередью расходов: она
            // принадлежит тому же уходящему человеку. Без этого события
            // прошлого владельца остались бы на диске навсегда.
            step("analytics") { analyticsQueue.keepOwned(null) }
        }
        step("avatars") { avatars.clear() }
        // Отложенное вступление по ссылке-приглашению: без чистки следующий
        // человек, вошедший на этом устройстве, молча вступил бы в чужую группу.
        if (clearPendingJoin) {
            step("pending-join") { pendingJoin.clear() }
        }
        return ok
    }
}
