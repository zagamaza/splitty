import SwiftUI

/// Корневой экран: таб-бар для авторизованного пользователя, иначе логин.
struct RootView: View {
    @Environment(SessionStore.self) private var session

    var body: some View {
        if session.isAuthenticated {
            MainTabView()
        } else {
            LoginView()
        }
    }
}

#Preview {
    RootView()
        .environment(SessionStore())
}
