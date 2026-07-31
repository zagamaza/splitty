import SwiftUI

/// Корневой экран: таб-бар для авторизованного пользователя, иначе логин.
/// Здесь же исполняется отложенное вступление в группу по ссылке-приглашению
/// (`PendingJoin`): корень — единственное место, которое живёт и до входа,
/// и после, и переживает переключение логин ↔ табы.
struct RootView: View {
    @Environment(SessionStore.self) private var session

    /// Комната, в которую вступили по ссылке — открывается поверх табов.
    @State private var joinedRoom: JoinedRoom?
    /// true — запрос вступления в полёте (показываем «Присоединяемся…»
    /// и не даём запустить второй).
    @State private var isJoining = false
    @State private var joinError: String?

    /// Обёртка кода комнаты для `fullScreenCover(item:)`.
    private struct JoinedRoom: Identifiable {
        let id: String
    }

    var body: some View {
        // Мягкий кроссфейд логин ↔ табы: без него смена корня резала кадр.
        Group {
            if session.isAuthenticated {
                MainTabView()
                    .transition(.opacity)
            } else {
                LoginView()
                    .transition(.opacity)
            }
        }
        .animation(.easeInOut(duration: 0.3), value: session.isAuthenticated)
        .overlay {
            if isJoining {
                joiningOverlay
            }
        }
        // Ссылка пришла на живое приложение (или намерение осталось с прошлого
        // запуска и его подхватил init PendingJoin).
        .onChange(of: PendingJoin.shared.roomId) {
            joinPendingRoom()
        }
        // Переход «гость → вошёл»: намерение ждало входа, теперь исполняем.
        .onChange(of: session.isAuthenticated) { _, isAuthenticated in
            if isAuthenticated {
                joinPendingRoom()
            }
        }
        // Холодный старт: и ссылка, и отложенное с прошлого раза намерение
        // уже лежат в PendingJoin к моменту появления корня.
        .task { joinPendingRoom() }
        .fullScreenCover(item: $joinedRoom) { room in
            NavigationStack {
                GroupDetailView(roomId: room.id)
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Готово") { joinedRoom = nil }
                        }
                    }
            }
        }
        .errorAlert($joinError)
    }

    /// Вступление в группу из отложенного намерения.
    ///
    /// Гостя не трогаем: намерение лежит в `PendingJoin` до входа — иначе
    /// запрос ушёл бы без токена, получил 401 и приглашение было бы потеряно.
    @MainActor
    private func joinPendingRoom() {
        guard session.isAuthenticated, !isJoining else { return }
        guard let roomId = PendingJoin.shared.take() else { return }
        isJoining = true
        Task { @MainActor in
            defer { isJoining = false }
            do {
                _ = try await session.api.joinRoom(id: roomId)
                // Единая инвалидация: список групп перечитается по dataVersion.
                session.noteDataChanged()
                Haptics.success()
                joinedRoom = JoinedRoom(id: roomId)
            } catch let error as APIError where error.isUnauthorized {
                // Токен протух ровно на этом запросе: APIClient уже сбросил
                // сессию и нас перекинуло на вход. Возвращаем намерение —
                // после переавторизации оно исполнится само, а алерт
                // «Требуется вход» поверх экрана входа сказал бы очевидное.
                PendingJoin.shared.set(roomId)
            } catch {
                joinError = joinLinkErrorText(error)
            }
        }
    }

    /// Полупрозрачная подложка со спиннером: вступление занимает один запрос,
    /// но по ссылке приложение открывается «в никуда», и без индикации
    /// пользователь видит пустой экран непонятной длительности.
    private var joiningOverlay: some View {
        ZStack {
            Color.black.opacity(0.25)
                .ignoresSafeArea()
            VStack(spacing: 10) {
                ProgressView()
                Text("Присоединяемся к группе…")
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.inkSecondary)
            }
            .padding(20)
            .background(Color.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .transition(.opacity)
    }
}

#Preview {
    RootView()
        .environment(SessionStore())
}
