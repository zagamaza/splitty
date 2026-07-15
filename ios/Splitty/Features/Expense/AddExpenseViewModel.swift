import Foundation
import Observation

/// VM экрана добавления/редактирования расхода: выбор группы,
/// описание, сумма, плательщик, участники и способ деления
/// («Поровну» — канонически на сервере, «По суммам» — точные доли).
@MainActor
@Observable
final class AddExpenseViewModel {
    /// Состояние первичной загрузки (группы или участники фиксированной группы).
    enum State {
        case loading
        case loaded
        case failed(String)
    }

    private(set) var state: State = .loading
    /// Группы для выбора (когда экран открыт без фиксированной группы).
    private(set) var rooms: [RoomSummary] = []
    private(set) var selectedRoomId: String?
    /// Участники выбранной группы.
    private(set) var members: [User] = []
    /// Валюта выбранной группы — в ней сумма расхода и подсказки деления.
    private(set) var currency: String = "RUB"

    var descriptionText = ""
    var sumText = ""
    /// Кто заплатил.
    var payerId: Int?
    /// Между кем делится расход.
    var recipientIds: Set<Int> = []
    /// Способ деления: «Поровну» (дефолт) или «По суммам».
    var splitType: SplitType = .equally
    /// Тексты полей сумм по участникам (режим «По суммам»), ключ — user id.
    var amountTexts: [Int: String] = [:]
    /// Позиции чека itemized-операции, загруженные при правке (AI-распознавание).
    /// nil у обычных (плоских) операций. Несёт позиции через ViewModel, чтобы
    /// правка itemized-операции не превращала чек в плоскую (иначе PUT уйдёт без
    /// items → сервер затрёт `Operation.Items`). Композер/шит позиций и запись
    /// items в write-path подключаются в Task 13.
    var draftItems: [OperationItem]? = nil

    private(set) var isSaving = false
    var alertMessage: String?

    /// id редактируемой СИНХРОНИЗИРОВАННОЙ операции (nil — создание/локальная).
    private(set) var editOperationId: String?
    /// Редактируемая ЛОКАЛЬНАЯ запись outbox (ещё не отправленная на сервер);
    /// сохранение правит саму запись outbox, сеть не нужна.
    private(set) var editEntry: OutboxEntry?
    /// Исходный порядок получателей редактируемой операции — от него зависит
    /// раздача остатка equally-деления на сервере.
    private var editRecipientOrder: [Int] = []
    private var meId: Int?
    private var isConfigured = false

    var isEditing: Bool { editOperationId != nil || editEntry != nil }

    /// true — редактируем локальную (неотправленную) запись outbox.
    var isEditingLocalEntry: Bool { editEntry != nil }

    // MARK: Офлайн-политика

    /// Правило офлайн-редактирования (зафиксированный дизайн v1):
    /// офлайн ЗАБЛОКИРОВАНО только редактирование синхронизированной операции;
    /// создание нового расхода и правка локальной записи работают через outbox.
    /// nonisolated: чистая функция, тестируется без главного актёра.
    nonisolated static func isSaveBlockedOffline(
        isOnline: Bool,
        isEditingSyncedOperation: Bool,
        isEditingLocalEntry: Bool
    ) -> Bool {
        !isOnline && isEditingSyncedOperation && !isEditingLocalEntry
    }

    /// Блокировка сохранения при текущем состоянии сети (плашка в форме).
    func isSaveBlocked(isOnline: Bool) -> Bool {
        Self.isSaveBlockedOffline(
            isOnline: isOnline,
            isEditingSyncedOperation: editOperationId != nil,
            isEditingLocalEntry: isEditingLocalEntry
        )
    }

    /// Введённая сумма в рублях (только целые, ≥ 0).
    var sum: Int? {
        Int(sumText)
    }

    var payer: User? {
        members.first { $0.id == payerId }
    }

    /// Выбранные участники в стабильном порядке списка участников группы.
    var selectedMembers: [User] {
        members.filter { recipientIds.contains($0.id) }
    }

