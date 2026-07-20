import Foundation
import Observation

/// Строка разбивки «С кого сколько» itemized-черновика: итог участника и
/// сколько из него добавили сборы (для подписи «+N ₽ сбор»).
struct PersonShare: Identifiable, Hashable {
    let userId: Int
    /// Полная доля: позиции + сборы (ровно то, что сохранит сервер).
    let total: Int
    /// Часть итога, пришедшая от сборов/чаевых; 0 — сборов нет.
    let surchargePart: Int

    var id: Int { userId }
}

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

    /// true — идёт AI-распознавание (`POST /parse`): спиннер в композере/нижней
    /// панели, кнопки записи заблокированы. Ошибка распознавания НЕ теряет черновик.
    private(set) var isParsing = false
    /// Индексы позиций, изменённых/добавленных последней голосовой правкой —
    /// чек подсвечивает их, чтобы было видно, ЧТО именно поменялось.
    private(set) var changedItemIndices: Set<Int> = []
    /// true — доступна отмена последней голосовой правки (`undoParse`).
    private(set) var canUndoParse = false
    /// Снапшот формы до последней голосовой правки.
    private var undoSnapshot: (items: [OperationItem]?, description: String, sum: String, payer: Int?)?
    /// Короткое подтверждение действия («Саня — это Александр. Запомнил»);
    /// UI показывает тостом и гасит сам.
    var toastMessage: String?
    /// Форма заполнена распознаванием (голос/фото), а не вручную. Нужно, чтобы
    /// плоский AI-результат (без позиций) не выглядел как обычный ручной ввод.
    private(set) var didRecognize = false
    /// Уточняющие вопросы модели из последнего ответа («кто платил?») — показываем
    /// подсказкой под чеком; пусто — вопросов нет.
    var parseQuestions: [String] = []

    private(set) var isSaving = false
    /// Сохранение уже завершилось успехом — экран закрывается. Защёлка нужна
    /// офлайн-веткам: они не делают await, поэтому `isSaving` успевал сброситься
    /// в defer до второго тапа, и двойной тап клал в outbox два одинаковых расхода
    /// с разными clientOpId (дедуп по нему не срабатывал).
    private var didSave = false
    var alertMessage: String?
    /// Ошибка распознавания с возможностью повторить (запись сохранена во вью):
    /// отдельно от `alertMessage`, чтобы алерт мог предложить «Повторить».
    var parseRetryMessage: String?

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

    /// Доступность «Сохранить»:
    /// - itemized-черновик (есть позиции): нельзя сохранить с нераспознанными
    ///   именами (`hasUnknownItems` → сервер тоже вернёт 400) и при невыводимых
    ///   долях (перебор фиксов и т.п.); сумму выводит сервер, поэтому расхождение
    ///   плоского `sum` с Σ позиций сохранению НЕ мешает;
    /// - «По суммам» — только при Σ == sum;
    /// - «Поровну» — активна всегда (валидация — алертами в save).
    var canSave: Bool {
        if hasDraftItems {
            if hasUnknownItems { return false }
            if hasPricelessItems { return false }
            return draftItemList.derivedShares() != nil
        }
        guard splitType == .byExactAmount else { return true }
        return !recipientIds.isEmpty && isDistributionBalanced
    }

    /// Почему «Сохранить» заблокирована — для нуджа по тапу (кнопка живая
    /// и объясняет причину, а не молча игнорирует). nil — сохранять можно.
    var saveBlockedReason: String? {
        if hasDraftItems {
            if hasUnknownItems { return "Сначала выберите, кто есть кто в позициях" }
            if hasPricelessItems { return "Укажите цены позиций — без них не посчитать доли" }
            if draftItemList.derivedShares() == nil { return "Проверьте позиции чека — доли не сходятся" }
            return nil
        }
        if splitType == .byExactAmount, !isDistributionBalanced || recipientIds.isEmpty {
            return distributionHint
        }
        return nil
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

    // MARK: AI-черновик (позиции чека)

    /// Позиции без опциональности; пусто — обычная (плоская) операция.
    var draftItemList: [OperationItem] { draftItems ?? [] }

    /// true — есть распознанный чек: показываем карточку-чек вместо плоского
    /// деления, микрофон переезжает в нижнюю панель.
    var hasDraftItems: Bool { !draftItemList.isEmpty }

    /// true — хотя бы в одной позиции есть нераспознанное имя (блокирует «Сохранить»).
    var hasUnknownItems: Bool { draftItemList.contains(where: \.hasUnknown) }

    /// true — есть обычная позиция без цены (price=0, «цена не определена»):
    /// модель услышала блюдо и участников, но не цену. Сохранение заблокировано,
    /// чек помечает такие позиции «цена?».
    var hasPricelessItems: Bool {
        draftItemList.contains { !$0.isSurcharge && $0.price < 1 }
    }

    /// Первое нераспознанное имя — для подсказки «выберите, кто такой …».
    var firstUnknownName: String? {
        for item in draftItemList {
            if let name = item.unknown?.first { return name }
        }
        return nil
    }

    /// true — форма пуста (нет позиций/описания/суммы и это не правка): показываем
    /// крупный композер (микрофон + «Сфотографировать чек») на весь блок.
    var isEmptyForm: Bool {
        !hasDraftItems
            && !isEditing
            && descriptionText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && (sum ?? 0) == 0
    }

    /// userId'ы участников из позиций (обычные позиции, стабильный порядок появления).
    /// Надбавки делятся по базе, поэтому их доли сюда не входят.
    var itemizedUserIds: [Int] {
        var seen: Set<Int> = []
        var ordered: [Int] = []
        for item in draftItemList where !item.isSurcharge {
            for share in item.shareList where !seen.contains(share.userId) {
                seen.insert(share.userId)
                ordered.append(share.userId)
            }
        }
        return ordered
    }

    /// Клиентское превью долей по позициям (зеркало серверного `DeriveShares`):
    /// userId→сумма или nil, если позиции невалидны (перебор фиксов и т.п.).
    var itemizedShares: [Int: Int]? {
        draftItemList.derivedShares()?.shares
    }

    /// Итог чека: подытог позиций + сборы (то, что сохранит сервер); nil при невалидных позициях.
    var itemizedTotal: Int? {
        draftItemList.derivedShares()?.total
    }

    /// Подытог обычных позиций (без надбавок).
    var itemizedSubtotal: Int {
        draftItemList.filter { !$0.isSurcharge }.reduce(0) { $0 + $1.price }
    }

    /// Сумма всех надбавок (сборов/чаевых/доставки).
    var itemizedSurcharges: Int {
        draftItemList.filter { $0.isSurcharge }.reduce(0) { $0 + $1.price }
    }

    /// Разбивка «С кого сколько» по позициям в стабильном порядке появления
    /// участников в чеке; nil — позиций нет или они невалидны (перебор фиксов).
    /// Суммы — точное зеркало серверного расчёта (`derivedShares`).
    var personShares: [PersonShare]? {
        guard hasDraftItems, let derived = draftItemList.derivedShares() else { return nil }
        let base = draftItemList.filter { !$0.isSurcharge }.derivedShares()?.shares ?? [:]
        return itemizedUserIds.compactMap { id in
            guard let total = derived.shares[id] else { return nil }
            return PersonShare(userId: id, total: total, surchargePart: total - (base[id] ?? 0))
        }
    }

    /// Переключает правило деления надбавки (сбор/чаевые/доставка):
    /// «пропорционально съеденному» ⇄ «поровну на всех». Обычные позиции не трогает.
    func toggleSurchargeRule(at index: Int) {
        guard var items = draftItems, items.indices.contains(index),
              items[index].isSurcharge else { return }
        let item = items[index]
        let newSplit = item.split == OperationItem.splitEqually
            ? OperationItem.splitProportional
            : OperationItem.splitEqually
        items[index] = OperationItem(
            name: item.name,
            price: item.price,
            qty: item.qty,
            shares: nil,
            kind: item.kind,
            split: newSplit,
            percent: item.percent,
            unknown: item.unknown
        )
        draftItems = items
    }

    /// Удаляет позицию чека (AI мог придумать лишнюю строку — путь починить
    /// руками, не передиктовывая). Последняя позиция → возврат к плоской форме.
    func deleteItem(at index: Int) {
        guard var items = draftItems, items.indices.contains(index) else { return }
        items.remove(at: index)
        draftItems = items.isEmpty ? nil : items
        changedItemIndices = []
        syncRecipientsFromItems()
    }

    /// Добавляет пустую позицию (AI мог пропустить блюдо): цена 0 = «цена не
    /// определена», деление поровну на всех участников. Возвращает индекс
    /// новой строки — вью сразу открывает её шит.
    func addBlankItem() -> Int? {
        guard hasDraftItems, var items = draftItems else { return nil }
        let shares = members.map { ItemShare(userId: $0.id, weight: 1) }
        items.append(OperationItem(name: "", price: 0, qty: 1, shares: shares))
        draftItems = items
        syncRecipientsFromItems()
        return items.count - 1
    }

    /// «Поровну на всех»: выбрасывает позиции, оставляя плоскую сумму.
    /// Деструктивно для распознанного чека — сохраняем снапшот, чтобы
    /// баннер «Отменить» мог вернуть всё как было (тот же механизм undoParse).
    func collapseToEqualSplit() {
        guard hasDraftItems else { return }
        undoSnapshot = (items: draftItems, description: descriptionText,
                        sum: sumText, payer: payerId)
        canUndoParse = true
        changedItemIndices = []
        // Сумму переносим ВСЕГДА: при невалидных позициях itemizedTotal == nil,
        // и старое `if let` оставляло в поле сумму от прежнего разбора —
        // плоский расход сохранялся с чужим итогом. Fallback — подытог + сборы.
        sumText = String(itemizedTotal ?? (itemizedSubtotal + itemizedSurcharges))
        draftItems = nil
        // Как в resetItems: без позиций деление снова каноническое «поровну»,
        // иначе остаётся режим «По суммам» с долями от чека.
        splitType = .equally
        recipientIds = Set(members.map(\.id))
    }

    /// Применяет ответ AI-распознавания к форме: описание, сумма, донор, позиции.
    /// Позиции становятся источником правды (itemized-операция); участники
    /// синхронизируются с позициями. Если это была ПРАВКА непустой формы —
    /// запоминает снапшот для «Отменить» и помечает изменённые позиции.
    func apply(parse response: ParseResponse) {
        let wasCorrection = didRecognize || hasDraftItems
        let oldItems = draftItems
        let oldSnapshot = (items: draftItems, description: descriptionText,
                           sum: sumText, payer: payerId)

        let draft = response.draft
        if !draft.description.isEmpty {
            descriptionText = draft.description
        }
        if draft.sum >= 1 {
            sumText = String(draft.sum)
        }
        if let donorId = draft.donorId, members.contains(where: { $0.id == donorId }) {
            payerId = donorId
        }
        // Голосовая правка БЕЗ позиций в ответе («платил Саша») не должна стирать
        // уже показанный чек: модель отвечает только тем, что расслышала, а
        // пустой items здесь означает «про позиции ничего не сказано», а не
        // «позиций нет». Для первичного распознавания перезаписываем как раньше.
        if wasCorrection, (draft.items ?? []).isEmpty {
            // оставляем текущие draftItems
        } else {
            draftItems = draft.items
        }
        parseQuestions = response.questionList
        // распознали что-то полезное — помечаем форму как «из AI»
        // Плательщик — тоже распознанное: правка «платил Саша» меняет форму, и
        // говорить на неё «не удалось распознать» (да ещё и снимать undo) неверно.
        let recognizedDonor = draft.donorId.map { id in members.contains { $0.id == id } } ?? false
        let recognizedSomething = !draft.description.isEmpty || draft.sum >= 1
            || !(draft.items ?? []).isEmpty || recognizedDonor
        if recognizedSomething {
            didRecognize = true
        } else if parseQuestions.isEmpty {
            // совсем пусто и без вопросов — говорим явно, а не молча возвращаем форму
            alertMessage = "Не удалось распознать. Скажите ещё раз — с блюдами и ценами"
        }
        // Голосовая правка непустой формы: снапшот для отмены + подсветка диффа.
        if wasCorrection, recognizedSomething {
            undoSnapshot = oldSnapshot
            canUndoParse = true
            changedItemIndices = Self.changedIndices(old: oldItems ?? [], new: draft.items ?? [])
        } else {
            changedItemIndices = []
            canUndoParse = false
        }
        syncRecipientsFromItems()
    }

    /// Поколение parse-запроса: новый запрос ОБГОНЯЕТ старый (например, во время
    /// распознавания голоса пользователь добавил фото чека — уходит голос+фото,
    /// а ответ первого запроса игнорируется).
    private var parseGeneration = 0
    /// Активная задача распознавания: держим ссылку, чтобы «Отмена» на оверлее
    /// РЕАЛЬНО рвала запрос (multipart до ~2.8 МБ WAV + JPEG и оплаченный вызов
    /// модели), а не только обесценивала ответ поколением.
    private var parseTask: Task<Void, Never>?

    /// Запускает распознавание отдельной задачей (её и отменяет `cancelParse`).
    /// `completion` вызывается на главном актёре после применения ответа —
    /// вью гасит в нём запись и фокус.
    func startParse(
        api: APIClient,
        audio: Data? = nil,
        image: Data? = nil,
        text: String? = nil,
        completion: (() -> Void)? = nil
    ) {
        parseTask?.cancel()
        parseTask = Task { [weak self] in
            await self?.parse(api: api, audio: audio, image: image, text: text)
            guard !Task.isCancelled else { return }
            completion?()
        }
    }

    /// AI-распознавание/голосовая правка: шлёт медиа + текущий черновик на `/parse`,
    /// применяет ответ. Ошибка (сеть/сервер) НЕ теряет черновик — форма остаётся
    /// как была, показывается алерт. Повторный вызов при активном запросе НЕ
    /// блокируется, а обгоняет его (см. `parseGeneration`).
    func parse(api: APIClient, audio: Data? = nil, image: Data? = nil, text: String? = nil) async {
        guard let roomId = selectedRoomId else {
            alertMessage = "Выберите группу"
            return
        }
        parseGeneration += 1
        let generation = parseGeneration
        isParsing = true
        defer {
            if generation == parseGeneration { isParsing = false }
        }

        // Текущий черновик передаётся для голосовой правки: сервер применяет
        // только дельту, не пересобирая уже проставленные доли/имена. Пустую
        // форму отправляем без черновика (распознавание с нуля).
        let currentDraft: ParseDraft? = (hasDraftItems || !descriptionText.isEmpty || (sum ?? 0) > 0)
            ? ParseDraft(description: descriptionText, sum: sum ?? 0, donorId: payerId, items: draftItems)
            : nil
        do {
            let response = try await api.parseOperation(
                roomId: roomId,
                audio: audio,
                image: image,
                text: text,
                draft: currentDraft
            )
            // Устаревший ответ (нас обогнал более полный запрос) — выбрасываем.
            guard generation == parseGeneration else { return }
            apply(parse: response)
            if didRecognize {
                Haptics.success()
            }
        } catch {
            if error.isTaskCancellation { return }
            guard generation == parseGeneration else { return }
            // Отдельный канал ошибки парсинга: у вью есть lastAudio, и она
            // предлагает «Повторить» — диктовка НЕ теряется из-за моргнувшей сети.
            parseRetryMessage = humanErrorText(error)
        }
    }

    /// Отмена активного распознавания (кнопка на parsing-оверлее): текущий
    /// запрос обесценивается поколением, форма остаётся как была.
    func cancelParse() {
        parseGeneration += 1
        parseTask?.cancel()
        parseTask = nil
        isParsing = false
    }

    /// «Что осталось уточнить» для экрана диктовки: нераспознанные имена,
    /// позиции без цены и вопросы модели (не дублирующие первые два).
    /// Показывается в оверлее записи при голосовой правке — видно, что сказать.
    var missingInfoHints: [String] {
        var hints: [String] = []
        var covered: [String] = []
        for item in draftItemList {
            for name in item.unknown ?? [] {
                // Безличная форма: «кто такой Маша?» звучала бы криво.
                hints.append("Кто это — «\(name)»?")
                covered.append(name.lowercased())
            }
        }
        for item in draftItemList where !item.isSurcharge && item.price < 1 {
            let name = item.name.isEmpty ? "позиция" : item.name
            hints.append("Сколько стоит «\(name)»?")
            covered.append(name.lowercased())
        }
        for question in parseQuestions {
            let lower = question.lowercased()
            if covered.contains(where: { lower.contains($0) }) { continue }
            hints.append(question)
        }
        return Array(hints.prefix(3))
    }

    /// Индексы позиций, отличающихся от прежней версии черновика: изменённые
    /// по месту и добавленные в конец. Удаления не подсвечиваются (строки нет).
    nonisolated static func changedIndices(old: [OperationItem], new: [OperationItem]) -> Set<Int> {
        var out: Set<Int> = []
        for (index, item) in new.enumerated() {
            if index >= old.count || old[index] != item {
                out.insert(index)
            }
        }
        return out
    }

    /// Откат последней голосовой правки к снапшоту формы.
    func undoParse() {
        guard let snapshot = undoSnapshot else { return }
        draftItems = snapshot.items
        descriptionText = snapshot.description
        sumText = snapshot.sum
        payerId = snapshot.payer
        undoSnapshot = nil
        canUndoParse = false
        changedItemIndices = []
        parseQuestions = []
        syncRecipientsFromItems()
    }

    /// Гасит подсветку изменённых позиций (по таймеру из вью).
    func clearChangeHighlights() {
        changedItemIndices = []
    }

    /// Принять правку («Ок»/таймаут карточки): прячет карточку отмены,
    /// снапшот выбрасывается.
    func dismissUndo() {
        canUndoParse = false
        undoSnapshot = nil
    }

    /// Сброс распознанного чека (чип «Поровну на всех» или ручная правка суммы/долей):
    /// позиции больше не источник правды, операция сохранится плоской (равное деление).
    func resetItems() {
        guard hasDraftItems else { return }
        draftItems = nil
        parseQuestions = []
        splitType = .equally
        if recipientIds.isEmpty {
            recipientIds = Set(members.map(\.id))
        }
    }

    /// Заменяет позицию по индексу (правка из шита позиции).
    func replaceItem(at index: Int, with item: OperationItem) {
        guard var items = draftItems, items.indices.contains(index) else { return }
        items[index] = item
        draftItems = items
        syncRecipientsFromItems()
    }

    /// Индекс позиции по её клиентскому id; nil — строки уже нет в черновике.
    /// Шит правки открыт долго, и голосовая правка за это время может
    /// переставить/заменить позиции: адресация по индексу писала бы в ЧУЖУЮ строку.
    func indexOfItem(id: UUID) -> Int? {
        draftItems?.firstIndex { $0.id == id }
    }

    /// Заменяет позицию по её клиентскому id (правка из шита позиции).
    func replaceItem(id: UUID, with item: OperationItem) {
        guard let index = indexOfItem(id: id) else { return }
        replaceItem(at: index, with: item)
    }

    /// Удаляет позицию по её клиентскому id (удаление из шита позиции).
    func deleteItem(id: UUID) {
        guard let index = indexOfItem(id: id) else { return }
        deleteItem(at: index)
    }

    /// Сопоставляет нераспознанное имя `name` в позиции участнику `userId`: имя
    /// убирается из `unknown`, участник добавляется в доли позиции (вес 1), а `alias`
    /// дозаписывается на сервере (best-effort), чтобы следующее распознавание сматчило само.
    func resolveUnknown(itemIndex: Int, name: String, to userId: Int, api: APIClient) {
        guard var items = draftItems, items.indices.contains(itemIndex) else { return }
        let item = items[itemIndex]
        var unknown = item.unknown ?? []
        unknown.removeAll { $0.caseInsensitiveCompare(name) == .orderedSame }
        var shares = item.shareList
        if !item.isSurcharge, !shares.contains(where: { $0.userId == userId }) {
            shares.append(ItemShare(userId: userId, weight: 1))
        }
        items[itemIndex] = OperationItem(
            name: item.name,
            price: item.price,
            qty: item.qty,
            shares: item.isSurcharge ? nil : shares,
            kind: item.kind,
            split: item.split,
            percent: item.percent,
            unknown: unknown.isEmpty ? nil : unknown
        )
        draftItems = items
        syncRecipientsFromItems()
        if let member = members.first(where: { $0.id == userId }) {
            toastMessage = "«\(name)» — это \(member.displayName). Запомнил, больше не спрошу"
        }
        // Дозапись алиаса — best-effort: ошибка (сеть/доступ) не критична для формы.
        Task { try? await api.addAlias(userId: userId, alias: name) }
    }

    /// Синхронизирует множество получателей с участниками позиций — чтобы
    /// плоские валидации/сохранение видели тех же людей, что и чек.
    private func syncRecipientsFromItems() {
        guard hasDraftItems else { return }
        let ids = itemizedUserIds
        guard !ids.isEmpty else { return }
        recipientIds = Set(ids)
    }

    /// Производные по позициям `recipientSums` в стабильном порядке `ids`
    /// (недостающие из позиций добавляются следом); nil — позиций нет или они
    /// невалидны. Сервер плоские поля игнорирует, но `OperationBody` их несёт.
    private func itemizedRecipientSums(orderedFrom ids: [Int]) -> [RecipientSum]? {
        guard hasDraftItems, let shares = itemizedShares else { return nil }
        let itemIds = itemizedUserIds
        let ordered = ids.filter { itemIds.contains($0) } + itemIds.filter { !ids.contains($0) }
        return ordered.compactMap { id in
            guard let sum = shares[id], sum >= 1 else { return nil }
            return RecipientSum(userId: id, sum: sum)
        }
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
            // Позиции чека неотправленной записи переносим так же, как у
            // синхронизированной операции: без этого правка описания у офлайн
            // itemized-расхода уходила в outbox с items=nil и чек терялся
            // безвозвратно — при синке создавался плоский расход.
            draftItems = payload.items
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
            // Отмена .task (закрыли sheet) — не ошибка, но состояние обязано
            // остаться ПОВТОРЯЕМЫМ: с `.loading` и isConfigured=true экран
            // навсегда застревал на спиннере — .task больше не сработает,
            // а кнопки «Повторить» в этом состоянии нет.
            if error.isTaskCancellation {
                isConfigured = false
                state = .failed("Загрузка прервана. Попробуйте ещё раз")
                return
            }
            state = .failed(humanErrorText(error))
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
        // Позиции чека принадлежат участникам ПРЕЖНЕЙ группы: без сброса
        // сохранение шло по itemized-ветке с чужими userId — 400 от сервера,
        // а при пересечении id деньги списывались не с тех людей.
        draftItems = nil
        undoSnapshot = nil
        canUndoParse = false
        changedItemIndices = []
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
        guard !isSaving, !didSave else { return false }
        guard let roomId = selectedRoomId else {
            alertMessage = "Выберите группу"
            return false
        }
        let description = descriptionText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty else {
            alertMessage = "Введите описание расхода"
            return false
        }
        // Сумма itemized-черновика — ПРОИЗВОДНАЯ от позиций, а не поле формы:
        // плоский `sumText` пишется только при разборе и отстаёт от правок
        // строк (правка 600→900 оставляла в теле запроса старый итог), а у
        // ответа модели без верхнеуровневой суммы его вообще нет — при живой
        // кнопке «Сохранить» пользователь получал «Введите сумму» без поля суммы.
        guard let sum = hasDraftItems ? itemizedTotal : sum, sum >= 1 else {
            alertMessage = hasDraftItems
                ? "Проверьте позиции чека — итог не считается"
                : "Введите сумму (целое число рублей, не меньше 1)"
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
        // itemized-черновик: позиции — источник правды, сервер сам выводит суммы
        // и игнорирует плоские поля, но `OperationBody` обязано нести валидный
        // способ деления — отправляем производные `by_exact_amount` из позиций.
        let itemsToSend: [OperationItem]?
        if let itemSums = itemizedRecipientSums(orderedFrom: ids) {
            guard !hasUnknownItems else {
                alertMessage = "Сначала выберите, кто такой \(firstUnknownName ?? "…")"
                return false
            }
            itemsToSend = draftItems
            split = .byExactAmount(recipientSums: itemSums)
            exactSums = itemSums
        } else if splitType == .byExactAmount {
            guard isDistributionBalanced else {
                alertMessage = "Суммы участников должны сходиться с суммой расхода"
                return false
            }
            let sums = exactRecipientSums(orderedIds: ids)
            split = .byExactAmount(recipientSums: sums)
            exactSums = sums
            itemsToSend = nil
        } else {
            split = .equally(recipientIds: ids)
            exactSums = nil
            itemsToSend = nil
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
                    recipientSums: exactSums,
                    items: itemsToSend
                )
            )
            didSave = true
            return true
        }

        let payload = OutboxPayload(
            description: description,
            sum: sum,
            donorId: payerId,
            recipientIds: exactSums == nil ? ids : nil,
            recipientSums: exactSums,
            items: itemsToSend
        )
        // localId создаётся заранее и служит clientOpId прямого POST:
        // если ответ потеряется, досылка из outbox не создаст дубль.
        let localId = UUID()

        // Офлайн-создание: в outbox, отправится при появлении сети.
        if editOperationId == nil, !isOnline {
            outbox.add(roomId: roomId, payload: payload, localId: localId, ownerUserId: meId)
            didSave = true
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
                    split: split,
                    items: itemsToSend
                )
            } else {
                _ = try await api.addOperation(
                    roomId: roomId,
                    description: description,
                    sum: sum,
                    donorId: payerId,
                    split: split,
                    items: itemsToSend,
                    clientOpId: localId.uuidString
                )
            }
            didSave = true
            return true
        } catch let error as APIError {
            // Сервер недоступен при живой сети (обслуживание, упал бэкенд):
            // создание не теряем — кладём в outbox с ТЕМ ЖЕ localId (идемпотентно).
            if editOperationId == nil, case .transport = error {
                outbox.add(roomId: roomId, payload: payload, localId: localId, ownerUserId: meId)
                didSave = true
                return true
            }
            alertMessage = humanErrorText(error)
            return false
        } catch {
            alertMessage = humanErrorText(error)
            return false
        }
    }
}
