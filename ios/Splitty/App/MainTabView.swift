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

/// Главный таб-бар: 5 вкладок, центральная — приподнятая зелёная кнопка «+»,
/// открывающая AddExpenseView как sheet (с выбором группы внутри).
/// Здесь же — глобальный офлайн-баннер (тонкая полоса сверху) и триггеры
/// синка outbox: сеть появилась / приложение стало активным.
struct MainTabView: View {
    private enum Tab: Hashable {
        case friends, groups, add, activity, account
    }

    @Environment(SessionStore.self) private var session
    @Environment(\.scenePhase) private var scenePhase
    @State private var selection: Tab = .friends
    @State private var isAddExpensePresented = false
    /// Кнопка «+» скрывается при видимой клавиатуре: overlay поднимается
    /// вместе с safe area и иначе висит поверх контента над клавиатурой.
    @State private var isKeyboardVisible = false
    /// true — открыт экран с собственным нижним баром (туса): глобальный «+» скрыт.
    @State private var isGlobalAddHidden = false

    var body: some View {
        TabView(selection: $selection) {
            FriendsListView()
                .tabItem { Label("Друзья", systemImage: "person.2.fill") }
                .tag(Tab.friends)

            GroupsListView()
                .tabItem { Label("Группы", systemImage: "person.3.fill") }
                .tag(Tab.groups)

            // Пустая вкладка-заглушка под центральной кнопкой «+»:
            // для VoiceOver невидима — реальное действие у overlay-кнопки.
            Color.clear
                .accessibilityHidden(true)
                .tabItem { Text("").accessibilityHidden(true) }
                .tag(Tab.add)

            // Раздел стал не журналом, а входящими: тут лежат приглашения с
            // кнопками, поэтому колокол и бейдж, а не часы.
            ActivityView()
                .tabItem { Label("Уведомления", systemImage: "bell.fill") }
                .badge(session.unreadNotifications)
                .tag(Tab.activity)

            AccountView()
                .tabItem { Label("Профиль", systemImage: "person.crop.circle.fill") }
                .tag(Tab.account)
        }
        .tint(Color.accent)
        .overlay(alignment: .bottom) {
            if !isKeyboardVisible && !isGlobalAddHidden {
                addExpenseButton
            }
        }
        .onPreferenceChange(HidesGlobalAddButtonKey.self) { isGlobalAddHidden = $0 }
        // Глобальный тонкий баннер офлайна/отправки outbox: полоса под
        // статус-баром над навбарами всех вкладок; онлайн без синка — скрыт.
        .safeAreaInset(edge: .top, spacing: 0) {
            statusBanner
        }
        .onChange(of: selection) { oldValue, newValue in
            // Вкладку «+» не открываем — возвращаем прежнюю и показываем sheet.
            if newValue == .add {
                selection = oldValue
                isAddExpensePresented = true
            }
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
                Task { await session.syncOutbox() }
            }
        }
        .task { await session.syncOutbox() }
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

    private func bannerRow(icon: String, text: String) -> some View {
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

    /// Центральная приподнятая кнопка добавления расхода:
    /// изумрудный градиент, мягкая цветная тень, белый plus.
    private var addExpenseButton: some View {
        Button {
            Haptics.tap()
            isAddExpensePresented = true
        } label: {
            ZStack {
                Circle()
                    .fill(
                        LinearGradient(
                            colors: [Color.accent, Color.accentPressed],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
                    .frame(width: 58, height: 58)
                    .shadow(color: Color.accent.opacity(0.35), radius: 10, y: 5)
                Image(systemName: "plus")
                    .font(.system(size: 24, weight: .semibold))
                    .foregroundStyle(.white)
            }
        }
        .accessibilityLabel("Добавить расход")
        .offset(y: -18)
    }
}

#Preview {
    MainTabView()
        .environment(SessionStore())
}
