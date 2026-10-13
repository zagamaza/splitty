import Combine
import SwiftUI

/// Экран с собственным нижним баром (туса) просит скрыть глобальную кнопку «+»:
/// .toolbar(.hidden, for: .tabBar) прячет только таб-бар, а overlay-кнопка
/// TabView иначе остаётся висеть поверх бара тусы.
struct HidesGlobalAddButtonKey: PreferenceKey {
    static var defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = value || nextValue()
    }
}

/// Главный экран: четыре вкладки и строка чека внизу (`ReceiptTabBar`) с
/// действием «+», открывающим AddExpenseView (с выбором группы внутри).
/// Здесь же — глобальный офлайн-баннер (тонкая полоса сверху) и триггеры
/// синка outbox: сеть появилась / приложение стало активным.
struct MainTabView: View {
    /// Не private: по вкладке проверяется маршрут push (`pushTarget`).
    enum Tab: Hashable {
        case friends, groups, activity, account

        /// Вкладка, на которой открывается приложение. Новый аккаунт попадал на
        /// «Друзей», где по определению пусто, и первым читал отрицание.
        static let initial: Tab = .groups
    }

    @Environment(SessionStore.self) private var session
    @Environment(\.scenePhase) private var scenePhase
    /// Стартовая вкладка — «Группы»: новый аккаунт открывался на «Друзьях»,
    /// где по определению пусто, и первое, что он читал, было отрицание.
    /// Диплинки на приглашение и на операцию перебивают это ниже по коду.
    @State private var selection: Tab = Tab.initial
    /// Путь стека вкладки «Группы» — здесь, а не внутри списка: тап по push
    /// про расход обязан открыть комнату поверх этой вкладки.
    @State private var groupsPath: [GroupsRoute] = []
    @State private var isAddExpensePresented = false
    /// Кнопка «+» скрывается при видимой клавиатуре: overlay поднимается
    /// вместе с safe area и иначе висит поверх контента над клавиатурой.
    @State private var isKeyboardVisible = false
    /// true — открыт экран с собственным нижним баром (туса): глобальный «+» скрыт.
    @State private var isGlobalAddHidden = false

    var body: some View {
        tabs
            // Одно место на всё приложение: подтверждение действия («Погашение
            // записано») вместо пяти разных плашек или, как было, молчания
            .overlay(alignment: .bottom) {
                if let toast = session.successToast {
                    SuccessToast(text: toast)
                        .padding(.bottom, 92)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                        .task(id: toast) {
                            try? await Task.sleep(for: .seconds(2.5))
                            session.dismissToast()
                        }
                        .onTapGesture { session.dismissToast() }
                }
            }
            .animation(.snappy(duration: 0.22), value: session.successToast)
    }

