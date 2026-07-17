package com.zagir.splitty.ui.settleup

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.zagir.splitty.core.UiState
import com.zagir.splitty.core.model.Debt
import com.zagir.splitty.core.network.ApiException
import com.zagir.splitty.core.network.NetworkMonitor
import com.zagir.splitty.core.session.SessionStore
import com.zagir.splitty.data.SplittyRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Максимум цифр в поле суммы платежа. */
private const val MAX_SUM_DIGITS = 9

private fun digitsOnly(raw: String): String =
    raw.filter { it.isDigit() }.take(MAX_SUM_DIGITS)

/**
 * Долг для предвыбора по nav-аргументам (переход из строки балансов/друга):
 * ищем ровно тот долг «должник→кредитор». Отрицательные id (нет аргумента) и
 * несовпадение — null (тогда работает обычная логика: единственный/список).
 */
internal fun matchPreselect(debts: List<Debt>, debtorId: Long, lenderId: Long): Debt? {
    if (debtorId <= 0L || lenderId <= 0L) return null
    return debts.firstOrNull { it.debtor.id == debtorId && it.lender.id == lenderId }
}

/** Алерт экрана погашения. */
sealed interface SettleUpAlert {
    /** 409 conflict: долг уже погашен (частично/полностью) — список перечитан. */
    data object DebtSettled : SettleUpAlert

    /** Прочая ошибка API — текст показывается как есть. */
    data class Message(val text: String) : SettleUpAlert
}

/** Состояние экрана погашения долга. */
data class SettleUpForm(
    /** Валюта комнаты — в ней долги и сумма платежа. */
    val currency: String,
    val meId: Long?,
    /** Мои долги комнаты (GET debts?involving=me). */
    val debts: List<Debt>,
    /** Выбранный долг — шаг 2 «Записать платёж»; null — список (шаг 1). */
    val selectedDebt: Debt? = null,
    /** Текст поля суммы платежа (prefill полной суммой долга). */
    val sumText: String = "",
    val isSaving: Boolean = false,
    val alert: SettleUpAlert? = null,
    /** true — платёж записан, экран пора закрывать (onDone). */
    val isSaved: Boolean = false,
) {
    val sum: Int? get() = sumText.toIntOrNull()

    /** Кнопка «назад к списку» видна, когда на шаг 2 пришли из списка. */
    val showsBackToList: Boolean get() = selectedDebt != null && debts.size > 1

    /** Платёж валиден: 1 <= сумма <= текущему долгу. */
    val isSumValid: Boolean
        get() {
            val debt = selectedDebt ?: return false
            return (sum ?: 0) in 1..debt.sum
        }
}

/**
 * VM погашения долга (порт iOS SettleUpView): шаг 1 — список моих долгов
 * комнаты, шаг 2 — «Записать платёж» (POST /rooms/{id}/repayments).
 * Единственный долг с участием меня выбирается сразу. 409 conflict —
 * алерт «Долг уже погашен» и перечитывание долгов. После успеха —
 * SessionStore.noteDataChanged() и isSaved = true (экран зовёт onDone).
 */
