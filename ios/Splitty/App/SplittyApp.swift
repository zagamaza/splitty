import SwiftUI

/// Точка входа приложения. SessionStore создаётся здесь и пробрасывается
/// во все view через environment.
@main
struct SplittyApp: App {
    @State private var session = SessionStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(session)
                .tint(.accent)
                .task {
                    await session.refreshMe()
                }
        }
    }
}