    // MARK: Режим «По суммам»

    /// Введённая доля участника (пустое/невалидное поле = 0).
    func enteredAmount(of userId: Int) -> Int {
        Int(amountTexts[userId] ?? "") ?? 0
    }

    /// Σ введённых долей ВЫБРАННЫХ участников (суммы снятых с выбора не считаются).
    var distributedTotal: Int {
        recipientIds.reduce(0) { $0 + enteredAmount(of: $1) }
    }

    /// Остаток нераспределённой суммы; < 0 — перерасход.
    var remainingToDistribute: Int {
        (sum ?? 0) - distributedTotal
    }

    /// true — суммы участников сходятся с суммой расхода (Σ == sum, sum ≥ 1).
    var isDistributionBalanced: Bool {
        guard let sum, sum >= 1 else { return false }
        return distributedTotal == sum
    }

    /// Доступность «Сохранить»: в режиме «По суммам» — только при Σ == sum;
    /// в режиме «Поровну» кнопка активна всегда (валидация — алертами в save).
    var canSave: Bool {
        guard splitType == .byExactAmount else { return true }
        return !recipientIds.isEmpty && isDistributionBalanced
    }

    /// Живая подпись режима «По суммам»: остаток/перерасход/готово.
    var distributionHint: String {
        guard !recipientIds.isEmpty else { return "Выберите хотя бы одного участника" }
        if isDistributionBalanced {
            return "Сумма распределена полностью"
        }
        if remainingToDistribute < 0 {
            return "Перерасход: \(money(-remainingToDistribute, currency: currency))"
        }
        return "Осталось распределить: \(money(remainingToDistribute, currency: currency))"
    }

    /// Доли получателей для `recipientSums` (контракт v2) в переданном
    /// стабильном порядке. Участники с нулевой/пустой долей ОПУСКАЮТСЯ:
    /// получатель с долей 0 не участвует в делении, а сервер отклоняет
    /// суммы < 1 (400 validation). При Σ == sum (isDistributionBalanced)
    /// пропуск нулей не меняет сумму долей — валидация сервера сходится.
    func exactRecipientSums(orderedIds: [Int]) -> [RecipientSum] {
        orderedIds.compactMap { id in
            let amount = enteredAmount(of: id)
            return amount >= 1 ? RecipientSum(userId: id, sum: amount) : nil
        }
    }

    /// Подпись под выбором участников: «1 200 ₽ / 3 = 400 ₽ с человека».
    /// При неровном делении — честный диапазон «100 ₽ / 3 = 33–34 ₽ с человека»
    /// (по каноническому правилу остаток достаётся первым получателям).
    var splitHint: String {
        let count = recipientIds.count
        guard count > 0 else { return "Выберите хотя бы одного участника" }
        guard let sum, sum >= 1 else { return "Участников: \(count)" }
        let parts = shares(sum: sum, count: count)
        guard let maxShare = parts.first, let minShare = parts.last else {
            return "Участников: \(count)"
        }
        if minShare == maxShare {
            return "\(money(sum, currency: currency)) / \(count) = \(money(minShare, currency: currency)) с человека"
        }
        let range = moneyRange(minShare, maxShare, currency: currency)
        return "\(money(sum, currency: currency)) / \(count) = \(range) с человека"
    }

