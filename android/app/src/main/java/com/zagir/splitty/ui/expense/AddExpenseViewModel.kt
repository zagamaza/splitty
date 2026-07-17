package com.zagir.splitty.ui.expense

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.RecipientSum
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.OutboxEntry
import com.zagir.splitty.data.OutboxPayload
import com.zagir.splitty.data.OutboxStore
import com.zagir.splitty.data.OutboxSyncer
import com.zagir.splitty.data.SplittyRepository
import com.zagir.splitty.ui.components.humanErrorText
import dagger.hilt.android.lifecycle.HiltViewModel
import java.io.File
import java.time.Instant
import java.util.UUID
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable

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
 * Накатывает ответ AI-распознавания на форму (порт iOS `apply(parse:)`): описание
 * и сумму берём из черновика (если модель их дала), донора — если распознан и он
 * среди участников. Позиции чека сохраняем в [AddExpenseForm.draftItems]; при их
 * наличии сохранение блокируется ([isItemizedLocked]) до itemized-режима формы
 * (Task 10). Черновик НИКОГДА не теряется — пустые поля ответа не затирают ввод.
 * Чистая функция — покрыта юнит-тестами без VM.
 */
internal fun AddExpenseForm.applyingParse(response: ParseResponse): AddExpenseForm {
    val draft = response.draft
    val items = draft.itemList
    val memberIds = members.map { it.id }.toSet()
    val recognizedPayer = draft.donorId?.takeIf { it in memberIds }
    // Плоский расход (без позиций): по умолчанию делим на всех участников.
    val recipients = if (items.isEmpty() && recipientIds.isEmpty()) memberIds else recipientIds
    return copy(
        description = draft.description.ifBlank { description },
        sumText = if (draft.sum > 0) draft.sum.toString() else sumText,
        payerId = recognizedPayer ?: payerId,
        recipientIds = recipients,
        draftItems = items,
        isItemizedLocked = items.isNotEmpty(),
        parseQuestions = response.questionList,
        parseRetryMessage = null,
    )
}

/**
 * Текущий черновик формы для отправки на /parse (голосовая правка Task 12):
 * null, если форма пустая (распознавание с нуля) — иначе описание/сумма/донор
 * и позиции чека. Пустой список позиций не сериализуем (null вместо []).
 */
internal fun AddExpenseForm.currentParseDraft(): ParseDraft? {
    val hasContent = draftItems.isNotEmpty() || description.isNotBlank() || (sum ?: 0) > 0
    return if (hasContent) {
        ParseDraft(
            description = description,
            sum = sum ?: 0,
            donorId = payerId,
            items = draftItems.ifEmpty { null },
        )
    } else {
        null
    }
}

/**
 * Снимок черновика формы для SavedStateHandle (переживает process death). Только
 * пользовательский ввод + результат распознавания; участники/валюта берутся из
 * заново загруженной комнаты. amountTexts как Map<Long,String> — JSON-ключи станут
 * строками (kotlinx это умеет).
 */
