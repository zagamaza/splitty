package com.zagir.splitty.ui.expense

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.RecipientSum
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxEntry
import com.zagir.splitty.data.OutboxPayload
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Максимум цифр в денежных полях формы (999 999 999 умещается в Int). */
private const val MAX_SUM_DIGITS = 9

/** Оставляет только цифры (максимум [MAX_SUM_DIGITS]) — целые суммы без знака. */
private fun digitsOnly(raw: String): String =
    raw.filter { it.isDigit() }.take(MAX_SUM_DIGITS)

/**
 * Политика офлайн-редактирования (фиксированный дизайн v1): создание и правка
 * НЕотправленных (локальных) записей возможны всегда — они живут в outbox;
 * правка синхронизированной (серверной) операции требует сети — офлайн PUT
 * невозможен, а очередь update в v1 не поддерживается. Чистая функция —
 * покрыта юнит-тестами.
 */
internal fun canSaveExpenseOffline(isEditingSyncedOperation: Boolean, isOnline: Boolean): Boolean =
    isOnline || !isEditingSyncedOperation

/**
 * Состояние формы добавления/редактирования расхода. Все производные
 * значения — чистые вычисления (аналог iOS AddExpenseViewModel).
 */
data class AddExpenseForm(
    /** true — редактирование существующей операции (серверной или локальной). */
    val isEditing: Boolean,
    /** true — редактирование СЕРВЕРНОЙ операции (PUT); офлайн недоступно. */
    val isEditingSynced: Boolean = false,
    /** true — редактирование неотправленной записи outbox (доступно офлайн). */
    val isEditingLocal: Boolean = false,
    /** true — экран открыт без фиксированной группы (сверху выбор чипами). */
    val showsRoomPicker: Boolean,
    /** Группы для выбора (только при [showsRoomPicker]). */
    val rooms: List<RoomSummary> = emptyList(),
    val selectedRoomId: String? = null,
    /** Участники выбранной группы. */
    val members: List<User> = emptyList(),
    /** Валюта выбранной группы — в ней сумма и подсказки деления. */
    val currency: String = "RUB",
    val meId: Long? = null,
    val description: String = "",
    /** Текст поля суммы — только цифры (см. [digitsOnly]). */
    val sumText: String = "",
    /** Кто заплатил (донор). */
    val payerId: Long? = null,
    /** Между кем делится расход. */
    val recipientIds: Set<Long> = emptySet(),
    val splitType: SplitType = SplitType.EQUALLY,
    /** Тексты полей точных сумм по участникам (режим «По суммам»). */
    val amountTexts: Map<Long, String> = emptyMap(),
    val isSaving: Boolean = false,
    /** null — алерта нет; иначе диалог «Ошибка» с этим текстом. */
    val alertMessage: String? = null,
    /** true — расход сохранён, экран пора закрывать (onDone). */
    val isSaved: Boolean = false,
) {
    /** Введённая сумма расхода; null — поле пустое/невалидное. */
    val sum: Int? get() = sumText.toIntOrNull()

    val payer: User? get() = members.firstOrNull { it.id == payerId }

    /** Выбранные участники в стабильном порядке списка участников группы. */
    val selectedMembers: List<User> get() = members.filter { it.id in recipientIds }

    /** Введённая доля участника (пустое/невалидное поле = 0). */
    fun enteredAmount(userId: Long): Int = amountTexts[userId]?.toIntOrNull() ?: 0

    /** Σ введённых долей ВЫБРАННЫХ участников (снятые с выбора не считаются). */
    val distributedTotal: Int get() = recipientIds.sumOf { enteredAmount(it) }

    /** Остаток нераспределённой суммы; < 0 — перерасход. */
    val remainingToDistribute: Int get() = (sum ?: 0) - distributedTotal

    /** true — суммы участников сходятся с суммой расхода (Σ == sum, sum >= 1). */
    val isDistributionBalanced: Boolean
        get() {
            val total = sum ?: return false
            return total >= 1 && distributedTotal == total
        }

    /**
     * Доступность «Сохранить»: группа выбрана, описание непусто, сумма >= 1,
     * донор выбран, есть получатели; в режиме «По суммам» — только при Σ == sum.
     */
    val canSave: Boolean
        get() = selectedRoomId != null &&
            description.isNotBlank() &&
            (sum ?: 0) >= 1 &&
            payerId != null &&
            recipientIds.isNotEmpty() &&
            (splitType != SplitType.BY_EXACT_AMOUNT || isDistributionBalanced)
}

