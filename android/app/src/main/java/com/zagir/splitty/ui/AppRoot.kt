package com.zagir.splitty.ui

import com.zagir.splitty.core.analytics.Analytics
import com.zagir.splitty.core.analytics.AnalyticsEvent
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.core.session.Session
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.push.PendingPushRoute
import com.zagir.splitty.push.PushEventBus
import com.zagir.splitty.push.PushRoute
import com.zagir.splitty.ui.auth.LoginScreen
import com.zagir.splitty.ui.groups.GroupsAlertDialog
import com.zagir.splitty.ui.main.MainScaffold
import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.ui.theme.Splitty
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * Корневой VM: состояние сессии для [AppRoot] плюс исполнение отложенного
 * вступления в группу по ссылке-приглашению.
 *
 * Почему вступление живёт здесь, а не в `MainActivity`: ссылку можно открыть
 * гостем, и тогда join обязан подождать входа. Единственное место, которое
 * видит и намерение, и появление сессии, — корень.
 */
@HiltViewModel
class AppRootViewModel @Inject constructor(
    private val sessionStore: SessionStore,
    private val pendingJoinStore: PendingJoinStore,
    private val repository: SplittyRepository,
    private val pushEventBus: PushEventBus,
    private val analytics: Analytics,
) : ViewModel() {
    val session: StateFlow<Session?> = sessionStore.state

    private val _openRoomId = MutableStateFlow<String?>(null)

    /** Комната, в которую только что вступили по ссылке: её нужно открыть. */
    val openRoomId: StateFlow<String?> = _openRoomId.asStateFlow()

    /**
     * Куда вести по тапу на push; null — вести некуда.
     *
     * Той же дорогой, что и вступление по ссылке: намерение ждёт здесь, пока
     * соберётся корневой экран (холодный старт по тапу) и пока человек войдёт
     * (тап при протухшей сессии). Гасит его исполнитель — [onPushRouteHandled].
     *
     * Намерение с ЧУЖИМ владельцем выбрасывается: пуш адресован тому, кто был
     * в аккаунте в момент тапа, а войти на устройстве после этого мог уже
     * другой человек — и его уносило бы в чужую группу (то же правило, что в
     * `PendingJoinStore.reconcileOwner`). Владелец null — сессия в момент тапа
     * ещё читалась с диска, сравнивать не с чем.
     */
    val pushRoute: StateFlow<PushRoute?> = combine(
        pushEventBus.pendingRoute,
        sessionStore.state,
    ) { pending, session ->
        pendingPushRouteFor(pending, session?.me?.id)
    }.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    /** Тап исполнен — намерение забываем, иначе оно переиграется. */
    fun onPushRouteHandled() {
        pushEventBus.consumeRoute()
    }

    private val _joinError = MutableStateFlow<UiText?>(null)

    /** Человеческий текст ошибки вступления по ссылке. */
    val joinError: StateFlow<UiText?> = _joinError.asStateFlow()

    /** Защита от повторного входа в [joinPending] на соседних эмиссиях. */
    private var isJoining = false

    /**
     * Токен, на котором вступление уже отвалилось с 401. Без этой отметки
     * оставшееся в хранилище намерение (см. ниже) дало бы новый запрос на
     * ближайшей эмиссии: разлогин по 401 асинхронный, и сессия в этот момент
     * ещё выглядит живой — получился бы плотный цикл 401-х.
     */
    private var unauthorizedToken: String? = null

    /**
     * Комната, в которую уже вступили в этом процессе.
     *
     * Очистка намерения и эмиссия сессии — два независимых источника для
     * [combine], и он вправе отдать промежуточную пару «новый токен + ЕЩЁ не
     * стёртое намерение». Без отметки это второй запрос на вступление в ту же
     * группу сразу после первого.
     */
    private var joinedRoomId: String? = null

    init {
        viewModelScope.launch {
            combine(sessionStore.state, pendingJoinStore.pending) { session, pending ->
                Triple(session?.token, session?.me?.id, pending)
            }
                // Сессия эмитит на ЛЮБУЮ запись в DataStore (обновление профиля,
                // смена темы), а нам интересна только тройка «токен + владелец +
                // намерение». Без этого фильтра неудачное вступление (см.
                // joinPending: при ошибке намерение остаётся) повторялось бы на
                // каждой посторонней записи — с алертом об ошибке каждый раз.
                .distinctUntilChanged()
                .collect { (token, userId, pending) ->
                    val roomId = pending?.roomId
                    if (token == null || roomId == null || token == unauthorizedToken) {
                        return@collect
                    }
                    // Намерение ЧУЖОЕ: ссылку открыл предыдущий владелец
                    // устройства, его сессия протухла (чистка приглашение при
                    // этом намеренно сохраняет), а вошёл уже другой человек.
                    // Без этой проверки он молча вступал бы в чужую приватную
                    // группу — и его туда ещё и уносило бы навигацией.
                    // Сравниваем, только когда обе стороны известны: userId ==
                    // null — это нечитаемый профиль, а не другой аккаунт.
                    val owner = pending.ownerId
                    if (owner != null && userId != null && owner != userId) return@collect
                    if (roomId == joinedRoomId) {
                        // Ту же ссылку открыли повторно. Запрос не нужен (в
                        // группе уже состоим), но намерение обязано быть
                        // стёрто, а комната — открыта: иначе тап по ссылке
                        // выглядит как «ничего не произошло», а забытое на
                        // диске намерение проваливает в эту группу на каждом
                        // следующем холодном старте.
                        runCatching { pendingJoinStore.clear() }
                        _openRoomId.value = roomId
                        return@collect
                    }
                    joinPending(token, roomId)
                }
        }

        // Незавершённая после tombstone чистка (`purge_incomplete`): доводим её
        // сами. Полагаться на то, что человек нажмёт «Удалить аккаунт» ещё раз,
        // нельзя — он волен уйти на другой экран или закрыть приложение, а
        // фонового реконсилятора на сервере нет, и его PII (имя в снимках
        // комнат, chat_state, bug_report, push_outbox) осталась бы в базе
        // навсегда. Флаг персистентный, поэтому холодный старт — тоже попытка
        // повтора. См. [com.zagir.splitty.core.session.Session.purgePending].
        viewModelScope.launch {
            sessionStore.state
                .map { it?.purgePending == true && it.token != null }
                .distinctUntilChanged()
                .collect { pending -> if (pending) finishPendingPurge() }
        }
    }

    /** Повтор чистки уже в полёте — второй запускать нельзя. */
    private var isFinishingPurge = false

    /**
     * Доводит до конца чистку, начатую упавшим после tombstone `DELETE /me`.
     *
     * 401 — единственный терминальный исход: `authDeleted` пускает удалённых, и
     * раз отказал он, токен мёртв по-настоящему (или аккаунт уже вычищен
     * целиком). Повторять нечем, поэтому выходим по-честному — иначе на
     * устройстве навсегда осталась бы зомби-сессия, которую ничто не выгоняет
     * на экран входа. Всё остальное (снова `purge_incomplete`, 5xx, нет сети)
     * временно: флаг и токен на месте, повтор случится на следующем запуске.
     */
    private suspend fun finishPendingPurge() {
        if (isFinishingPurge) return
        isFinishingPurge = true
        try {
            repository.deleteAccount()
            sessionStore.logout()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            // Throwable, а не ApiException: любое другое исключение вылетало бы
            // из collect и НАВСЕГДА убивало подписку (тот же фикс, что в
            // joinPending).
            if ((e as? ApiException)?.isUnauthorized == true) {
                runCatching { sessionStore.logout() }
            }
        } finally {
            isFinishingPurge = false
        }
    }

    /** Экран открыл комнату — намерение исполнено. */
    fun onRoomOpened() {
        _openRoomId.value = null
    }

    fun dismissJoinError() {
        _joinError.value = null
    }

    /**
     * Исполняет намерение вступить в [roomId].
     *
     * Намерение здесь только ЧИТАЕТСЯ (оно уже пришло эмиссией потока) и
     * стирается ровно в двух случаях: вступление удалось либо сервер ответил
     * терминально (404 «группы нет», 403 «нет доступа») — повторять такой
     * запрос бессмысленно. Раньше намерение забиралось `take()` ДО запроса, и
     * одна попытка вступить без сети сжигала приглашение навсегда: ссылку
     * присылают в мессенджере один раз, второй раз её взять неоткуда.
     */
    private suspend fun joinPending(token: String, roomId: String) {
        if (isJoining) return
        isJoining = true
        try {
            repository.joinRoom(roomId)
            analytics.track(AnalyticsEvent.RoomJoined(via = "link"))
            joinedRoomId = roomId
            // Намерение исполнено — стираем. Ошибку записи глотаем: повторное
            // вступление сервер обработает идемпотентно (участник уже в группе),
            // а отметка выше не даст запросу уйти второй раз в этом процессе.
            runCatching { pendingJoinStore.clear() }
            // Единая инвалидация: экраны-списки перезагрузятся сами.
            sessionStore.noteDataChanged()
            _openRoomId.value = roomId
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            // Throwable, а не ApiException: любое другое исключение (запись в
            // DataStore, неожиданный сбой) вылетало из collect в init и
            // НАВСЕГДА убивало подписку — диплинки переставали работать до
            // перезапуска процесса (тот же фикс, что в ProfileViewModel).
            handleJoinFailure(e, token)
        } finally {
            isJoining = false
        }
    }

    /** Разбор неудачи вступления: что показать и стирать ли намерение. */
    private suspend fun handleJoinFailure(e: Throwable, token: String) {
        // Не ApiException вовсе (сбой записи, неожиданное исключение): намерение
        // не трогаем — причина к самой группе отношения не имеет.
        val api = e as? ApiException ?: run {
            _joinError.value = humanErrorText(e)
            return
        }
        when {
            api.isUnauthorized -> {
                // Сессия слетела ровно на этом запросе: AuthInterceptor уже
                // разлогинил, нас вернёт на экран входа. Намерение НЕ трогаем —
                // после переавторизации вступление доедет само, а «Требуется
                // вход» поверх экрана входа сказало бы очевидное.
                // unauthorizedToken гасит повторную попытку на этом же токене:
                // разлогин асинхронный, и сессия секунду ещё выглядит живой.
                unauthorizedToken = token
            }

            // Терминальный отказ: группы нет или в неё не пускают. Намерение
            // стираем — иначе оно всплывало бы алертом на каждом старте.
            isTerminalJoinError(api) -> {
                runCatching { pendingJoinStore.clear() }
                _joinError.value = joinLinkErrorText(api)
            }

            // Всё остальное (нет сети, 5xx) — временно: намерение остаётся,
            // вступление повторится на следующем входе или следующей ссылке.
            // Именно это и есть смысл отказа от `take()` до запроса: одно
            // открытие ссылки в метро больше не сжигает приглашение.
            else -> _joinError.value = joinLinkErrorText(api)
        }
    }
}

