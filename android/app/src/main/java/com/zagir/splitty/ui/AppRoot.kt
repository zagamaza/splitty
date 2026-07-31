package com.zagir.splitty.ui

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
import com.zagir.splitty.ui.auth.LoginScreen
import com.zagir.splitty.ui.groups.GroupsAlertDialog
import com.zagir.splitty.ui.main.MainScaffold
import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.ui.theme.Splitty
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
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
) : ViewModel() {
    val session: StateFlow<Session?> = sessionStore.state

    private val _openRoomId = MutableStateFlow<String?>(null)

    /** Комната, в которую только что вступили по ссылке: её нужно открыть. */
    val openRoomId: StateFlow<String?> = _openRoomId

    private val _joinError = MutableStateFlow<String?>(null)

    /** Человеческий текст ошибки вступления по ссылке. */
    val joinError: StateFlow<String?> = _joinError

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
            combine(sessionStore.state, pendingJoinStore.pending) { session, roomId ->
                session?.token to roomId
            }
                // Сессия эмитит на ЛЮБУЮ запись в DataStore (обновление профиля,
                // смена темы), а нам интересна только пара «токен + намерение».
                // Без этого фильтра неудачное вступление (см. joinPending: при
                // ошибке намерение остаётся) повторялось бы на каждой посторонней
                // записи — с алертом об ошибке каждый раз.
                .distinctUntilChanged()
                .collect { (token, roomId) ->
                    if (token != null && roomId != null &&
                        token != unauthorizedToken && roomId != joinedRoomId
                    ) {
                        joinPending(token, roomId)
                    }
                }
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
internal fun joinLinkErrorText(e: ApiException): String = when {
    e.status == 404 || e.code == "not_found" ->
        "Группа не найдена. Возможно, её удалили или ссылка-приглашение устарела"

    e.status == 403 || e.code == "forbidden" ->
        "Нет доступа к этой группе. Попросите участника прислать новое приглашение"

    else -> e.message
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
    val joinError by viewModel.joinError.collectAsStateWithLifecycle()
    Surface(modifier = Modifier.fillMaxSize(), color = Splitty.colors.bg) {
        Box(Modifier.fillMaxSize()) {
            when {
                session == null -> Box(
                    Modifier
                        .fillMaxSize()
                        .background(Splitty.colors.bg)
                )

                session?.isAuthenticated == true -> MainScaffold(
                    openRoomId = openRoomId,
                    onRoomOpened = viewModel::onRoomOpened,
                )

                else -> LoginScreen()
            }
            // Ошибку вступления показываем поверх любого состояния: 404 по
            // приглашению может прийти и гостю — сразу после входа.
            GroupsAlertDialog(joinError, viewModel::dismissJoinError)
        }
    }
}