    /// Первичная настройка и загрузка данных (вызывается один раз из .task).
    /// Комната/список групп читаются через `DataRepo`: офлайн форма работает
    /// на кеше (участники и валюта — из последней успешной загрузки).
    func load(
        repo: DataRepo,
        fixedRoomId: String?,
        editOperation: Operation?,
        editEntry: OutboxEntry? = nil,
        me: Me?
    ) async {
        guard !isConfigured else { return }
        isConfigured = true
        meId = me?.id

        if let editOperation {
            editOperationId = editOperation.id
            descriptionText = editOperation.description
            sumText = String(editOperation.sum)
            payerId = editOperation.donor.id
            recipientIds = Set(editOperation.recipients.map(\.user.id))
            // Исходный порядок получателей: сервер раздаёт остаток equally-деления
            // первым в массиве, поэтому при сохранении правки порядок сохраняем.
            editRecipientOrder = editOperation.recipients.map(\.user.id)
            splitType = editOperation.splitType ?? .equally
            // Позиции чека itemized-операции переносим в ViewModel, чтобы правка
            // не потеряла их: при плоском PUT сервер затрёт `Operation.Items`
            // (Task 8). Отображение/правка чека — в композере Task 13.
            draftItems = editOperation.items
            // Prefill долей из ХРАНИМЫХ сумм: для «По суммам» — точные, для
            // «Поровну» — канонические (стартовые значения при смене режима).
            amountTexts = Dictionary(
                editOperation.recipients.map { ($0.user.id, String($0.sum)) },
                uniquingKeysWith: { first, _ in first }
            )
        } else if let editEntry, let payload = editEntry.payload {
            // Локальная (неотправленная) запись outbox: prefill из payload.
            self.editEntry = editEntry
            descriptionText = payload.description
            sumText = String(payload.sum)
            payerId = payload.donorId
            if let sums = payload.recipientSums {
                splitType = .byExactAmount
                recipientIds = Set(sums.map(\.userId))
                editRecipientOrder = sums.map(\.userId)
                amountTexts = Dictionary(
                    sums.map { ($0.userId, String($0.sum)) },
                    uniquingKeysWith: { first, _ in first }
                )
            } else {
                splitType = .equally
                recipientIds = Set(payload.recipientIds ?? [])
                editRecipientOrder = payload.recipientIds ?? []
            }
        }

        state = .loading
        do {
            if let fixedRoomId {
                let room = try await repo.room(id: fixedRoomId).value
                applyRoom(id: room.id, members: room.members, currency: room.currency)
            } else {
                rooms = try await repo.rooms(archived: false).value
            }
            state = .loaded
        } catch {
            // Отмена .task (закрыли sheet) — не ошибка.
            if error.isTaskCancellation { return }
            state = .failed(error.localizedDescription)
        }
    }

    /// Повторная загрузка после ошибки.
    func retry(
        repo: DataRepo,
        fixedRoomId: String?,
        editOperation: Operation?,
        editEntry: OutboxEntry? = nil,
        me: Me?
    ) async {
        isConfigured = false
        await load(
            repo: repo,
            fixedRoomId: fixedRoomId,
            editOperation: editOperation,
            editEntry: editEntry,
            me: me
        )
    }

    /// Выбор группы из чипов: плательщик — текущий пользователь, делим на всех.
    func selectRoom(_ summary: RoomSummary) {
        guard summary.id != selectedRoomId else { return }
        recipientIds = []
        payerId = nil
        amountTexts = [:]
        applyRoom(id: summary.id, members: summary.members, currency: summary.currency)
    }

    func toggleRecipient(_ userId: Int) {
        if recipientIds.contains(userId) {
            recipientIds.remove(userId)
        } else {
            recipientIds.insert(userId)
        }
    }

    private func applyRoom(id: String, members: [User], currency: String) {
        selectedRoomId = id
        self.members = members
        self.currency = currency
        let memberIds = Set(members.map(\.id))
        recipientIds = recipientIds.intersection(memberIds)
        if recipientIds.isEmpty {
            recipientIds = memberIds
        }
        amountTexts = amountTexts.filter { memberIds.contains($0.key) }
        if let payerId, memberIds.contains(payerId) {
            // Плательщик валиден — оставляем.
        } else if let meId, memberIds.contains(meId) {
            payerId = meId
        } else {
            payerId = members.first?.id
        }
    }