/**
 * Кому достанется намерение перейти по пушу: своему — да, чужому — нет.
 * Отдельной функцией — внутри `combine` это правило не проверить ничем, а
 * ошибка в нём уносит человека в чужую группу.
 */
internal fun pendingPushRouteFor(pending: PendingPushRoute?, userId: Long?): PushRoute? {
    if (pending == null) return null
    val owner = pending.ownerId
    if (owner != null && userId != null && owner != userId) return null
    return pending.route
}

/**
 * Отказ, который не исправится повторной попыткой: группы нет (404) либо в неё
 * не пускают (403). Только на них намерение стирается — всё остальное (сеть,
 * 5xx) может пройти со второго раза, и приглашение обязано дожить до него.
 */
internal fun isTerminalJoinError(e: ApiException): Boolean =
    e.status == 404 || e.code == "not_found" || e.status == 403 || e.code == "forbidden"

/**
 * Человеческий текст ошибки вступления по приглашению (порт iOS
 * `joinLinkErrorText`).
 *
 * Пользователь диплинка не нажимал «Присоединиться» и не вводил код — для него
 * это «открыл ссылку, и что-то пошло не так». Сырое «Не найдено» от сервера в
 * этом контексте не объясняет ничего.
 */
internal fun joinLinkErrorText(e: ApiException): UiText = when {
    e.status == 404 || e.code == "not_found" ->
        UiText.res(R.string.error_group_not_found)

    e.status == 403 || e.code == "forbidden" ->
        UiText.res(R.string.error_group_no_access)

    else -> e.uiText()
}

