package com.zagir.splitty.ui.expense

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.ExpenseSplit
import com.zagir.splitty.core.model.ItemShare
import com.zagir.splitty.core.model.Operation
import com.zagir.splitty.core.model.OperationItem
import com.zagir.splitty.core.model.ParseDraft
import com.zagir.splitty.core.model.ParseResponse
import com.zagir.splitty.core.model.PersonShare
import com.zagir.splitty.core.model.RecipientSum
import com.zagir.splitty.core.model.RoomDetail
import com.zagir.splitty.core.model.RoomSummary
import com.zagir.splitty.core.model.SplitType
import com.zagir.splitty.core.model.SplittyJson
import com.zagir.splitty.core.model.User
import com.zagir.splitty.core.model.derivedShares
import com.zagir.splitty.core.model.hasUnknown
import com.zagir.splitty.core.model.isSurcharge
import com.zagir.splitty.core.model.itemizedUserIds
import com.zagir.splitty.core.model.personShares
import com.zagir.splitty.core.model.shareList
import com.zagir.splitty.core.money.money
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

/** Текст нудж-тоста «выберите группу» — константа, чтобы выбор группы мог погасить его мгновенно. */
internal const val SELECT_GROUP_TOAST = "Сначала выберите группу"

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
 * Сумма операции для отправки: при itemized-чеке — Σ производных долей, иначе
 * поле ввода. Сервер требует `sum == Σ recipientSums` (иначе 400), а в
 * itemized-режиме поле суммы read-only и [AddExpenseForm.sumText] не
 * пересчитывается при правке/удалении позиции — отправлять его нельзя. Заодно
 * снимается тупик «чек распознан, общей суммы нет»: sumText пуст, а исправить
 * его негде. null — сохранять нечего. Чистая функция — под JVM-тест.
 */
internal fun effectiveSum(form: AddExpenseForm, itemSums: List<RecipientSum>?): Int? =
    (itemSums?.sumOf { it.sum } ?: form.sum)?.takeIf { it >= 1 }

/**
 * Снапшот формы до последней голосовой правки/«Поровну на всех» — для отмены
 * ([AddExpenseForm.undoingParse]). Хранит ровно то, что может измениться правкой:
 * позиции, описание, сумму и донора. Порт iOS `undoSnapshot`.
 */
@Serializable
data class UndoSnapshot(
    val draftItems: List<OperationItem> = emptyList(),
    val description: String = "",
    val sumText: String = "",
    val payerId: Long? = null,
)

/**
 * Индексы позиций, отличающихся от прежней версии черновика: изменённые по месту
 * и добавленные в конец. Удаления не подсвечиваются (строки уже нет). Порт iOS
 * `changedIndices`. Чистая функция — под JVM-тест.
 */
internal fun changedItemIndices(old: List<OperationItem>, new: List<OperationItem>): Set<Int> {
    val out = HashSet<Int>()
    new.forEachIndexed { index, item ->
        if (index >= old.size || old[index] != item) out.add(index)
    }
    return out
}

/**
 * Накатывает ответ AI-распознавания на форму (порт iOS `apply(parse:)`): описание,
 * сумму и донора берём из черновика (если модель их дала), позиции становятся
 * источником правды (itemized-операция), получатели синхронизируются с позициями.
 * Если это была ПРАВКА непустой формы (голосом/фото поверх распознанного) — кладём
 * снапшот для «Отменить» и помечаем изменённые позиции. Черновик НИКОГДА не теряется:
 * пустые поля ответа не затирают ввод. Чистая функция — покрыта юнит-тестами.
 */
