import SwiftUI

/// Настройки уведомлений: категория событий × канал доставки.
/// Telegram работает сразу (шлёт бот), «Приложение» — задел под пуши
/// (APNs/FCM), пока выключен и подписан «скоро».
struct NotificationSettingsView: View {
    @Environment(SessionStore.self) private var session
    @State private var settings: NotifySettings?
    @State private var errorMessage: String?
    /// true — PATCH в полёте; тумблеры не блокируем, но изменения сериализуем.
    @State private var isSaving = false

    var body: some View {
        Group {
            if let settings {
                form(settings)
            } else if let errorMessage {
                ContentUnavailableView {
                    Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
                } description: {
                    Text(errorMessage)
                } actions: {
                    Button("Повторить") {
                        Task { await load() }
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Color.accent)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Color.bg)
        .navigationTitle("Уведомления")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .alert(
            "Ошибка",
            isPresented: Binding(
                get: { errorMessage != nil && settings != nil },
                set: { if !$0 { errorMessage = nil } }
            )
        ) {
            Button("Ок", role: .cancel) {}
        } message: {
            Text(errorMessage ?? "")
        }
    }

    private func form(_ current: NotifySettings) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                section(
                    title: "Операции",
                    footer: "Кто-то добавил или изменил расход в вашей тусе",
                    telegram: Binding(
                        get: { current.operations.telegram },
                        set: { newValue in
                            var updated = current
                            updated.operations.telegram = newValue
                            save(updated)
                        }
                    )
                )
                section(
                    title: "Долги",
                    footer: "Вам вернули долг",
                    telegram: Binding(
                        get: { current.debts.telegram },
                        set: { newValue in
                            var updated = current
                            updated.debts.telegram = newValue
                            save(updated)
                        }
                    )
                )
            }
            .padding(16)
        }
    }

    private func section(title: String, footer: String, telegram: Binding<Bool>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .sectionHeaderStyle()
                .padding(.leading, 4)
            VStack(spacing: 0) {
                Toggle(isOn: telegram) {
                    Label("Telegram", systemImage: "paperplane")
                        .font(.system(size: 16, design: .rounded))
                        .foregroundStyle(Color.ink)
                }
                .tint(Color.accent)
                .padding(.horizontal, 16)
                .padding(.vertical, 10)

                Rectangle().fill(Color.hairline).frame(height: 1).padding(.leading, 16)

                // Пуши появятся вместе с APNs/FCM — тумблер-задел, пока недоступен.
                Toggle(isOn: .constant(false)) {
                    HStack(spacing: 6) {
                        Label("Приложение", systemImage: "app.badge")
                            .font(.system(size: 16, design: .rounded))
                        Text("скоро")
                            .font(.system(size: 11, weight: .semibold, design: .rounded))
                            .padding(.horizontal, 7)
                            .padding(.vertical, 2)
                            .background(Color.hairline, in: Capsule())
                    }
                    .foregroundStyle(Color.inkSecondary)
                }
                .disabled(true)
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
            }
            .surfaceCard(padding: 0)
            Text(footer)
                .font(.system(size: 12, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
        }
    }

    private func load() async {
        do {
            settings = try await session.api.notifications()
        } catch {
            if error.isTaskCancellation { return }
            errorMessage = error.localizedDescription
        }
    }

    /// Оптимистичное сохранение: UI обновляется сразу, при ошибке — откат.
    private func save(_ updated: NotifySettings) {
        let previous = settings
        settings = updated
        Task {
            do {
                settings = try await session.api.updateNotifications(updated)
            } catch {
                if error.isTaskCancellation { return }
                settings = previous
                errorMessage = error.localizedDescription
            }
        }
    }
}

#Preview {
    NavigationStack {
        NotificationSettingsView()
    }
    .environment(SessionStore())
}