/**
 * Корень приложения: DataStore ещё читается → пустой фон (без мигания
 * логина), нет токена → LoginScreen, есть → MainScaffold.
 * Аналог iOS RootView.
 */
@Composable
fun AppRoot(viewModel: AppRootViewModel = hiltViewModel()) {
    val session by viewModel.session.collectAsStateWithLifecycle()
    val openRoomId by viewModel.openRoomId.collectAsStateWithLifecycle()
    val pushRoute by viewModel.pushRoute.collectAsStateWithLifecycle()
    val joinError by viewModel.joinError.collectAsStateWithLifecycle()
    Surface(modifier = Modifier.fillMaxSize(), color = Splitty.colors.bg) {
        Box(Modifier.fillMaxSize()) {
            when {
                session == null -> Box(
                    Modifier
                        .fillMaxSize()
                        .background(Splitty.colors.bg)
                )

                // Тап по пушу исполняется только здесь, под входом: гостю
                // открывать нечего, и намерение дождётся его входа.
                session?.isAuthenticated == true -> MainScaffold(
                    openRoomId = openRoomId,
                    onRoomOpened = viewModel::onRoomOpened,
                    pushRoute = pushRoute,
                    onPushRouteHandled = viewModel::onPushRouteHandled,
                )

                else -> LoginScreen()
            }
            // Ошибку вступления показываем поверх любого состояния: 404 по
            // приглашению может прийти и гостю — сразу после входа.
            GroupsAlertDialog(joinError, viewModel::dismissJoinError)
        }
    }
}