    private var tabs: some View {
        // Системный таббар iOS 26 — стеклянная капсула с иконками, и в чек она
        // не вписывается никак; его место занимает строка чека ReceiptTabBar.
        TabView(selection: $selection) {
            FriendsListView()
                .toolbar(.hidden, for: .tabBar)
                .tag(Tab.friends)

            GroupsListView(path: $groupsPath)
                .toolbar(.hidden, for: .tabBar)
                .tag(Tab.groups)

            ActivityView()
                .toolbar(.hidden, for: .tabBar)
                .tag(Tab.activity)

            AccountView()
                .toolbar(.hidden, for: .tabBar)
                .tag(Tab.account)
        }
        .tint(Color.ink)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if !isKeyboardVisible && !isGlobalAddHidden {
                ReceiptTabBar(
                    selection: $selection,
                    badge: Self.badgeLabel(for: session.unreadNotifications),
                    onAdd: {
                        Haptics.tap()
                        isAddExpensePresented = true
                    }
                )
            }
        }
        .onPreferenceChange(HidesGlobalAddButtonKey.self) { isGlobalAddHidden = $0 }
        // Глобальный тонкий баннер офлайна/отправки outbox: полоса под
        // статус-баром над навбарами всех вкладок; онлайн без синка — скрыт.
        .safeAreaInset(edge: .top, spacing: 0) {
            statusBanner
        }
        // Полноэкранно, а не sheet: форму со введёнными данными нельзя
        // случайно смахнуть — выход только «Отмена»/«Сохранить».
        .fullScreenCover(isPresented: $isAddExpensePresented) {
            // Списки под формой обновятся через session.dataVersion
            // (AddExpenseView делает noteDataChanged() после сохранения).
            AddExpenseView()
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            isKeyboardVisible = true
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            isKeyboardVisible = false
        }
        // Триггеры синка outbox: сеть снова появилась / приложение стало
        // активным / первый показ (плюс pull-to-refresh на экранах групп).
        .onChange(of: session.isOnline) { _, isOnline in
            if isOnline {
                Task { await session.syncOutbox() }
            }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active {
                // Пока приложение было в фоне, в группах могли появиться
                // расходы и погашения: обновляем не только бейдж, но и сами
                // экраны — иначе человек возвращался к устаревшим суммам
                session.noteDataChanged()
                Analytics.shared.track(.appOpen(cold: false))
                Task {
                    await session.syncOutbox()
                    await session.refreshUnreadCount()
                }
            }
            if phase == .background {
                // Уход в фон — последний момент, когда можно отправить
                // накопленное: дальше система приложение усыпит.
                Analytics.shared.flushInBackground()
            }
        }
        // Тап по push: единственный подписчик, который реально куда-то ведёт.
        // Без него событие постилось в пустоту, и переход по уведомлению был
        // фикцией (SplittyApp лишь перечитывает счётчик).
        .onReceive(NotificationCenter.default.publisher(for: .splittyPushTapped)) { notification in
            guard let route = notification.userInfo?[PushRoute.userInfoKey] as? PushRoute else { return }
            let target = Self.pushTarget(for: route)
            selection = target.tab
            // Присваивание, а не append: повторный тап по тому же уведомлению
            // задаёт тот же путь и ничего не наслаивает.
            if !target.path.isEmpty {
                groupsPath = target.path
            }
        }
        .task { await session.syncOutbox() }
    }

    /// Текст бейджа непрочитанного: nil — бейджа нет.
    ///
    /// Сервер отдаёт точное число до 99, а `99 + 1` означает «больше 99»
    /// (см. `maxUnreadCount` в internal/rest/notifications_feed.go). Рисовать
    /// такое значение числом нельзя: «100» выглядело бы точным количеством,
    /// которого никто не считал.
    static func badgeLabel(for count: Int) -> String? {
        switch count {
        case ...0: return nil
        case ...99: return String(count)
        default: return "99+"
        }
    }

    /// Куда ведёт тап по push: вкладка и путь стека вкладки «Группы».
    /// Отдельной функцией — внутри `body` этот выбор не проверить ничем.
    ///
    /// Карточка операции ложится ПОВЕРХ комнаты, а не вместо неё: «назад» с
    /// карточки обязано вести в группу — иначе удалённая операция запирает
    /// человека на «не найдено» прямо над корнем вкладки.
    static func pushTarget(for route: PushRoute) -> (tab: Tab, path: [GroupsRoute]) {
        switch route {
        case .room(let id):
            return (.groups, [.room(id: id)])
        case .operation(let roomId, let operationId):
            return (.groups, [.room(id: roomId), .operation(roomId: roomId, operationId: operationId)])
        case .notifications:
            // Приглашение ведёт в раздел, а не в комнату: доступа к ней у
            // приглашённого ещё нет.
            return (.activity, [])
        }
    }

    // MARK: Офлайн-баннер

    /// «Офлайн — изменения сохраняются локально» без сети;
    /// кратко «Отправка…» пока уходит outbox. VStack-обёртка постоянна,
    /// чтобы transition появления/скрытия баннера анимировался.
    private var statusBanner: some View {
        VStack(spacing: 0) {
            if !session.isOnline {
                bannerRow(icon: "wifi.slash", text: "Офлайн — изменения сохраняются локально")
                    .transition(.move(edge: .top).combined(with: .opacity))
            } else if session.outbox.isSyncing {
                bannerRow(icon: "icloud.and.arrow.up", text: "Отправка…")
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.25), value: session.isOnline)
        .animation(.easeInOut(duration: 0.25), value: session.outbox.isSyncing)
    }

    private func bannerRow(icon: String, text: LocalizedStringKey) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)
                .font(.system(size: 12, weight: .semibold))
            Text(text)
                .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
        }
        .foregroundStyle(Color.inkSecondary)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 6)
        .background(Color.surface)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(Color.hairline)
                .frame(height: 1)
        }
    }
}

