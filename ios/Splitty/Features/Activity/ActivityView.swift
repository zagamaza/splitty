import SwiftUI

/// Действия на карточке приглашения.
enum InviteAction {
    case accept, decline, leave
}

/// Спрашивать ли подтверждение перед действием.
///
/// Выход необратим — вернуться можно только по новому приглашению участника, —
/// а кнопка стоит вплотную к «Открыть»: один промах, и человек вне группы.
/// Принять и отклонить приглашение и стоят дешевле, и отменяются сами собой.
///
/// Вынесено из вью, чтобы правило проверялось тестом: у Android оно живёт
/// отдельной функцией `inviteActionNeedsConfirm` ровно по той же причине.
func inviteActionNeedsConfirm(_ action: InviteAction) -> Bool {
    switch action {
    case .leave: return true
    case .accept, .decline: return false
    }
}

/// Вкладка «Уведомления»: карточки приглашений и лента операций всех групп
/// с пагинацией. Заголовок экрана обязан совпадать с подписью таба (и с
/// Android, где заголовок берётся из той же строки tab_activity): раздел
/// один, а имён у него было два.
///
/// Лента показывается ровно такой, какой её отдал сервер: раздел стал
/// входящими, счётчик непрочитанного считает адресованное вам
/// (`notifiesUser`), и тумблер «Только мои» — переключавший ленту между
/// «мне» и «всё подряд» — противоречил этому, да ещё и без подписи.
struct ActivityView: View {
    /// Карточка, выход из которой ждёт подтверждения; nil — диалога нет.
    @State private var leaveConfirmCard: InviteCard?
    @Environment(SessionStore.self) private var session
    @State private var model = ActivityViewModel()
    /// Задача перезагрузки по dataVersion (отменяем прежнюю — см. GroupDetailView).
    @State private var reloadTask: Task<Void, Never>?

    init() {}

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle("Уведомления")
                // .task на контенте (не на NavigationStack): срабатывает и при
                // возврате (pop) с экрана группы — лента обновляется.
                .task {
                    await model.load(repo: session.repo)
                    // Раздел открыт — значит человек всё это увидел.
                    await model.markSeen(session: session)
                }
                // Единая инвалидация: перезагрузка после любой мутации данных.
                .onChange(of: session.dataVersion) {
                    reloadTask?.cancel()
                    reloadTask = Task { await model.load(repo: session.repo) }
                }
                .onDisappear { reloadTask?.cancel() }
                .alert(
                    "Ошибка",
                    isPresented: Binding(
                        get: { model.errorMessage != nil },
                        set: { if !$0 { model.errorMessage = nil } }
                    )
                ) {
                    Button("Ок", role: .cancel) {}
                } message: {
                    Text(model.errorMessage ?? "")
                }
                // Тот же текст, что в настройках группы: правило одно, и
                // человек не должен узнавать его в двух разных формулировках
                .confirmationDialog(
                    leaveConfirmTitle,
                    isPresented: Binding(
                        get: { leaveConfirmCard != nil },
                        set: { if !$0 { leaveConfirmCard = nil } }
                    ),
                    titleVisibility: .visible
                ) {
                    Button("Выйти", role: .destructive) {
                        guard let card = leaveConfirmCard else { return }
                        leaveConfirmCard = nil
                        Task { await model.leaveFromCard(card, session: session) }
                    }
                    Button("Отмена", role: .cancel) { leaveConfirmCard = nil }
                } message: {
                    Text("Группа исчезнет из вашего списка. Вернуться можно только по приглашению участника")
                }
        }
    }

    /// Заголовок подтверждения выхода с карточки.
    private var leaveConfirmTitle: String {
        guard let card = leaveConfirmCard else { return "" }
        return "Выйти из «\(card.roomName)»?"
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            ProgressView()
        case .failed(let message):
            failedView(message)
        case .loaded:
            feed
        }
    }

    /// Ошибка первичной загрузки: текст + кнопка «Повторить».
    private func failedView(_ message: String) -> some View {
        ContentUnavailableView {
            Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
        } description: {
            Text(message)
        } actions: {
            Button("Повторить") {
                Task { await model.load(repo: session.repo) }
            }
            .buttonStyle(.borderedProminent)
            .tint(Color.accent)
        }
    }

    /// Лента карточных строк на Color.bg; ленивая подгрузка страниц сохранена
    /// (.task на строке — LazyVStack создаёт строки по мере прокрутки).
    private var feed: some View {
        ScrollView {
            LazyVStack(spacing: 12) {
                // Лента не имеет сводки, к которой крепится подпись, — поэтому
                // отдельной строкой сверху: старые события молча выглядят как
                // «ничего нового»
                CacheNote(isFromCache: model.isFromCache, updatedAt: model.lastUpdatedAt)
                    .frame(maxWidth: .infinity, alignment: .leading)
                ForEach(model.invites) { card in
                    InviteCardView(card: card) { action in
                        if inviteActionNeedsConfirm(action) {
                            leaveConfirmCard = card
                            return
                        }
                        Task {
                            switch action {
                            case .accept: await model.acceptInvite(card, session: session)
                            case .decline: await model.declineInvite(card, session: session)
                            case .leave: break
                            }
                        }
                    }
                }
                ForEach(model.items) { item in
                    NavigationLink {
                        GroupDetailView(roomId: item.roomId)
                    } label: {
                        ActivityRow(item: item, myUserId: session.me?.id)
                    }
                    .buttonStyle(.plain)
                    .task {
                        await model.loadMoreIfNeeded(repo: session.repo, current: item)
                    }
                }
                if model.isLoadingMore {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 12)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
        }
        .refreshable {
            await model.refresh(repo: session.repo)
        }
        // Запас под центральную кнопку «+»: на устройствах без home indicator
        // она выступает над таб-баром и перекрывала бы последнюю строку.
        .contentMargins(.bottom, 40, for: .scrollContent)
        .overlay {
            if activityFeedIsEmpty(items: model.items, invites: model.invites) {
                ContentUnavailableView {
                    Label("Здесь появятся события ваших групп", systemImage: "clock")
                } description: {
                    Text("Новые расходы, погашения и приглашения — всё, что делают участники")
                }
            }
        }
    }
}

