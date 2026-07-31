package com.zagir.splitty.data

import android.util.Log
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.SessionEndReason
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.di.ApplicationScope
import javax.inject.Inject
import javax.inject.Singleton
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
                    val keepPendingJoin = switchedAccount ||
                        sessionStore.lastSessionEndReason.value == SessionEndReason.EXPIRED
                    if (clearAll(clearPendingJoin = !keepPendingJoin)) {
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
     */
    private suspend fun clearAll(clearPendingJoin: Boolean = true): Boolean {
        var ok = true
        suspend fun step(name: String, block: suspend () -> Unit) {
            runCatching { block() }.onFailure {
                ok = false
                Log.e(TAG, "не удалось очистить $name", it)
            }
        }
        step("api-cache") { cache.clear() }
        step("outbox") { outbox.clear() }
        step("avatars") { avatars.clear() }
        // Отложенное вступление по ссылке-приглашению: без чистки следующий
        // человек, вошедший на этом устройстве, молча вступил бы в чужую группу.
        if (clearPendingJoin) {
            step("pending-join") { pendingJoin.clear() }
        }
        return ok
    }
}