@HiltViewModel
class SettleUpViewModel @Inject constructor(
    private val repository: SplittyRepository,
    private val sessionStore: SessionStore,
    private val networkMonitor: NetworkMonitor,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow<UiState<SettleUpForm>>(UiState.Loading)
    val state: StateFlow<UiState<SettleUpForm>> = _state.asStateFlow()

    /** Онлайн-статус — CTA гасится офлайн с подписью-причиной (не алерт по тапу). */
    val isOnline: StateFlow<Boolean> = networkMonitor.isOnline

    // Предвыбор долга из nav-аргументов (переход «Погасить» строки балансов).
    private val preselectDebtorId: Long = savedStateHandle.get<Long>("debtorId") ?: -1L
    private val preselectLenderId: Long = savedStateHandle.get<Long>("lenderId") ?: -1L

    private var isStarted = false
    private var roomId: String? = null

    /** Первичная настройка (идемпотентна — зовётся из LaunchedEffect экрана). */
    fun start(roomId: String) {
        if (isStarted) return
        isStarted = true
        this.roomId = roomId
        load()
    }

    /** Повторная загрузка после ошибки. */
    fun retry() {
        if (isStarted) load()
    }

    private fun load() {
        val roomId = roomId ?: return
        _state.value = UiState.Loading
        viewModelScope.launch {
            try {
                // Валюта — из детали комнаты, долги — только с моим участием.
                val room = repository.room(roomId).value
                val debts = repository.debts(roomId, involving = "me")
                // Единственный долг — сразу шаг 2; иначе предвыбор по nav-аргументам.
                val selected = debts.singleOrNull()
                    ?: matchPreselect(debts, preselectDebtorId, preselectLenderId)
                _state.value = UiState.Content(
                    SettleUpForm(
                        currency = room.currency,
                        meId = sessionStore.state.value?.me?.id,
                        debts = debts,
                        selectedDebt = selected,
                        sumText = selected?.sum?.toString().orEmpty(),
                    )
                )
            } catch (e: CancellationException) {
                throw e // отмена — не ошибка
            } catch (e: ApiException) {
                _state.value = UiState.Error(e.message)
            }
        }
    }

    /** Выбор долга из списка: шаг 2 с prefill полной суммой долга. */
    fun selectDebt(debt: Debt) = updateForm {
        it.copy(selectedDebt = debt, sumText = debt.sum.toString())
    }

    /** Назад к списку долгов (шаг 1). */
    fun backToList() = updateForm { it.copy(selectedDebt = null) }

    fun onSumChange(raw: String) = updateForm { it.copy(sumText = digitsOnly(raw)) }

    fun dismissAlert() = updateForm { it.copy(alert = null) }

    /**
     * Записать платёж. Защита от двойного тапа — isSaving выставляется
     * синхронно до запуска корутины. 409 conflict (долг уже погашен или
     * стал меньше) — алерт и перечитывание долгов.
     */
    fun repay() {
        val roomId = roomId ?: return
        val form = currentForm() ?: return
        val debt = form.selectedDebt ?: return
        val sum = form.sum ?: return
        if (form.isSaving || !form.isSumValid) return
        // Погашения офлайн недоступны (фиксированный дизайн v1): долг мог
        // измениться на сервере, а конфликт 409 офлайн не разрешить. CTA уже
        // заблокирован с подписью-причиной — здесь молчаливый guard, без алерта.
        if (!networkMonitor.isOnline.value) return

        updateForm { it.copy(isSaving = true) }
        viewModelScope.launch {
            try {
                repository.repay(roomId, debtorId = debt.debtor.id, lenderId = debt.lender.id, sum = sum)
                sessionStore.noteDataChanged()
                updateForm { it.copy(isSaving = false, isSaved = true) }
            } catch (e: CancellationException) {
                throw e // отмена — не ошибка
            } catch (e: ApiException) {
                if (e.status == 409) {
                    // Долг уже погашен/уменьшен конкурентным платежом:
                    // перечитываем долги и возвращаемся к списку.
                    val fresh = runCatching { repository.debts(roomId, involving = "me") }
                        .getOrNull()
                    updateForm { current ->
                        val debts = fresh ?: current.debts
                        val single = fresh?.singleOrNull()
                        current.copy(
                            isSaving = false,
                            alert = SettleUpAlert.DebtSettled,
                            debts = debts,
                            selectedDebt = single,
                            sumText = single?.sum?.toString().orEmpty(),
                        )
                    }
                } else {
                    updateForm {
                        it.copy(isSaving = false, alert = SettleUpAlert.Message(e.message))
                    }
                }
            }
        }
    }

    private fun currentForm(): SettleUpForm? {
        val state = _state.value
        return if (state is UiState.Content) state.value else null
    }

    private fun updateForm(transform: (SettleUpForm) -> SettleUpForm) {
        _state.update { state ->
            if (state is UiState.Content) UiState.Content(transform(state.value)) else state
        }
    }
}