/// Карточная строка ленты: градиентный аватар автора, текст события
/// с жирными именами, ваша позиция с MoneyText и относительное время.
private struct ActivityRow: View {
    let item: ActivityItem
    let myUserId: Int?

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            UserAvatarView(user: item.operation.donor, size: 44)
            VStack(alignment: .leading, spacing: 6) {
                title
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.ink)
                position
                Text(DateFmt.relative(item.operation.createdAt))
                    .scaledFont(size: 12, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 0)
            // Аффорданс перехода: карточка кликабельна (ведёт в группу),
            // без шеврона это не считывалось.
            Image(systemName: "chevron.right")
                .scaledFont(size: 12, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
                .padding(.top, 4)
        }
        .surfaceCard()
        // VoiceOver читает карточку одним элементом, а не по кускам.
        .accessibilityElement(children: .combine)
    }

    /// «Загир добавил(а) «Ужин» в группе «Стамбул»» /
    /// «Загир заплатил(а) Алмазу 500 ₽ в группе «Стамбул»».
    private var title: Text {
        let op = item.operation
        let donor = Text(op.donor.displayName).fontWeight(.semibold)
        if op.isDebtRepayment {
            // «заплатил(а) вам» — когда кредитор текущий пользователь; иначе имя без склонения.
            let sum = Text(money(op.sum, currency: item.roomCurrency))
                .fontWeight(.semibold)
                .monospacedDigit()
            if let lender = op.recipients.first?.user, lender.id == myUserId {
                return donor
                    + Text(" заплатил(а) вам ")
                    + sum
                    + Text(" в группе «\(item.roomName)»")
            }
            let lenderName = op.recipients.first?.user.displayName ?? String(localized: "участнику")
            return donor
                + Text(" заплатил(а) ")
                + Text(lenderName).fontWeight(.semibold)
                + Text(verbatim: " ")
                + sum
                + Text(" в группе «\(item.roomName)»")
        }
        return donor + Text(" добавил(а) «\(op.description)» в группе «\(item.roomName)»")
    }

    /// Вторая строка: ваша позиция — подпись вторичным цветом и сумма
    /// через MoneyText (семантическая окраска, numericText-переход).
    @ViewBuilder
    private var position: some View {
        let info = positionInfo
        HStack(spacing: 5) {
            Text(info.label)
                .scaledFont(size: 14, weight: .medium)
                .foregroundStyle(Color.inkSecondary)
            if let amount = info.amount {
                MoneyText(amount, role: info.role, size: 15, currency: item.roomCurrency)
            }
        }
    }

    /// Подпись позиции, сумма (nil — только серый текст без суммы) и её роль.
    private var positionInfo: (label: String, amount: Int?, role: MoneyText.Role) {
        let op = item.operation
        guard let myUserId else {
            return (String(localized: "Вы не участвуете"), nil, .neutral)
        }

        if op.isDebtRepayment {
            if op.donor.id == myUserId {
                return (String(localized: "Вы заплатили"), op.sum, .negative)
            }
            if op.recipients.contains(where: { $0.user.id == myUserId }) {
                return (String(localized: "Вы получили"), op.sum, .positive)
            }
            return (String(localized: "Вы не участвуете"), nil, .neutral)
        }

        // Расход: позиция — из ХРАНИМЫХ долей операции
        // (Operation.netPosition; при неравном делении доли не пересчитываются).
        guard let net = op.netPosition(of: myUserId) else {
            return (String(localized: "Вы не участвуете"), nil, .neutral)
        }
        if net > 0 {
            return (String(localized: "Вы одолжили"), net, .positive)
        }
        if net < 0 {
            return (String(localized: "Вы должны"), -net, .negative)
        }
        return (Glossary.settled, nil, .neutral)
    }
}