internal fun AddExpenseForm.applyingParse(response: ParseResponse): AddExpenseForm {
    val draft = response.draft
    val items = draft.itemList
    val memberIds = members.map { it.id }.toSet()
    val recognizedPayer = draft.donorId?.takeIf { it in memberIds }
    val wasCorrection = didRecognize || hasDraftItems
    val oldItems = draftItems
    val oldSnapshot = UndoSnapshot(draftItems, description, sumText, payerId)

    // Распознанный плательщик — тоже результат: правка «платил Саша» возвращает
    // черновик, где заполнен только donorId. Без этого условия она считалась
    // пустым ответом, показывала «Не удалось распознать» и сносила чек.
    val recognizedSomething = draft.description.isNotBlank() || draft.sum >= 1 ||
        items.isNotEmpty() || recognizedPayer != null
    val questions = response.questionList
    // Совсем пусто и без вопросов — говорим явно, а не молча возвращаем форму.
    val emptyAlert = if (!recognizedSomething && questions.isEmpty()) {
        "Не удалось распознать. Скажите ещё раз — с блюдами и ценами"
    } else {
        null
    }

    val correction = wasCorrection && recognizedSomething
    // Правка без позиций в ответе («платил Саша») не должна стирать собранный
    // чек: пустые поля ответа не затирают ввод — это касается и позиций.
    val nextItems = if (wasCorrection && items.isEmpty()) oldItems else items
    var next = copy(
        description = draft.description.ifBlank { description },
        sumText = if (draft.sum >= 1) draft.sum.toString() else sumText,
        payerId = recognizedPayer ?: payerId,
        draftItems = nextItems,
        parseQuestions = questions,
        parseRetryMessage = null,
        didRecognize = didRecognize || recognizedSomething,
        alertMessage = emptyAlert ?: alertMessage,
        undoSnapshot = if (correction) oldSnapshot else null,
        canUndoParse = correction,
        changedItemIndices = if (correction) changedItemIndices(oldItems, nextItems) else emptySet(),
    )
    // Плоский расход (без позиций) с пустым выбором — делим на всех участников.
    next = if (nextItems.isEmpty() && next.recipientIds.isEmpty()) {
        next.copy(recipientIds = memberIds)
    } else {
        next
    }
    return next.syncingRecipientsFromItems()
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

// --- Операции над позициями чека (itemized) — чистые функции над формой ---

/**
 * Синхронизирует множество получателей с участниками позиций — чтобы плоские
 * валидации/сохранение видели тех же людей, что и чек. Порт iOS
 * `syncRecipientsFromItems`.
 */
internal fun AddExpenseForm.syncingRecipientsFromItems(): AddExpenseForm {
    if (!hasDraftItems) return this
    val ids = draftItems.itemizedUserIds()
    return if (ids.isEmpty()) this else copy(recipientIds = ids.toSet())
}

/** Заменяет позицию по индексу (правка из шита позиции). Порт iOS `replaceItem`. */
internal fun AddExpenseForm.replacingItem(index: Int, item: OperationItem): AddExpenseForm {
    if (index !in draftItems.indices) return this
    val items = draftItems.toMutableList().apply { this[index] = item }
    return copy(draftItems = items).syncingRecipientsFromItems()
}

/**
 * Удаляет позицию чека (AI мог придумать лишнюю строку — путь починить руками,
 * не передиктовывая). Последняя позиция → возврат к плоской форме. Порт iOS `deleteItem`.
 */
internal fun AddExpenseForm.deletingItem(index: Int): AddExpenseForm {
    if (index !in draftItems.indices) return this
    val items = draftItems.toMutableList().apply { removeAt(index) }
    if (items.isEmpty()) {
        // Сумму и деление надо привести к плоской форме так же, как это делает
        // «Поровну на всех». Иначе после удаления всех позиций hasDraftItems
        // становился false, effectiveSum откатывался к sumText с распознанным
        // итогом чека, и удалённые позиции сохранялись как расход на всю сумму.
        return copy(draftItems = items, changedItemIndices = emptySet())
            .copy(
                sumText = "",
                recipientIds = members.map { it.id }.toSet(),
                splitType = SplitType.EQUALLY,
            )
    }
    return copy(draftItems = items, changedItemIndices = emptySet()).syncingRecipientsFromItems()
}

/**
 * Добавляет пустую позицию (AI мог пропустить блюдо): цена 0 = «цена не определена»,
 * деление поровну на всех участников. Возвращает новую форму и индекс строки (вью
 * сразу открывает её шит) либо null, если добавлять не к чему. Порт iOS `addBlankItem`.
 */
internal fun AddExpenseForm.addingBlankItem(): Pair<AddExpenseForm, Int?> {
    if (!hasDraftItems) return this to null
    val shares = members.map { ItemShare(userId = it.id, weight = 1) }
    val items = draftItems + OperationItem(name = "", price = 0, qty = 1, shares = shares)
    return copy(draftItems = items).syncingRecipientsFromItems() to (items.size - 1)
}

/**
 * Переключает правило деления надбавки (сбор/чаевые/доставка): «пропорционально
 * съеденному» ⇄ «поровну на всех». Обычные позиции не трогает. Порт iOS
 * `toggleSurchargeRule`.
 */
internal fun AddExpenseForm.togglingSurchargeRule(index: Int): AddExpenseForm {
    val item = draftItems.getOrNull(index) ?: return this
    if (!item.isSurcharge) return this
    val newSplit = if (item.split == OperationItem.SPLIT_EQUALLY) {
        OperationItem.SPLIT_PROPORTIONAL
    } else {
        OperationItem.SPLIT_EQUALLY
    }
    return replacingItem(index, item.copy(shares = null, split = newSplit))
}

/**
 * Сопоставляет нераспознанное имя `name` в позиции участнику `userId`: имя убирается
 * из `unknown`, участник добавляется в доли позиции (вес 1). Возвращает форму с
 * подтверждающим тостом. Дозапись alias на сервере — side-effect VM. Порт iOS
 * `resolveUnknown` (без сетевой части).
 */
internal fun AddExpenseForm.resolvingUnknown(itemIndex: Int, name: String, userId: Long): AddExpenseForm {
    val item = draftItems.getOrNull(itemIndex) ?: return this
    val unknown = (item.unknown ?: emptyList()).filterNot { it.equals(name, ignoreCase = true) }
    val shares = item.shareList.toMutableList()
    if (!item.isSurcharge && shares.none { it.userId == userId }) {
        shares.add(ItemShare(userId = userId, weight = 1))
    }
    val updated = item.copy(
        shares = if (item.isSurcharge) null else shares,
        unknown = unknown.ifEmpty { null },
    )
    val member = members.firstOrNull { it.id == userId }
    val toast = member?.let { "«$name» — это ${it.displayName}. Запомнил, больше не спрошу" }
    return replacingItem(itemIndex, updated).copy(toastMessage = toast ?: toastMessage)
}

/**
 * Сброс распознанного чека (ручная правка суммы): позиции больше не источник
 * правды, операция сохранится плоской (равное деление). Порт iOS `resetItems`.
 */
internal fun AddExpenseForm.resettingItems(): AddExpenseForm {
    if (!hasDraftItems) return this
    val recipients = recipientIds.ifEmpty { members.map { it.id }.toSet() }
    return copy(
        draftItems = emptyList(),
        parseQuestions = emptyList(),
        splitType = SplitType.EQUALLY,
        recipientIds = recipients,
    )
}

/**
 * «Поровну на всех»: выбрасывает позиции, оставляя плоскую сумму. Деструктивно
 * для распознанного чека — кладём снапшот, чтобы баннер «Отменить» вернул всё как
 * было (тот же механизм undo). Порт iOS `collapseToEqualSplit`.
 */
internal fun AddExpenseForm.collapsingToEqualSplit(): AddExpenseForm {
    if (!hasDraftItems) return this
    val snapshot = UndoSnapshot(draftItems, description, sumText, payerId)
    val total = draftItems.derivedShares()?.total
    return copy(
        undoSnapshot = snapshot,
        canUndoParse = true,
        changedItemIndices = emptySet(),
        sumText = total?.toString() ?: sumText,
        draftItems = emptyList(),
        recipientIds = members.map { it.id }.toSet(),
        splitType = SplitType.EQUALLY,
    )
}

/** Откат последней голосовой правки/«Поровну» к снапшоту формы. Порт iOS `undoParse`. */
internal fun AddExpenseForm.undoingParse(): AddExpenseForm {
    val snapshot = undoSnapshot ?: return this
    return copy(
        draftItems = snapshot.draftItems,
        description = snapshot.description,
        sumText = snapshot.sumText,
        payerId = snapshot.payerId,
        undoSnapshot = null,
        canUndoParse = false,
        changedItemIndices = emptySet(),
        parseQuestions = emptyList(),
    ).syncingRecipientsFromItems()
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
    val didRecognize: Boolean = false,
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
            didRecognize = form.didRecognize,
        )

        /**
         * true — в снимке есть что восстанавливать (не пустой стартовый черновик).
         * Выбранная группа считается содержимым: диктовка с экрана «Записано»
         * переживает смерть процесса (KEY_PENDING_AUDIO), и без выбранной группы
         * [launchParse] откажет, а полноэкранный оверлей закроет чипы выбора.
         */
        fun hasContent(s: ExpenseDraftSnapshot): Boolean =
            s.selectedRoomId != null ||
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
            didRecognize = didRecognize,
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
     * Позиции чека itemized-операции: источник правды при [hasDraftItems] —
     * форма показывает интерактивный чек, сохранение шлёт [draftItems] в POST/PUT.
     * Пусто — плоский расход.
     */
    val draftItems: List<OperationItem> = emptyList(),
    /** Уточняющие вопросы модели («кто платил?») — показываются под формой. */
    val parseQuestions: List<String> = emptyList(),
    /** true — форма заполнена распознаванием (голос/фото), а не вручную. */
    val didRecognize: Boolean = false,
    /** Индексы позиций, изменённых последней голосовой правкой (подсветка в чеке). */
    val changedItemIndices: Set<Int> = emptySet(),
    /** true — доступна отмена последней голосовой правки/«Поровну на всех». */
    val canUndoParse: Boolean = false,
    /** Снапшот формы до последней правки — для [undoingParse]; null — отменять нечего. */
    val undoSnapshot: UndoSnapshot? = null,
    /** Короткое подтверждение действия (тост «…Запомнил»); null — тоста нет. */
    val toastMessage: String? = null,
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

    // --- Позиции чека (itemized) ---

    /** true — есть распознанный чек: форма показывает карточку-чек вместо плоского деления. */
    val hasDraftItems: Boolean get() = draftItems.isNotEmpty()

    /** true — хотя бы в одной позиции есть нераспознанное имя (блокирует «Сохранить»). */
    val hasUnknownItems: Boolean get() = draftItems.any { it.hasUnknown }

    /**
     * true — есть позиция без цены (price < 1): модель услышала блюдо/сбор, но не цену.
     * Сборы учитываются наравне с позициями: нулевой сбор так же валит derivedShares,
     * и без чипа «цена?» блокировка сохранения оставалась необъяснённой.
     */
    val hasPricelessItems: Boolean get() = draftItems.any { it.price < 1 }

    /** Первое нераспознанное имя — для подсказки «выберите, кто такой …». */
    val firstUnknownName: String? get() = draftItems.firstNotNullOfOrNull { it.unknown?.firstOrNull() }

    /** userId'ы участников из позиций (обычные позиции, стабильный порядок появления). */
    val itemizedUserIds: List<Long> get() = draftItems.itemizedUserIds()

    /** Клиентское превью долей по позициям (userId→сумма); null — позиции невалидны. */
    val itemizedShares: Map<Long, Int>? get() = draftItems.derivedShares()?.shares

    /** Итог чека: подытог позиций + сборы; null — позиции невалидны. */
    val itemizedTotal: Int? get() = draftItems.derivedShares()?.total

    /** Подытог обычных позиций (без надбавок). */
    val itemizedSubtotal: Int get() = draftItems.filter { !it.isSurcharge }.sumOf { it.price }

    /** Сумма всех надбавок (сборов/чаевых/доставки). */
    val itemizedSurcharges: Int get() = draftItems.filter { it.isSurcharge }.sumOf { it.price }

    /** Разбивка «С кого сколько» по позициям; null — позиций нет или они невалидны. */
    val personShares: List<PersonShare>? get() = draftItems.personShares()

    /** true — форма пуста (нет позиций/описания/суммы и это не правка). */
    val isEmptyForm: Boolean
        get() = !hasDraftItems && !isEditing && description.isBlank() && (sum ?: 0) == 0

    /**
     * «Что осталось уточнить» для экрана диктовки (Task 12): нераспознанные имена,
     * позиции без цены и вопросы модели, не дублирующие первые два. Порт iOS
     * `missingInfoHints`.
     */
    val missingInfoHints: List<String>
        get() {
            val hints = ArrayList<String>()
            val covered = ArrayList<String>()
            for (item in draftItems) {
                for (name in item.unknown ?: emptyList()) {
                    hints.add("Кто это — «$name»?")
                    covered.add(name.lowercase())
                }
            }
            for (item in draftItems) {
                if (item.isSurcharge || item.price >= 1) continue
                val name = item.name.ifEmpty { "позиция" }
                hints.add("Сколько стоит «$name»?")
                covered.add(name.lowercase())
            }
            for (question in parseQuestions) {
                val lower = question.lowercase()
                if (covered.any { lower.contains(it) }) continue
                hints.add(question)
            }
            return hints.take(3)
        }

    /**
     * Доступность «Сохранить» (порт iOS `canSave`):
     * - itemized-черновик: нельзя с нераспознанными именами / без цен / при
     *   невыводимых долях; расхождение плоского sum с Σ позиций НЕ мешает —
     *   суммы выводит сервер;
     * - «По суммам» — только при Σ == sum;
     * - «Поровну» — активна всегда (остальную валидацию делает [save] алертами).
     */
    val canSave: Boolean
        get() {
            if (hasDraftItems) {
                if (hasUnknownItems) return false
                if (hasPricelessItems) return false
                return draftItems.derivedShares() != null
            }
            if (splitType == SplitType.BY_EXACT_AMOUNT) {
                return recipientIds.isNotEmpty() && isDistributionBalanced
            }
            return true
        }

    /**
     * Живая подпись режима «По суммам»: остаток/перерасход/готово (порт iOS
     * `distributionHint`). Строится через [money] — чистая, годна для тоста и теста.
     */
    val distributionHint: String
        get() = when {
            recipientIds.isEmpty() -> "Выберите хотя бы одного участника"
            isDistributionBalanced -> "Сумма распределена полностью"
            remainingToDistribute < 0 -> "Перерасход: ${money(-remainingToDistribute, currency)}"
            else -> "Осталось распределить: ${money(remainingToDistribute, currency)}"
        }

    /**
     * Почему «Сохранить» заблокирована — для нуджа по тапу (кнопка живая и
     * объясняет причину тостом, а не молча игнорирует). null — сохранять можно.
     * Порт iOS `saveBlockedReason`.
     */
    val saveBlockedReason: String?
        get() {
            if (hasDraftItems) {
                if (hasUnknownItems) return "Сначала выберите, кто есть кто в позициях"
                if (hasPricelessItems) return "Укажите цены позиций — без них не посчитать доли"
                if (draftItems.derivedShares() == null) return "Проверьте позиции чека — доли не сходятся"
                return null
            }
            if (splitType == SplitType.BY_EXACT_AMOUNT && (!isDistributionBalanced || recipientIds.isEmpty())) {
                return distributionHint
            }
            return null
        }
}

