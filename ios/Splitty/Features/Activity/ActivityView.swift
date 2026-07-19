import SwiftUI

/// Вкладка «Активность»: лента карточных строк операций всех групп с пагинацией.
struct ActivityView: View {
    @Environment(SessionStore.self) private var session
    @State private var model = ActivityViewModel()

    init() {}

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle("Активность")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            Haptics.tap()
                            model.isMineOnly.toggle()
                            Task { await model.fillFilteredIfNeeded(repo: session.repo, meId: session.me?.id) }
                        } label: {
                            Image(systemName: model.isMineOnly
                                ? "person.crop.circle.fill"
                                : "person.crop.circle")
                        }
                        // Активный фильтр — акцентный, выключенный — тихий:
                        // иначе состояние тумблера читалось только по заливке иконки.
                        .tint(model.isMineOnly ? Color.accent : Color.inkSecondary)
                        .accessibilityLabel("Только мои")
                        .accessibilityValue(model.isMineOnly ? "включено" : "выключено")
                    }
                }
                // .task на контенте (не на NavigationStack): срабатывает и при
                // возврате (pop) с экрана группы — лента обновляется.
                .task {
                    await model.load(repo: session.repo)
                }
                // Единая инвалидация: перезагрузка после любой мутации данных.
                .onChange(of: session.dataVersion) {
                    Task { await model.load(repo: session.repo) }
                }
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
        }
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
        let displayItems = model.displayItems(meId: session.me?.id)
        return ScrollView {
            LazyVStack(spacing: 12) {
                ForEach(displayItems) { item in
                    NavigationLink {
                        GroupDetailView(roomId: item.roomId)
                    } label: {
                        ActivityRow(item: item, myUserId: session.me?.id)
                    }
                    .buttonStyle(.plain)
                    .task {
                        await model.loadMoreIfNeeded(repo: session.repo, current: item, meId: session.me?.id)
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
            if displayItems.isEmpty {
                ContentUnavailableView {
                    Label(
                        model.isMineOnly ? "Нет операций с вами" : "Пока нет активности",
                        systemImage: "clock"
                    )
                } description: {
                    Text(
                        model.isMineOnly
                            ? "Операции, где вы платили или участвовали, появятся здесь"
                            : "Здесь появятся расходы и платежи ваших групп"
                    )
                } actions: {
                    // Пустота может быть следствием фильтра — даём выход
                    // одним тапом вместо поиска тумблера в тулбаре.
                    if model.isMineOnly {
                        Button("Показать все") {
                            model.isMineOnly = false
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(Color.accent)
                    }
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
            let lenderName = op.recipients.first?.user.displayName ?? "участнику"
            return donor
                + Text(" заплатил(а) ")
                + Text(lenderName).fontWeight(.semibold)
                + Text(" ")
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
            return ("Вы не участвуете", nil, .neutral)
        }

        if op.isDebtRepayment {
            if op.donor.id == myUserId {
                return ("Вы заплатили", op.sum, .negative)
            }
            if op.recipients.contains(where: { $0.user.id == myUserId }) {
                return ("Вы получили", op.sum, .positive)
            }
            return ("Вы не участвуете", nil, .neutral)
        }

        // Расход: позиция — из ХРАНИМЫХ долей операции
        // (Operation.netPosition; при неравном делении доли не пересчитываются).
        guard let net = op.netPosition(of: myUserId) else {
            return ("Вы не участвуете", nil, .neutral)
        }
        if net > 0 {
            return ("Вы одолжили", net, .positive)
        }
        if net < 0 {
            return ("Вы должны", -net, .negative)
        }
        return (Glossary.settled, nil, .neutral)
    }
}

#Preview {
    ActivityView()
        .environment(SessionStore())
}
