package com.zagir.splitty.ui.groups

import com.zagir.splitty.ui.components.humanErrorText
import com.zagir.splitty.R
import com.zagir.splitty.core.ui.UiText
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * VM карточки операции: отдельного GET операции в API нет — грузим деталь
 * комнаты (заодно даёт валюту) и находим операцию по id. Перезагрузка —
 * по dataVersion (после редактирования расхода карточка обновится сама).
 */
@HiltViewModel
class OperationDetailViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
) : ViewModel() {

    /**
     * Данные карточки: операция + валюта комнаты (в ней все суммы) + участники
     * комнаты. [members] нужны ReceiptCard'у секции «Позиции чека» — по userId
     * доли он рисует аватары; без списка участников чек не отрисовать.
     */
    data class OperationCard(
        val operation: Operation,
        val currency: String,
        val members: List<User>,
    )

    private val paramsFlow = MutableStateFlow<Pair<String, String>?>(null)

    private val _state = MutableStateFlow<UiState<OperationCard>>(UiState.Loading)
    val state: StateFlow<UiState<OperationCard>> = _state.asStateFlow()

    /** id текущего пользователя; null — профиль не загружен. */
    val meId: StateFlow<Long?> = sessionStore.state
        .map { it?.me?.id }
        .stateIn(viewModelScope, SharingStarted.Eagerly, sessionStore.state.value?.me?.id)

    private val _isDeleting = MutableStateFlow(false)
    val isDeleting: StateFlow<Boolean> = _isDeleting.asStateFlow()

    private val _alertMessage = MutableStateFlow<UiText?>(null)
    val alertMessage: StateFlow<UiText?> = _alertMessage.asStateFlow()

    /** true после успешного удаления: перезагрузки по dataVersion не нужны. */
    private var isDeleted = false

    init {
        viewModelScope.launch {
            combine(paramsFlow.filterNotNull(), sessionStore.dataVersion) { params, _ -> params }
                .collectLatest { (roomId, operationId) -> load(roomId, operationId) }
        }
    }

    /** Привязка экрана к операции; повторные вызовы с теми же id — no-op. */
    fun start(roomId: String, operationId: String) {
        val params = roomId to operationId
        if (paramsFlow.value != params) paramsFlow.value = params
    }

    /** Повтор после ошибки загрузки. */
    fun retry() {
        val (roomId, operationId) = paramsFlow.value ?: return
        _state.value = UiState.Loading
        viewModelScope.launch { load(roomId, operationId) }
    }

    fun dismissAlert() {
        _alertMessage.value = null
    }

    /**
     * Байты вложения для просмотра. Идёт через repository (auth-заголовок ставит
     * OkHttp-интерцептор) — сырой URL в Coil не даст авторизоваться на GET /files.
     */
    suspend fun fileData(fileId: String): ByteArray = repository.fileData(fileId)

    /** DELETE операции; успех — dataVersion bump и [onDeleted] (закрыть экран). */
    fun delete(onDeleted: () -> Unit) {
        val (roomId, operationId) = paramsFlow.value ?: return
        if (_isDeleting.value) return
        viewModelScope.launch {
            _isDeleting.value = true
            try {
                repository.deleteOperation(roomId, operationId)
                isDeleted = true
                // Единая инвалидация: экран группы и списки перезагрузятся сами.
                sessionStore.noteDataChanged()
                onDeleted()
            } catch (e: ApiException) {
                _alertMessage.value = humanErrorText(e)
            } finally {
                _isDeleting.value = false
            }
        }
    }

    private suspend fun load(roomId: String, operationId: String) {
        if (isDeleted) return
        try {
            val room = repository.room(roomId).value
            val operation = room.operations.firstOrNull { it.id == operationId }
            _state.value = if (operation != null) {
                UiState.Content(
                    OperationCard(
                        operation = operation,
                        currency = room.currency,
                        members = room.members,
                    )
                )
            } else {
                UiState.Error(OPERATION_NOT_FOUND)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: ApiException) {
            if (_state.value is UiState.Content) {
                _alertMessage.value = humanErrorText(e)
            } else {
                _state.value = UiState.Error(e.message)
            }
        }
    }

    private companion object {
        const val OPERATION_NOT_FOUND = "Операция не найдена"
    }
}

/** Тип вложения для показа: сырое значение API («video») в UI пугает. */
enum class AttachmentKind { PHOTO, VIDEO, DOCUMENT, OTHER }

/**
 * Категория вложения по сырому типу API. Зеркало iOS `attachmentTypeName`:
 * «photo»/«image» → фото, «video» → видео, «document» → документ, прочее —
 * нейтральное «вложение».
 */
fun attachmentKind(type: String): AttachmentKind = when (type) {
    "photo", "image" -> AttachmentKind.PHOTO
    "video" -> AttachmentKind.VIDEO
    "document" -> AttachmentKind.DOCUMENT
    else -> AttachmentKind.OTHER
}