#Preview {
    ActivityView()
        .environment(SessionStore())
}


// MARK: - Карточка приглашения

/// Пусто ли на экране. Карточки приглашений — тоже содержимое: типовой первый
/// экран новичка это одно приглашение и ни одной операции, и оверлей «Пока нет
/// активности» ложился бы поверх карточек, пряча кнопки «Принять»/«Отклонить» —
/// то есть весь смысл раздела. Паритет с Android (ActivityScreen.kt).
func activityFeedIsEmpty(items: [ActivityItem], invites: [InviteCard]) -> Bool {
    items.isEmpty && invites.isEmpty
}

/// Заголовок карточки приглашения.
///
/// Плейсхолдер занимает слот ПОДЛЕЖАЩЕГО: сервер оставляет `inviterName` пустым,
/// если строку пригласившего прочитать не удалось. Прежнее «Вас» давало «Вас
/// добавил вас в группу».
func inviteCardTitle(_ card: InviteCard) -> String {
    let who = card.inviterName.isEmpty ? String(localized: "Кто-то") : card.inviterName
    switch card.status {
    case .pending:
        return String(localized: "\(who) приглашает вас вернуться в «\(card.roomName)»")
    default:
        return String(localized: "\(who) добавил вас в группу «\(card.roomName)»")
    }
}

/// Закреплённая карточка над лентой. Два вида:
/// `added` — «вас добавили», кнопки «Открыть» и **«Выйти»**;
/// `pending` — «приглашает вернуться», кнопки «Принять» и «Отклонить».
///
/// «Выйти» на карточке `added` обязательна: человека добавили, не спросив, и
/// без неё отказаться можно было бы только разыскав настройки группы.
private struct InviteCardView: View {
    let card: InviteCard
    let onAction: (InviteAction) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: "person.2.badge.plus.fill")
                    .font(.system(size: 20))
                    .foregroundStyle(Color.accent)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title)
                        .scaledFont(size: 15)
                        .foregroundStyle(Color.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(DateFmt.relative(card.createdAt))
                        .scaledFont(size: 12, relativeTo: .footnote)
                        .foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 0)
            }
            buttons
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Color.accent.opacity(0.5), lineWidth: 1.5)
        }
    }

    private var title: String { inviteCardTitle(card) }

    @ViewBuilder
    private var buttons: some View {
        HStack(spacing: 8) {
            switch card.status {
            case .pending:
                Button("Принять") { onAction(.accept) }
                    .buttonStyle(.softChip(isSelected: true))
                Button("Отклонить") { onAction(.decline) }
                    .buttonStyle(.softChip)
            default:
                NavigationLink("Открыть") {
                    GroupDetailView(roomId: card.roomId)
                }
                .buttonStyle(.softChip(isSelected: true))
                // Отступ отделяет необратимое действие от обычного: рядом
                // стоящие кнопки ловили промах, и человек выходил из группы
                Spacer(minLength: 24)
                Button("Выйти") { onAction(.leave) }
                    .buttonStyle(.softChip)
            }
        }
    }
}
