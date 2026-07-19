import SwiftUI

/// Настройки уведомлений: категория событий × канал доставки.
/// Telegram работает сразу (шлёт бот), «Приложение» — задел под пуши
/// (APNs/FCM), пока выключен и подписан «скоро».
struct NotificationSettingsView: View {
    @Environment(SessionStore.self) private var session
    @State private var settings: NotifySettings?
    @State private var errorMessage: String?
    /// true — PATCH в полёте; тумблеры задизейблены, чтобы быстрые
    /// переключения не гонялись между собой (последний ответ сервера побеждал бы).
    @State private var isSaving = false
    /// Мастер-тумблер (перенесён из AccountView, где дублировал строку-ссылку):
    /// локальная копия `me.notificationOn`, PATCH /me, при ошибке откат.
    @State private var masterOn = true

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
        .task {
            syncMasterFromMe()
            await load()
        }
        .onChange(of: session.me) {
            syncMasterFromMe()
        }
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
                masterSection
                Group {
                    section(
                        title: "Операции",
                        footer: "Кто-то добавил или изменил расход в вашей группе",
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
                // Мастер выключен — категории не действуют, показываем это
                // визуально и блокируем их переключение.
                .disabled(!masterOn)
                .opacity(masterOn ? 1 : 0.5)
            }
            .padding(16)
        }
    }

    /// Первая секция — мастер-тумблер всех уведомлений (PATCH /me).
    private var masterSection: some View {
        Toggle(isOn: $masterOn) {
            Text("Уведомления")
                .scaledFont(size: 16)
                .foregroundStyle(Color.ink)
        }
        .tint(Color.accent)
        .disabled(isSaving)
        .onChange(of: masterOn) { _, newValue in
            guard newValue != session.me?.notificationOn else { return }
            saveMaster(newValue)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 11)
        .surfaceCard(padding: 0)
    }

    private func section(title: String, footer: String, telegram: Binding<Bool>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .sectionHeaderStyle()
                .padding(.leading, 4)
            VStack(spacing: 0) {
                Toggle(isOn: telegram) {
                    Label("Telegram", systemImage: "paperplane")
                        .scaledFont(size: 16)
                        .foregroundStyle(Color.ink)
                }
                .tint(Color.accent)
                // Пока PATCH в полёте — не даём переключать: быстрые тапы
                // порождали гонку запросов с непредсказуемым итогом.
                .disabled(isSaving)
                .padding(.horizontal, 16)
                .padding(.vertical, 10)

                Rectangle().fill(Color.hairline).frame(height: 1).padding(.leading, 16)

                // Пуши появятся вместе с APNs/FCM — тумблер-задел, пока недоступен.
                Toggle(isOn: .constant(false)) {
                    HStack(spacing: 6) {
                        Label("Приложение", systemImage: "app.badge")
                            .scaledFont(size: 16)
                        Text("скоро")
                            .scaledFont(size: 11, weight: .semibold, relativeTo: .footnote)
                            .padding(.horizontal, 7)
                            .padding(.vertical, 2)
                            .background(Color.hairline, in: Capsule())
                    }
                    .foregroundStyle(Color.inkSecondary)
                }
                .disabled(true)
                .accessibilityHint("Появится в следующих версиях")
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
            }
            .surfaceCard(padding: 0)
            Text(footer)
                .scaledFont(size: 12, relativeTo: .footnote)
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
    /// На время PATCH тумблеры задизейблены (isSaving) — см. `form`.
    private func save(_ updated: NotifySettings) {
        let previous = settings
        settings = updated
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                settings = try await session.api.updateNotifications(updated)
            } catch {
                if error.isTaskCancellation { return }
                settings = previous
                errorMessage = error.localizedDescription
            }
        }
    }

    // MARK: - Мастер-тумблер (PATCH /me)

    private func syncMasterFromMe() {
        guard let me = session.me else { return }
        masterOn = me.notificationOn
    }

    /// Сохраняет мастер-настройку в профиле; при ошибке откатывает тумблер.
    private func saveMaster(_ newValue: Bool) {
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                session.me = try await session.api.updateMe(
                    displayName: nil,
                    lang: nil,
                    notificationOn: newValue
                )
                Haptics.success()
            } catch {
                if error.isTaskCancellation { return }
                syncMasterFromMe()
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