/// Нижняя строка чека вместо системного таббара: вкладки — текстом, как в
/// референсе (`docs/design/receipt/receipt/groups.png`): выбранная — плотным
/// начертанием и точкой, остальные — подчёркнутыми ссылками. «+» — плоская
/// печать цвета действия ровно по центру строки, две вкладки слева, две справа.
/// Ничто не меняет ширины: жирное начертание зарезервировано, бейдж —
/// надстрочный, точка — под подписью. Поэтому «+» не ездит ни при смене вкладки,
/// ни при появлении счётчика. Не помещаются подписи — строка переходит на значки.
struct ReceiptTabBar: View {
    @Binding var selection: MainTabView.Tab
    let badge: String?
    let onAdd: () -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            row(iconsOnly: false)
            row(iconsOnly: true)
        }
        .padding(.horizontal, 12)
        // Запас сверху под надстрочный бейдж: на крупном шрифте он высокий и
        // иначе заходил за черту строки.
        .padding(.top, 14)
        .padding(.bottom, 6)
        .frame(maxWidth: .infinity)
        .background(Color.bg)
        .overlay(alignment: .top) {
            Rectangle().fill(Color.hairline).frame(height: 1)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("tabBar")
    }

    private func row(iconsOnly: Bool) -> some View {
        CenteredTabsLayout(minGap: 6) {
            item(.friends, "Друзья", icon: "person.2", iconsOnly: iconsOnly)
            item(.groups, "Группы", icon: "person.3", iconsOnly: iconsOnly)
            addButton
            item(.activity, "Уведомления", icon: "bell", iconsOnly: iconsOnly, badge: badge)
            item(.account, "Профиль", icon: "person.crop.circle", iconsOnly: iconsOnly)
        }
    }

    private func item(
        _ tab: MainTabView.Tab,
        _ title: LocalizedStringKey,
        icon: String,
        iconsOnly: Bool,
        badge: String? = nil
    ) -> some View {
        let isOn = selection == tab
        return Button {
            if selection != tab { Haptics.tap() }
            selection = tab
        } label: {
            Group {
                if iconsOnly {
                    Image(systemName: isOn ? icon + ".fill" : icon)
                        .font(.system(size: 19, weight: .medium))
                        .frame(width: 28)
                } else {
                    // Невидимая жирная подпись держит ширину: выбор её не меняет.
                    // Без подчёркиваний: четыре подчёркнутые подписи читались
                    // мелким футером сайта. Выбор — чернилами, начертанием и точкой.
                    Text(title)
                        .scaledFont(size: 13, weight: .semibold)
                        .lineLimit(1)
                        .fixedSize()
                        .hidden()
                        .overlay {
                            Text(title)
                                .scaledFont(size: 13, weight: isOn ? .semibold : .regular)
                                .foregroundStyle(isOn ? Color.ink : Color.inkSecondary)
                                .lineLimit(1)
                                .fixedSize()
                        }
                }
            }
            // Надстрочный бейдж: правым краем — по правому краю подписи и растёт
            // влево над ней, не заходя в соседнюю вкладку (иначе «99+» читался
            // счётчиком «Профиля»). Ширины не занимает, «+» не сдвигается.
            // У значка — правым краем по краю зоны нажатия, едва касаясь верха значка.
            .overlay(alignment: .topTrailing) {
                if let badge {
                    badgeView(badge)
                        .fixedSize()
                        .frame(width: 0, height: 0, alignment: .bottomTrailing)
                        .offset(x: iconsOnly ? 8 : 0, y: iconsOnly ? 3 : -1)
                }
            }
            // Точка под подписью — и в режиме значков: выбор виден формой, а не
            // только начертанием или заливкой; ширины тоже не занимает.
            .overlay(alignment: .bottom) {
                if isOn {
                    Circle().fill(Color.ink).frame(width: 4, height: 4).offset(y: 8)
                }
            }
            .foregroundStyle(Color.ink)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text(title))
        .accessibilityValue(Text(verbatim: badge ?? ""))
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .accessibilityIdentifier("tab.\(String(describing: tab))")
    }

    private func badgeView(_ text: String) -> some View {
        Text(verbatim: text)
            .scaledFont(size: 10, weight: .bold, design: .monospaced, relativeTo: .caption2)
            .foregroundStyle(Color.onAccent)
            .padding(.horizontal, 3)
            .frame(minWidth: 14, minHeight: 14)
            .background(Color.accent, in: RoundedRectangle(cornerRadius: 2, style: .continuous))
    }

    private var addButton: some View {
        Button(action: onAdd) {
            Image(systemName: "plus")
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(Color.onAccent)
                .frame(width: 44, height: 36)
                .background(Color.accent, in: RoundedRectangle(cornerRadius: 3, style: .continuous))
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Добавить расход")
    }
}

/// Пять элементов строки: средний — ровно по центру, по два слева и справа
/// делят свою половину поровну (равные зазоры, включая края). Идеальная
/// ширина — по более широкой половине, чтобы ViewThatFits честно переходил на
/// значки, когда подписи в половину не помещаются.
struct CenteredTabsLayout: Layout {
    var minGap: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
        let height = sizes.map(\.height).max() ?? 0
        guard sizes.count == 5 else { return CGSize(width: proposal.width ?? 0, height: height) }
        let side = max(sizes[0].width + sizes[1].width, sizes[3].width + sizes[4].width) + minGap * 3
        let needed = side * 2 + sizes[2].width
        return CGSize(width: proposal.width.map { max($0, needed) } ?? needed, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
        guard sizes.count == 5 else { return }
        let mid = sizes[2].width
        let half = (bounds.width - mid) / 2
        subviews[2].place(at: CGPoint(x: bounds.midX, y: bounds.midY), anchor: .center, proposal: .unspecified)
        place([0, 1], from: bounds.minX, width: half, sizes: sizes, subviews: subviews, y: bounds.midY)
        place([3, 4], from: bounds.minX + half + mid, width: half, sizes: sizes, subviews: subviews, y: bounds.midY)
    }

    private func place(_ indices: [Int], from x: CGFloat, width: CGFloat, sizes: [CGSize], subviews: Subviews, y: CGFloat) {
        let used = indices.map { sizes[$0].width }.reduce(0, +)
        let gap = max(0, (width - used) / CGFloat(indices.count + 1))
        var cursor = x + gap
        for index in indices {
            subviews[index].place(at: CGPoint(x: cursor, y: y), anchor: .leading, proposal: .unspecified)
            cursor += sizes[index].width + gap
        }
    }
}

#Preview {
    MainTabView()
        .environment(SessionStore())
}