@Serializable
internal data class ExpenseDraftSnapshot(
    val selectedRoomId: String? = null,
    val description: String = "",
    val sumText: String = "",
    val payerId: Long? = null,
    val recipientIds: List<Long> = emptyList(),
    val splitType: SplitType = SplitType.EQUALLY,
    val amountTexts: Map<Long, String> = emptyMap(),
    val draftItems: List<OperationItem> = emptyList(),
    val parseQuestions: List<String> = emptyList(),
    val isItemizedLocked: Boolean = false,
) {
    companion object {
        /** Снимок из формы (только поля, которые нужно восстановить). */
        fun from(form: AddExpenseForm): ExpenseDraftSnapshot = ExpenseDraftSnapshot(
            selectedRoomId = form.selectedRoomId,
            description = form.description,
            sumText = form.sumText,
            payerId = form.payerId,
            recipientIds = form.recipientIds.toList(),
            splitType = form.splitType,
            amountTexts = form.amountTexts,
            draftItems = form.draftItems,
            parseQuestions = form.parseQuestions,
            isItemizedLocked = form.isItemizedLocked,
        )

        /** true — в снимке есть что восстанавливать (не пустой стартовый черновик). */
        fun hasContent(s: ExpenseDraftSnapshot): Boolean =
            s.description.isNotBlank() || s.sumText.isNotEmpty() || s.draftItems.isNotEmpty()
    }

    /** Накатывает снимок на форму, уже привязанную к комнате (участники/валюта готовы). */
    fun applyTo(form: AddExpenseForm): AddExpenseForm {
        val memberIds = form.members.map { it.id }.toSet()
        return form.copy(
            description = description,
            sumText = sumText,
            payerId = payerId?.takeIf { it in memberIds } ?: form.payerId,
            recipientIds = recipientIds.filter { it in memberIds }.toSet().ifEmpty { form.recipientIds },
            splitType = splitType,
            amountTexts = amountTexts.filterKeys { it in memberIds },
            draftItems = draftItems,
            parseQuestions = parseQuestions,
            isItemizedLocked = isItemizedLocked,
        )
    }
}

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
    /**
     * true — операция с позициями чека (itemized): правка плоской формой затёрла
     * бы чек, поэтому сохранение запрещено (временно, до itemized-режима формы
     * в Task 10). Просмотр полей — как раньше.
     */
    val isItemizedLocked: Boolean = false,
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
    /** true — идёт AI-распознавание (parsing-оверлей со спиннером). */
    val isParsing: Boolean = false,
    /**
     * Позиции чека из AI-распознавания (Task 7 их держит и блокирует сохранение;
     * интерактивный чек и itemized-сохранение — Task 8/10). Пусто — плоский расход.
     */
    val draftItems: List<OperationItem> = emptyList(),
    /** Уточняющие вопросы модели («кто платил?») — показываются под формой. */
    val parseQuestions: List<String> = emptyList(),
    /**
     * null — ошибки распознавания нет; иначе текст с кнопкой «Повторить» (данные
     * НЕ теряются: фото сохранено в cacheDir, форма осталась как была).
     */
    val parseRetryMessage: String? = null,
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
        get() = !isItemizedLocked &&
            selectedRoomId != null &&
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
    private val savedStateHandle: SavedStateHandle,
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

    /**
     * Позиции чека редактируемой операции — проносятся в PUT НЕТРОНУТЫМИ
     * (см. [SplittyRepository.updateOperation]). Для itemized-операций правка
     * сейчас всё равно заблокирована ([AddExpenseForm.isItemizedLocked]);
     * поле — страховка от затирания чека, если правка когда-то пройдёт.
     */
    private var editOriginalItems: List<OperationItem>? = null

    /**
     * Поколение parse-запроса: новый запрос ОБГОНЯЕТ старый (пользователь во время
     * распознавания добавил фото/сменил ввод) — ответ устаревшего игнорируется.
     * Порт iOS `parseGeneration`.
     */
    private var parseGeneration = 0

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
                    val form = fixedRoomForm(room, operation, meId)
                    // Создание (не правка) — восстанавливаем черновик после process death.
                    _state.value = UiState.Content(
                        if (operation == null) restoreDraftInto(form) else form
                    )
                } else {
                    val rooms = repository.rooms(archived = false).value
                    val base = AddExpenseForm(
                        isEditing = false,
                        showsRoomPicker = true,
                        rooms = rooms,
                        meId = meId,
                    )
                    _state.value = UiState.Content(restoreDraftInto(base))
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
        editOriginalItems = payload.items
        val form = AddExpenseForm(
            isEditing = true,
            isEditingLocal = true,
            isItemizedLocked = !payload.items.isNullOrEmpty(),
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
            isItemizedLocked = !operation?.items.isNullOrEmpty(),
            showsRoomPicker = false,
            meId = meId,
        )
        if (operation != null) {
            editRecipientOrder = operation.recipients.map { it.user.id }
            editOriginalItems = operation.items
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

    // --- AI-распознавание расхода (фото чека, Task 7) ---

    /**
     * Распознать расход по фото чека: путь к JPEG (в cacheDir) сохраняется в
     * SavedStateHandle — переживает process death и нужен «Повторить». Запуск —
     * [launchParse]; обгон предыдущего запроса — через [parseGeneration].
     */
    fun parseReceiptImage(path: String) {
        savedStateHandle[KEY_RECEIPT_PATH] = path
        launchParse { readImageBytes(path) }
    }

    /**
     * Повторить распознавание последнего фото (кнопка «Повторить» на баннере
     * ошибки): диктовка/фото НЕ потеряны — читаем из сохранённого пути.
     */
    fun retryParse() {
        val path = savedStateHandle.get<String>(KEY_RECEIPT_PATH) ?: return
        launchParse { readImageBytes(path) }
    }

    /**
     * Отмена активного распознавания (кнопка на parsing-оверлее): текущий запрос
     * обесценивается поколением, форма остаётся как была. Порт iOS `cancelParse()`.
     */
    fun cancelParse() {
        parseGeneration++
        updateForm { it.copy(isParsing = false) }
    }

    /** Сбросить баннер ошибки распознавания (пользователь его закрыл). */
    fun dismissParseRetry() = updateForm { it.copy(parseRetryMessage = null) }

    /**
     * Общий запуск распознавания: помечает форму isParsing, шлёт медиа + текущий
     * черновик на /parse, применяет ответ. Ошибка НЕ теряет черновик — форма как
     * была, показывается баннер «Повторить». Новый запрос обгоняет активный
     * (см. [parseGeneration]); ответ устаревшего запроса выбрасывается.
     */
    private fun launchParse(loadImage: suspend () -> ByteArray?) {
        val form = currentForm() ?: return
        val roomId = form.selectedRoomId
        if (roomId == null) {
            updateForm { it.copy(alertMessage = "Выберите группу") }
            return
        }
        parseGeneration++
        val generation = parseGeneration
        updateForm { it.copy(isParsing = true, parseRetryMessage = null) }
        // Текущий черновик — для голосовой правки (Task 12); при первом фото — null.
        val draft = form.currentParseDraft()
        viewModelScope.launch {
            try {
                val image = loadImage()
                val response = repository.parseOperation(roomId, image = image, draft = draft)
                if (generation != parseGeneration) return@launch // обогнан — игнор
                updateForm { it.applyingParse(response).copy(isParsing = false) }
                persistDraft()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                if (generation != parseGeneration) return@launch
                updateForm { it.copy(isParsing = false, parseRetryMessage = humanErrorText(e)) }
            }
        }
    }

    private suspend fun readImageBytes(path: String): ByteArray? = withContext(Dispatchers.IO) {
        File(path).takeIf { it.exists() }?.readBytes()
    }

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
                        // items оригинала проносятся НЕТРОНУТЫМИ — плоский PUT
                        // без них затёр бы чек itemized-операции на сервере.
                        repository.updateOperation(
                            roomId, operationId, description, sum, payerId, split,
                            items = editOriginalItems,
                        )
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
        persistDraft()
    }

    // --- Черновик в SavedStateHandle (восстановление после process death) ---

    /**
     * Сохраняет текущий черновик создания расхода в SavedStateHandle. Правку
     * существующей операции НЕ персистим — она перезагружается с сервера/из outbox.
     */
    private fun persistDraft() {
        val form = currentForm() ?: return
        if (form.isEditing) return
        val snapshot = ExpenseDraftSnapshot.from(form)
        if (ExpenseDraftSnapshot.hasContent(snapshot)) {
            savedStateHandle[KEY_DRAFT] =
                SplittyJson.encodeToString(ExpenseDraftSnapshot.serializer(), snapshot)
        }
    }

    private fun savedDraft(): ExpenseDraftSnapshot? =
        savedStateHandle.get<String>(KEY_DRAFT)?.let { raw ->
            runCatching { SplittyJson.decodeFromString(ExpenseDraftSnapshot.serializer(), raw) }.getOrNull()
        }

    /**
     * Накатывает сохранённый черновик (если есть) на форму создания расхода:
     * фиксированная комната — просто поверх, пикер — сперва выбираем комнату.
     */
    private fun restoreDraftInto(base: AddExpenseForm): AddExpenseForm {
        val snapshot = savedDraft()?.takeIf { ExpenseDraftSnapshot.hasContent(it) } ?: return base
        if (!base.showsRoomPicker) {
            val sameRoom = snapshot.selectedRoomId == null || snapshot.selectedRoomId == base.selectedRoomId
            return if (sameRoom) snapshot.applyTo(base) else base
        }
        val summary = base.rooms.firstOrNull { it.id == snapshot.selectedRoomId } ?: return base
        val withRoom = appliedRoom(base, summary.id, summary.members, summary.currency)
        return snapshot.applyTo(withRoom)
    }

    private companion object {
        /** JSON-снимок черновика формы (см. [ExpenseDraftSnapshot]). */
        const val KEY_DRAFT = "expense_draft"

        /** Путь к JPEG чека в cacheDir — для «Повторить» после process death. */
        const val KEY_RECEIPT_PATH = "expense_receipt_path"
    }
}