/**
 * VM формы добавления/редактирования расхода: выбор группы (когда экран
 * открыт с центральной «+»), описание, сумма, донор и способ деления —
 * «Поровну» (recipientIds, доли раскладывает сервер), «По суммам»
 * (recipientSums, Σ == sum) или itemized-чек (draftItems → производные суммы).
 * После успешного сохранения — SessionStore.noteDataChanged() и isSaved = true.
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
            // Позиции чека редактируемой локальной записи — сразу источник правды.
            draftItems = payload.items.orEmpty(),
            didRecognize = !payload.items.isNullOrEmpty(),
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
                // Позиции чека itemized-операции — источник правды: правка идёт
                // через интерактивный чек, items уходят в PUT (не затираются).
                draftItems = operation.items.orEmpty(),
                didRecognize = !operation.items.isNullOrEmpty(),
            )
        }
        return appliedRoom(form, room.id, room.members, room.currency)
    }

    /** Выбор группы из чипов: делим на всех, платит текущий пользователь. */
    fun selectRoom(summary: RoomSummary) {
        updateForm { form ->
            if (form.selectedRoomId == summary.id) {
                // Нудж «выберите группу» выполнен — гасим именно его тост сразу.
                if (form.toastMessage == SELECT_GROUP_TOAST) form.copy(toastMessage = null) else form
            } else {
                // Позиции чека привязаны к участникам ПРЕЖНЕЙ группы: оставить их
                // — значит уйти в itemized-ветку сохранения с чужими userId
                // (400, а при пересечении составов — деньги не на тех людей).
                // Снапшот «Отменить» относится к тому же чеку и тоже недействителен.
                val applied = appliedRoom(
                    form.copy(
                        recipientIds = emptySet(),
                        payerId = null,
                        amountTexts = emptyMap(),
                        draftItems = emptyList(),
                        undoSnapshot = null,
                        canUndoParse = false,
                        changedItemIndices = emptySet(),
                    ),
                    summary.id,
                    summary.members,
                    summary.currency,
                )
                if (applied.toastMessage == SELECT_GROUP_TOAST) applied.copy(toastMessage = null) else applied
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

    /**
     * Ручная правка суммы: если форма показывала распознанный чек — позиции
     * больше не источник правды (сброс к плоскому равному делению, порт iOS
     * `resetItems` по фокусу поля суммы).
     */
    fun onSumChange(raw: String) = updateForm {
        val reset = if (it.hasDraftItems) it.resettingItems() else it
        reset.copy(sumText = digitsOnly(raw))
    }

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

    fun dismissToast() = updateForm { it.copy(toastMessage = null) }

    /** Показать короткий тост (причина блокировки «Сохранить», подтверждение). */
    fun showToast(message: String) = updateForm { it.copy(toastMessage = message) }

    /** Нудж «выберите группу»: тост показывается, поле группы вью встряхивает. */
    fun nudgeSelectGroup() = updateForm { it.copy(toastMessage = SELECT_GROUP_TOAST) }

    // --- Правка позиций чека (itemized) ---

    /** Заменяет позицию по индексу (write-back шита позиции). */
    fun replaceItem(index: Int, item: OperationItem) = updateForm { it.replacingItem(index, item) }

    /** Удаляет позицию (шит «Удалить»); последняя — возврат к плоской форме. */
    fun deleteItem(index: Int) = updateForm { it.deletingItem(index) }

    /** Добавляет пустую позицию; возвращает её индекс (вью открывает шит) либо null. */
    fun addBlankItem(): Int? {
        var newIndex: Int? = null
        updateForm { form ->
            val (next, index) = form.addingBlankItem()
            newIndex = index
            next
        }
        return newIndex
    }

    /** Переключает правило деления сбора (пропорционально ⇄ поровну). */
    fun toggleSurchargeRule(index: Int) = updateForm { it.togglingSurchargeRule(index) }

    /** «Поровну на всех»: сброс позиций (обратимо баннером «Отменить»). */
    fun collapseToEqualSplit() = updateForm { it.collapsingToEqualSplit() }

    /** Откат последней голосовой правки/«Поровну» к снапшоту. */
    fun undoParse() = updateForm { it.undoingParse() }

    /** Принять правку (баннер отмены закрывается, снапшот выбрасывается). */
    fun dismissUndo() = updateForm { it.copy(canUndoParse = false, undoSnapshot = null) }

    /** Гасит подсветку изменённых позиций (по таймеру из вью). */
    fun clearChangeHighlights() = updateForm { it.copy(changedItemIndices = emptySet()) }

    /**
     * Сопоставляет нераспознанное имя участнику: локально применяет доли и
     * тост, а alias дозаписывает на сервере (best-effort — ошибка не критична).
     */
    fun resolveUnknown(itemIndex: Int, name: String, userId: Long) {
        updateForm { it.resolvingUnknown(itemIndex, name, userId) }
        viewModelScope.launch { repository.addAlias(userId, name) }
    }

    // --- AI-распознавание расхода (фото чека, Task 7) ---

    /**
     * Распознать расход по фото чека: путь к JPEG (в cacheDir) сохраняется в
     * SavedStateHandle — переживает process death и нужен «Повторить». Фото
     * уходит ВМЕСТЕ с уже записанным голосом (если он был): Gemini сопоставляет
     * цены с чека и распределение из голоса в одном запросе. Запуск — [launchParse].
     */
    fun parseReceiptImage(path: String) {
        savedStateHandle[KEY_RECEIPT_PATH] = path
        // Экран «Записано» гасим, ТОЛЬКО если распознавание действительно пошло:
        // иначе (форма ещё грузится или не загрузилась) голос остался бы
        // приложенным, а отменить или распознать его было бы уже нечем.
        if (launchParse()) savedStateHandle[KEY_PENDING_AUDIO] = null
    }

    /**
     * Распознать расход по голосу: путь к WAV (в cacheDir) сохраняется в
     * SavedStateHandle (переживает process death, нужен для «Повторить»).
     * Голос уходит вместе с уже приложенным фото чека (Task 12). Новая диктовка
     * ЗАМЕНЯЕТ предыдущий голос (перезапись KEY_AUDIO_PATH).
     */
    fun parseVoice(audioPath: String) {
        savedStateHandle[KEY_AUDIO_PATH] = audioPath
        // Гасим экран «Записано», только если распознавание реально стартовало
        // (см. [parseReceiptImage]).
        if (launchParse()) savedStateHandle[KEY_PENDING_AUDIO] = null
    }

    /**
     * Диктовка, ожидающая решения на экране «Записано» (фото / распознать /
     * отмена), или null. Живёт в SavedStateHandle, а не в state композиции:
     * иначе поворот экрана или смерть процесса убирали бы оверлей, оставляя
     * уже приложенный к форме голос без способа его увидеть и отменить.
     */
    val pendingAudioPath: StateFlow<String?> =
        savedStateHandle.getStateFlow<String?>(KEY_PENDING_AUDIO, null)

    /**
     * Приложить голос к форме БЕЗ запуска распознавания (экран «Записано» →
     * «Добавить фото чека»): путь к WAV сохраняется, чтобы последующий
     * [parseReceiptImage] отправил голос и фото одним запросом.
     */
    fun attachAudio(audioPath: String) {
        savedStateHandle[KEY_AUDIO_PATH] = audioPath
        savedStateHandle[KEY_PENDING_AUDIO] = audioPath
    }

    /**
     * Отбросить приложенную диктовку («Отменить запись» на экране «Записано»):
     * иначе следующее фото чека ушло бы вместе с отменённым голосом.
     */
    fun discardAudio() {
        savedStateHandle.remove<String>(KEY_AUDIO_PATH)
        savedStateHandle[KEY_PENDING_AUDIO] = null
    }

    /**
     * Повторить распознавание (кнопка «Повторить» на баннере ошибки): диктовка
     * и фото НЕ потеряны — читаем из сохранённых путей. Нет ни голоса, ни фото —
     * повторять нечего.
     */
    fun retryParse() {
        val hasMedia = savedStateHandle.get<String>(KEY_AUDIO_PATH) != null ||
            savedStateHandle.get<String>(KEY_RECEIPT_PATH) != null
        if (hasMedia) launchParse()
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
     * Общий запуск распознавания: помечает форму isParsing, шлёт медиа (голос
     * и/или фото из сохранённых путей) + текущий черновик на /parse, применяет
     * ответ. Ошибка НЕ теряет черновик — форма как была, показывается баннер
     * «Повторить». Новый запрос обгоняет активный (см. [parseGeneration]); ответ
     * устаревшего запроса выбрасывается. Возвращает false, если запускать нечего
     * (форма ещё не загрузилась или группа не выбрана) — вызывающий по этому
     * признаку решает, гасить ли экран «Записано».
     */
    private fun launchParse(): Boolean {
        val form = currentForm() ?: return false
        val roomId = form.selectedRoomId
        if (roomId == null) {
            updateForm { it.copy(alertMessage = "Выберите группу") }
            return false
        }
        parseGeneration++
        val generation = parseGeneration
        updateForm { it.copy(isParsing = true, parseRetryMessage = null) }
        // Текущий черновик — для голосовой правки (Task 12); при первом фото — null.
        val draft = form.currentParseDraft()
        val audioPath = savedStateHandle.get<String>(KEY_AUDIO_PATH)
        val imagePath = savedStateHandle.get<String>(KEY_RECEIPT_PATH)
        viewModelScope.launch {
            try {
                val audio = audioPath?.let { readBytes(it) }
                val image = imagePath?.let { readBytes(it) }
                // Пути живут в SavedStateHandle, а файлы — в cacheDir: система могла
                // вычистить кеш между записью и «Повторить». Без этой проверки на
                // сервер ушёл бы запрос без медиа и вернулся невнятный 400.
                if (audio == null && image == null) {
                    savedStateHandle.remove<String>(KEY_AUDIO_PATH)
                    savedStateHandle.remove<String>(KEY_RECEIPT_PATH)
                    savedStateHandle[KEY_PENDING_AUDIO] = null
                    updateForm {
                        it.copy(
                            isParsing = false,
                            parseRetryMessage = null,
                            alertMessage = "Запись не сохранилась — надиктуйте или снимите чек заново",
                        )
                    }
                    return@launch
                }
                val response = repository.parseOperation(roomId, audio = audio, image = image, draft = draft)
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
        return true
    }

    /** Байты медиа или null, если файл не пережил очистку кеша/смерть процесса (пустой — тоже null). */
    private suspend fun readBytes(path: String): ByteArray? = withContext(Dispatchers.IO) {
        File(path).takeIf { it.isFile && it.length() > 0 }?.readBytes()
    }

    /**
     * Сохранение: POST /operations, PUT (правка серверной) либо правка записи
     * outbox (локальная). itemized-чек: позиции — источник правды, отправляем
     * их вместе с производными recipientSums (сервер выводит суммы сам, но тело
     * обязано нести валидный способ деления). Создание идемпотентно (clientOpId).
     * Защита от двойного тапа — isSaving выставляется синхронно до корутины.
     */
    fun save() {
        val form = currentForm() ?: return
        if (form.isSaving) return
        // Причина блокировки объясняется тостом во вью — здесь тихий выход.
        if (form.saveBlockedReason != null) return
        val roomId = form.selectedRoomId
        if (roomId == null) {
            updateForm { it.copy(alertMessage = "Выберите группу") }
            return
        }
        val description = form.description.trim()
        if (description.isEmpty()) {
            updateForm { it.copy(alertMessage = "Введите описание расхода") }
            return
        }
        val orderedIds = orderedRecipientIds(form)
        val itemSums = itemizedRecipientSums(form, orderedIds)
        // itemized: сумма выводится из позиций, а не из поля ввода. Поле в этом
        // режиме read-only (DerivedTotal), и form.sum не пересчитывается при
        // правке/удалении позиции — отправив его, мы бы разошлись с Σ долей и
        // получили 400 «сумма долей должна равняться сумме операции». Плюс при
        // чеке без распознанной общей суммы form.sum пуст, а исправить его
        // руками негде — сохранение было тупиком.
        val sum = effectiveSum(form, itemSums)
        if (sum == null) {
            updateForm { it.copy(alertMessage = "Введите сумму (целое число рублей, не меньше 1)") }
            return
        }
        val payerId = form.payerId
        if (payerId == null) {
            updateForm { it.copy(alertMessage = "Выберите, кто заплатил") }
            return
        }
        if (form.recipientIds.isEmpty()) {
            updateForm { it.copy(alertMessage = "Выберите хотя бы одного участника") }
            return
        }
        // Правка серверной операции офлайн невозможна (страховка от гонки).
        if (!canSaveExpenseOffline(form.isEditingSynced, isOnline.value)) {
            updateForm {
                it.copy(alertMessage = "Нет соединения. Можно редактировать только неотправленные операции")
            }
            return
        }

        val itemsToSend: List<OperationItem>?
        val split: ExpenseSplit
        if (itemSums != null) {
            // itemized-чек: производные суммы + сами позиции.
            itemsToSend = form.draftItems
            split = ExpenseSplit.ByExactAmount(itemSums)
        } else if (form.splitType == SplitType.BY_EXACT_AMOUNT) {
            // Участники с нулевой/пустой долей опускаются: сервер отклоняет
            // суммы < 1, а при Σ == sum пропуск нулей суммы долей не меняет.
            itemsToSend = null
            split = ExpenseSplit.ByExactAmount(
                orderedIds.mapNotNull { id ->
                    form.enteredAmount(id).takeIf { it >= 1 }?.let { RecipientSum(userId = id, sum = it) }
                }
            )
        } else {
            itemsToSend = null
            split = ExpenseSplit.Equally(orderedIds)
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
                            OutboxPayload.of(description, sum, payerId, split, items = itemsToSend),
                        )
                        outboxSyncer.syncNow()
                    }

                    operationId != null -> {
                        repository.updateOperation(
                            roomId, operationId, description, sum, payerId, split,
                            items = itemsToSend,
                        )
                        sessionStore.noteDataChanged()
                    }

                    else -> createOperation(roomId, description, sum, payerId, split, itemsToSend)
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
     * Производные по позициям recipientSums в стабильном порядке [orderedIds]
     * (недостающие из позиций добавляются следом); null — позиций нет или они
     * невалидны. Порт iOS `itemizedRecipientSums`.
     */
    internal fun itemizedRecipientSums(form: AddExpenseForm, orderedIds: List<Long>): List<RecipientSum>? {
        if (!form.hasDraftItems) return null
        val shares = form.itemizedShares ?: return null
        val itemIds = form.itemizedUserIds
        val ordered = orderedIds.filter { it in itemIds } + itemIds.filter { it !in orderedIds }
        return ordered.mapNotNull { id ->
            shares[id]?.takeIf { it >= 1 }?.let { RecipientSum(userId = id, sum = it) }
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
        items: List<OperationItem>?,
    ) {
        val localId = UUID.randomUUID().toString()
        if (isOnline.value) {
            try {
                repository.addOperation(roomId, description, sum, payerId, split, items = items, clientOpId = localId)
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
                payload = OutboxPayload.of(description, sum, payerId, split, items = items),
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
    internal fun orderedRecipientIds(form: AddExpenseForm): List<Long> {
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
        } else {
            // Иначе удалённый черновик воскресал: «Отменить» (или очистка всех
            // позиций) → process death → restoreDraftInto накатывал устаревший
            // снимок, и распознанный чек возвращался вопреки отмене.
            savedStateHandle.remove<String>(KEY_DRAFT)
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

        /** Путь к WAV голоса в cacheDir — для «Повторить» и досыла с фото (Task 12). */
        const val KEY_AUDIO_PATH = "expense_audio_path"

        /** Путь диктовки, ожидающей решения на экране «Записано» (см. [pendingAudioPath]). */
        const val KEY_PENDING_AUDIO = "expense_pending_audio"
    }
}
