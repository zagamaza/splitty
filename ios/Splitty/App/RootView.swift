import SwiftUI

/// Корневой экран: таб-бар для авторизованного пользователя, иначе логин.
struct RootView: View {
    @Environment(SessionStore.self) private var session

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
    }
}

#Preview {
    RootView()
        .environment(SessionStore())
}
