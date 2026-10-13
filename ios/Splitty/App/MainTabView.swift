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
/// референсе (`docs/design/receipt/receipt/groups.png`): выбранная — точкой и
/// плотным начертанием, остальные — подчёркнутыми ссылками. «+» — плоская
/// печать цвета действия вровень со строкой. Если текст не помещается
/// (крупный шрифт, длинный язык), строка переходит на значки.
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
        .padding(.top, 10)
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
        HStack(spacing: 0) {
            item(.friends, "Друзья", icon: "person.2", iconsOnly: iconsOnly)
            Spacer(minLength: 6)
            item(.groups, "Группы", icon: "person.3", iconsOnly: iconsOnly)
            Spacer(minLength: 6)
            addButton
            Spacer(minLength: 6)
            item(.activity, "Уведомления", icon: "bell", iconsOnly: iconsOnly, badge: badge, reservesBadge: true)
            Spacer(minLength: 6)
            item(.account, "Профиль", icon: "person.crop.circle", iconsOnly: iconsOnly)
        }
    }

    private func item(
        _ tab: MainTabView.Tab,
        _ title: LocalizedStringKey,
        icon: String,
        iconsOnly: Bool,
        badge: String? = nil,
        reservesBadge: Bool = false
    ) -> some View {
        let isOn = selection == tab
        return Button {
            if selection != tab { Haptics.tap() }
            selection = tab
        } label: {
            // Ширина пункта не зависит ни от выбора, ни от счётчика: место под
            // жирное начертание и бейдж «99+» занято всегда. Иначе гибкие зазоры
            // перераспределялись, и «+» ездил при каждой смене вкладки.
            HStack(spacing: 5) {
                Group {
                if iconsOnly {
                    Image(systemName: isOn ? icon + ".fill" : icon)
                        .font(.system(size: 19, weight: .medium))
                        .frame(width: 28)
                } else {
                    Text(title)
                        .scaledFont(size: 14, weight: .semibold)
                        .lineLimit(1)
                        .fixedSize()
                        .hidden()
                        .overlay(alignment: .leading) {
                            Text(title)
                                .scaledFont(size: 14, weight: isOn ? .semibold : .regular)
                                .underline(!isOn, color: Color.ink.opacity(0.45))
                                .lineLimit(1)
                                .fixedSize()
                        }
                }
                }
                // Точка — в зазоре перед подписью, ширины не занимает; и в
                // режиме значков: выбор виден формой, а не только заливкой.
                .overlay(alignment: .leading) {
                    if isOn {
                        Circle().fill(Color.ink).frame(width: 5, height: 5).offset(x: -9)
                    }
                }
                if reservesBadge {
                    badgeView("99+")
                        .hidden()
                        .overlay(alignment: .leading) {
                            if let badge { badgeView(badge) }
                        }
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
            .scaledFont(size: 11, weight: .bold, design: .monospaced, relativeTo: .caption2)
            .foregroundStyle(Color.onAccent)
            .padding(.horizontal, 4)
            .frame(minWidth: 16, minHeight: 16)
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

#Preview {
    MainTabView()
        .environment(SessionStore())
}