/**
 * VM формы добавления/редактирования расхода: выбор группы (когда экран
 * открыт с центральной «+»), описание, сумма, донор и способ деления —
 * «Поровну» (recipientIds, доли раскладывает сервер) или «По суммам»
 * (recipientSums, Σ == sum). После успешного сохранения —
 * SessionStore.noteDataChanged() и isSaved = true (экран зовёт onDone).
 */
@HiltViewModel
class AddExpenseViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val outboxStore: OutboxStore,
    private val outboxSyncer: OutboxSyncer,
    networkMonitor: NetworkMonitor,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<AddExpenseForm>>(UiState.Loading)
    val state: StateFlow<UiState<AddExpenseForm>> = _state.asStateFlow()

    /**
     * Онлайн-статус: офлайн при правке серверной операции сохранение
     * блокируется, в форме — плашка (см. [canSaveExpenseOffline]).
     */
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline

    private var isStarted = false
    private var fixedRoomId: String? = null
    private var editOperationId: String? = null
    private var editLocalId: String? = null

    /**
     * Исходный порядок получателей редактируемой операции — от него зависит
     * раздача остатка equally-деления на сервере, поэтому при сохранении
     * правки порядок сохраняется, новые участники добавляются в конец.
     */
    private var editRecipientOrder: List<Long> = emptyList()

    /** Первичная настройка (идемпотентна — зовётся из LaunchedEffect экрана). */
    fun start(roomId: String?, operationId: String?, localId: String? = null) {
        if (isStarted) return
        isStarted = true
        fixedRoomId = roomId
        // Редактирование возможно только внутри конкретной группы; правка
        // локальной (неотправленной) записи имеет приоритет над серверной.
        editLocalId = localId.takeIf { roomId != null }
        editOperationId = operationId.takeIf { roomId != null && editLocalId == null }
        load()
    }

    /** Повторная загрузка после ошибки. */
    fun retry() {
        if (isStarted) load()
    }

    private fun load() {
        _state.value = UiState.Loading
        viewModelScope.launch {
            try {
                val meId = sessionStore.state.value?.me?.id
                val roomId = fixedRoomId
                val localId = editLocalId
                if (roomId != null && localId != null) {
                    // Правка неотправленной записи outbox: участники и валюта —
                    // из детали комнаты (офлайн придёт из кеша), поля — из payload.
                    val room = repository.room(roomId).value
                    val entry = outboxStore.entry(localId)
                    if (entry == null) {
                        _state.value = UiState.Error("Операция не найдена")
                        return@launch
                    }
                    _state.value = UiState.Content(localEntryForm(room, entry, meId))
                } else if (roomId != null) {
                    val room = repository.room(roomId).value
                    val operation = editOperationId
                        ?.let { id -> room.operations.firstOrNull { it.id == id } }
                    if (editOperationId != null && operation == null) {
                        _state.value = UiState.Error("Операция не найдена")
                        return@launch
                    }
                    _state.value = UiState.Content(fixedRoomForm(room, operation, meId))
                } else {
                    val rooms = repository.rooms(archived = false).value
                    _state.value = UiState.Content(
                        AddExpenseForm(
                            isEditing = false,
                            showsRoomPicker = true,
                            rooms = rooms,
                            meId = meId,
                        )
                    )
                }
            } catch (e: CancellationException) {
                throw e // отмена — не ошибка
            } catch (e: ApiException) {
                _state.value = UiState.Error(e.message)
            }
        }
    }

    /** Форма правки локальной (неотправленной) записи outbox. */
    private fun localEntryForm(room: RoomDetail, entry: OutboxEntry, meId: Long?): AddExpenseForm {
        val payload = entry.payload
        editRecipientOrder = payload.recipientOrder
        val form = AddExpenseForm(
            isEditing = true,
            isEditingLocal = true,
            showsRoomPicker = false,
            meId = meId,
            description = payload.description,
            sumText = payload.sum.toString(),
            payerId = payload.donorId,
            recipientIds = editRecipientOrder.toSet(),
            splitType = payload.splitType,
            amountTexts = payload.recipientSums
                ?.associate { it.userId to it.sum.toString() }
                .orEmpty(),
        )
        return appliedRoom(form, room.id, room.members, room.currency)
    }

    /** Форма для фиксированной группы, с prefill из редактируемой операции. */
    private fun fixedRoomForm(room: RoomDetail, operation: Operation?, meId: Long?): AddExpenseForm {
        var form = AddExpenseForm(
            isEditing = operation != null,
            isEditingSynced = operation != null,
            showsRoomPicker = false,
            meId = meId,
        )
        if (operation != null) {
            editRecipientOrder = operation.recipients.map { it.user.id }
            form = form.copy(
                description = operation.description,
                sumText = operation.sum.toString(),
                payerId = operation.donor.id,
                recipientIds = editRecipientOrder.toSet(),
                splitType = operation.splitType ?: SplitType.EQUALLY,
                // Prefill долей из ХРАНИМЫХ сумм: для «По суммам» — точные,
                // для «Поровну» — канонические (стартовые при смене режима).
                amountTexts = operation.recipients.associate { it.user.id to it.sum.toString() },
            )
        }
        return appliedRoom(form, room.id, room.members, room.currency)
    }

    /** Выбор группы из чипов: делим на всех, платит текущий пользователь. */
    fun selectRoom(summary: RoomSummary) {
        updateForm { form ->
            if (form.selectedRoomId == summary.id) {
                form
            } else {
                appliedRoom(
                    form.copy(recipientIds = emptySet(), payerId = null, amountTexts = emptyMap()),
                    summary.id,
                    summary.members,
                    summary.currency,
                )
            }
        }
    }

    /**
     * Применяет группу к форме: чистит выбор от чужих участников, пустой
     * выбор — все участники, донор — текущий пользователь либо первый участник.
     */
    private fun appliedRoom(
        form: AddExpenseForm,
        roomId: String,
        members: List<User>,
        currency: String,
    ): AddExpenseForm {
        val memberIds = members.map { it.id }.toSet()
        val recipients = form.recipientIds.intersect(memberIds).ifEmpty { memberIds }
        val currentPayer = form.payerId
        val me = form.meId
        val payerId = when {
            currentPayer != null && currentPayer in memberIds -> currentPayer
            me != null && me in memberIds -> me
            else -> members.firstOrNull()?.id
        }
        return form.copy(
            selectedRoomId = roomId,
            members = members,
            currency = currency,
            recipientIds = recipients,
            amountTexts = form.amountTexts.filterKeys { it in memberIds },
            payerId = payerId,
        )
    }

    fun onDescriptionChange(value: String) = updateForm { it.copy(description = value) }

    fun onSumChange(raw: String) = updateForm { it.copy(sumText = digitsOnly(raw)) }

    fun selectPayer(userId: Long) = updateForm { it.copy(payerId = userId) }

    fun setSplitType(type: SplitType) = updateForm { it.copy(splitType = type) }

    fun toggleRecipient(userId: Long) = updateForm { form ->
        val ids = if (userId in form.recipientIds) {
            form.recipientIds - userId
        } else {
            form.recipientIds + userId
        }
        form.copy(recipientIds = ids)
    }

    fun onAmountChange(userId: Long, raw: String) = updateForm {
        it.copy(amountTexts = it.amountTexts + (userId to digitsOnly(raw)))
    }

    fun dismissAlert() = updateForm { it.copy(alertMessage = null) }

    /**
     * Сохранение: POST /operations, PUT (правка серверной) либо правка записи
     * outbox (локальная). Создание идемпотентно: POST всегда шлёт clientOpId,
     * и при обрыве сети/таймауте расход уходит в outbox с ТЕМ ЖЕ ключом —
     * досылка не создаст дубль, даже если первый POST успел примениться.
     * Защита от двойного тапа — isSaving выставляется синхронно до корутины.
     */
    fun save() {
        val form = currentForm() ?: return
        if (form.isSaving || !form.canSave) return
        // Правка серверной операции офлайн невозможна (кнопка задизейблена,
        // это — страховка от гонки «сеть пропала после тапа»).
        if (!canSaveExpenseOffline(form.isEditingSynced, isOnline.value)) return
        val roomId = form.selectedRoomId ?: return
        val sum = form.sum ?: return
        val payerId = form.payerId ?: return
        val description = form.description.trim()
        val orderedIds = orderedRecipientIds(form)
        val split = if (form.splitType == SplitType.BY_EXACT_AMOUNT) {
            // Участники с нулевой/пустой долей опускаются: сервер отклоняет
            // суммы < 1, а при Σ == sum пропуск нулей суммы долей не меняет.
            ExpenseSplit.ByExactAmount(
                orderedIds.mapNotNull { id ->
                    form.enteredAmount(id)
                        .takeIf { it >= 1 }
                        ?.let { RecipientSum(userId = id, sum = it) }
                }
            )
        } else {
            ExpenseSplit.Equally(orderedIds)
        }

        updateForm { it.copy(isSaving = true) }
        viewModelScope.launch {
            try {
                val operationId = editOperationId
                val localId = editLocalId
                when {
                    // Правка неотправленной записи — только outbox, сеть не нужна.
                    localId != null -> {
                        outboxStore.update(
                            localId,
                            OutboxPayload.of(description, sum, payerId, split),
                        )
                        outboxSyncer.syncNow()
                    }

                    operationId != null -> {
                        repository.updateOperation(roomId, operationId, description, sum, payerId, split)
                        sessionStore.noteDataChanged()
                    }

                    else -> createOperation(roomId, description, sum, payerId, split)
                }
                updateForm { it.copy(isSaving = false, isSaved = true) }
            } catch (e: CancellationException) {
                throw e // отмена — не ошибка
            } catch (e: ApiException) {
                updateForm { it.copy(isSaving = false, alertMessage = e.message) }
            }
        }
    }

    /**
     * Создание: офлайн — сразу в outbox; онлайн — POST с clientOpId, а при
     * транспортной ошибке (сеть пропала/таймаут) — в outbox с тем же ключом.
     * Ошибки сервера (4xx/5xx) пробрасываются наверх — алерт формы.
     */
    private suspend fun createOperation(
        roomId: String,
        description: String,
        sum: Int,
        payerId: Long,
        split: ExpenseSplit,
    ) {
        val localId = UUID.randomUUID().toString()
        if (isOnline.value) {
            try {
                repository.addOperation(roomId, description, sum, payerId, split, clientOpId = localId)
                sessionStore.noteDataChanged()
                return
            } catch (e: ApiException) {
                if (e.code != ApiException.CODE_TRANSPORT) throw e
                // Транспортная ошибка — падаем в офлайн-ветку ниже.
            }
        }
        outboxStore.add(
            OutboxEntry(
                localId = localId,
                roomId = roomId,
                payload = OutboxPayload.of(description, sum, payerId, split),
                createdAt = Instant.now(),
            )
        )
        outboxSyncer.syncNow()
    }

    /** Удаление неотправленной записи outbox (доступно из локальной правки). */
    fun deleteLocal() {
        val localId = editLocalId ?: return
        val form = currentForm() ?: return
        if (form.isSaving) return
        updateForm { it.copy(isSaving = true) }
        viewModelScope.launch {
            outboxStore.remove(localId)
            updateForm { it.copy(isSaving = false, isSaved = true) }
        }
    }

    /**
     * Стабильный порядок получателей: при редактировании — исходный порядок
     * операции (сервер раздаёт остаток equally-деления первым в массиве),
     * новые участники — следом; при создании — порядок списка участников.
     */
    private fun orderedRecipientIds(form: AddExpenseForm): List<Long> {
        val kept = editRecipientOrder.filter { it in form.recipientIds }
        val added = form.members
            .map { it.id }
            .filter { it in form.recipientIds && it !in kept }
        return kept + added
    }

    private fun currentForm(): AddExpenseForm? {
        val state = _state.value
        return if (state is UiState.Content) state.value else null
    }

    private fun updateForm(transform: (AddExpenseForm) -> AddExpenseForm) {
        _state.update { state ->
            if (state is UiState.Content) UiState.Content(transform(state.value)) else state
        }
    }
}