    /// Валидация и сохранение. true — успех (экран можно закрывать).
    /// Офлайн-ветки (зафиксированный дизайн v1):
    /// - создание без сети → запись в outbox (отправится при синке);
    /// - правка локальной записи outbox → правка самой записи (сеть не нужна);
    /// - правка синхронизированной операции без сети → запрещена (алерт,
    ///   в форме — плашка и заблокированная кнопка).
    func save(api: APIClient, outbox: OutboxStore, isOnline: Bool) async -> Bool {
        // Защита от двойного тапа по «Сохранить»: второй Task в том же кадре
        // не должен отправить второй POST (isSaving выставляется до await).
        guard !isSaving else { return false }
        guard let roomId = selectedRoomId else {
            alertMessage = "Выберите группу"
            return false
        }
        let description = descriptionText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty else {
            alertMessage = "Введите описание расхода"
            return false
        }
        guard let sum, sum >= 1 else {
            alertMessage = "Введите сумму (целое число рублей, не меньше 1)"
            return false
        }
        guard let payerId else {
            alertMessage = "Выберите, кто заплатил"
            return false
        }
        guard !recipientIds.isEmpty else {
            alertMessage = "Выберите хотя бы одного участника"
            return false
        }
        guard !isSaveBlocked(isOnline: isOnline) else {
            alertMessage = "Нет соединения. Можно редактировать только неотправленные операции"
            return false
        }

        // Стабильный порядок id: при редактировании — исходный порядок операции
        // (сервер раздаёт остаток equally-деления первым в массиве), новые
        // участники добавляются следом; при создании — порядок списка участников.
        let kept = editRecipientOrder.filter { recipientIds.contains($0) }
        let added = members.map(\.id).filter { recipientIds.contains($0) && !kept.contains($0) }
        let ids = kept + added

        let split: ExpenseSplit
        let exactSums: [RecipientSum]?
        if splitType == .byExactAmount {
            guard isDistributionBalanced else {
                alertMessage = "Суммы участников должны сходиться с суммой расхода"
                return false
            }
            let sums = exactRecipientSums(orderedIds: ids)
            split = .byExactAmount(recipientSums: sums)
            exactSums = sums
        } else {
            split = .equally(recipientIds: ids)
            exactSums = nil
        }

        // Локальная запись outbox: правим её саму (failed сбрасывается
        // в pending — исправленная запись уйдёт при следующем синке).
        if let editEntry {
            outbox.update(
                localId: editEntry.localId,
                payload: OutboxPayload(
                    description: description,
                    sum: sum,
                    donorId: payerId,
                    recipientIds: exactSums == nil ? ids : nil,
                    recipientSums: exactSums
                )
            )
            return true
        }

        let payload = OutboxPayload(
            description: description,
            sum: sum,
            donorId: payerId,
            recipientIds: exactSums == nil ? ids : nil,
            recipientSums: exactSums
        )
        // localId создаётся заранее и служит clientOpId прямого POST:
        // если ответ потеряется, досылка из outbox не создаст дубль.
        let localId = UUID()

        // Офлайн-создание: в outbox, отправится при появлении сети.
        if editOperationId == nil, !isOnline {
            outbox.add(roomId: roomId, payload: payload, localId: localId)
            return true
        }

        isSaving = true
        defer { isSaving = false }
        do {
            if let editOperationId {
                _ = try await api.updateOperation(
                    roomId: roomId,
                    operationId: editOperationId,
                    description: description,
                    sum: sum,
                    donorId: payerId,
                    split: split
                )
            } else {
                _ = try await api.addOperation(
                    roomId: roomId,
                    description: description,
                    sum: sum,
                    donorId: payerId,
                    split: split,
                    clientOpId: localId.uuidString
                )
            }
            return true
        } catch let error as APIError {
            // Сервер недоступен при живой сети (обслуживание, упал бэкенд):
            // создание не теряем — кладём в outbox с ТЕМ ЖЕ localId (идемпотентно).
            if editOperationId == nil, case .transport = error {
                outbox.add(roomId: roomId, payload: payload, localId: localId)
                return true
            }
            alertMessage = error.localizedDescription
            return false
        } catch {
            alertMessage = error.localizedDescription
            return false
        }
    }
}
