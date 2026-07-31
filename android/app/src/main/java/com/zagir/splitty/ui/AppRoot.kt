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
import com.zagir.splitty.ui.theme.Splitty
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
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
     * возврат намерения в хранилище (см. ниже) немедленно дал бы новую эмиссию
     * и новый запрос: разлогин по 401 асинхронный, и сессия в этот момент ещё
     * выглядит живой — получился бы плотный цикл 401-х.
     */
    private var unauthorizedToken: String? = null

    init {
        viewModelScope.launch {
            combine(sessionStore.state, pendingJoinStore.pending) { session, roomId ->
                session?.token to roomId
            }.collect { (token, roomId) ->
                if (token != null && roomId != null && token != unauthorizedToken) {
                    joinPending(token)
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

    private suspend fun joinPending(token: String) {
        if (isJoining) return
        isJoining = true
        try {
            val roomId = runCatching { pendingJoinStore.take() }.getOrNull() ?: return
            try {
                repository.joinRoom(roomId)
                // Единая инвалидация: экраны-списки перезагрузятся сами.
                sessionStore.noteDataChanged()
                _openRoomId.value = roomId
            } catch (e: ApiException) {
                if (e.isUnauthorized) {
                    // Сессия слетела ровно на этом запросе: AuthInterceptor уже
                    // разлогинил, нас вернёт на экран входа. Намерение кладём
                    // обратно — после переавторизации вступление доедет само, а
                    // «Требуется вход» поверх экрана входа сказало бы очевидное.
                    unauthorizedToken = token
                    runCatching { pendingJoinStore.set(roomId) }
                } else {
                    _joinError.value = joinLinkErrorText(e)
                }
            }
        } finally {
            isJoining = false
        }
    }
}

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
