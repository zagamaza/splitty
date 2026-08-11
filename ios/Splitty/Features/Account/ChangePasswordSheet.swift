import SwiftUI

/// Задать или сменить пароль от аккаунта — POST /me/password.
/// Забывшему текущий пароль остаётся «Не помню текущий»: пароль отвязывается
/// (DELETE /me/link/password, адрес входа остаётся за аккаунтом) и задаётся
/// заново. Писем мы не шлём, другого пути восстановления нет.
struct ChangePasswordSheet: View {
    /// Пароль у аккаунта уже задан — сервер потребует текущий.
    let hasPassword: Bool

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss

    /// Пароль сброшен прямо в этом листе: текущий больше не спрашиваем.
    @State private var didReset = false
    @State private var isResetConfirmPresented = false
    @State private var currentPassword = ""
    @State private var newPassword = ""
    @State private var repeatPassword = ""
    @State private var isSaving = false
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            ScrollView {
                form.padding(16)
            }
            .background(Color.bg)
            .navigationTitle(hasPassword ? "Смена пароля" : "Пароль")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                        .disabled(isSaving)
                }
            }
            .confirmationDialog(
                "Сбросить пароль?",
                isPresented: $isResetConfirmPresented,
                titleVisibility: .visible
            ) {
                Button("Сбросить", role: .destructive) { resetPassword() }
                Button("Отмена", role: .cancel) {}
            } message: {
                Text("Старый пароль перестанет работать, вход по \(session.me?.loginEmail ?? "email") останется доступен после того, как зададите новый.")
            }
            .alert(
                "Ошибка",
                isPresented: Binding(
                    get: { errorMessage != nil },
                    set: { if !$0 { errorMessage = nil } }
                )
            ) {
                Button("Ок", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
        }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 12) {
            if needsCurrent {
                SecureField("Текущий пароль", text: $currentPassword)
                    .textContentType(.password)
                    .modifier(PasswordFieldStyle())

                Button("Не помню текущий") {
                    isResetConfirmPresented = true
                }
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.accent)
                .disabled(isSaving)
            }

            SecureField("Новый пароль", text: $newPassword)
                .textContentType(.newPassword)
                .modifier(PasswordFieldStyle())

            SecureField("Новый пароль ещё раз", text: $repeatPassword)
                .textContentType(.newPassword)
                .modifier(PasswordFieldStyle())
                .submitLabel(.done)
                .onSubmit { save() }

            if let hint {
                Text(hint)
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }

            Button {
                save()
            } label: {
                Text("Сохранить")
            }
            .buttonStyle(.primaryPill)
            .disabled(!isValid || isSaving)
            .padding(.top, 4)
        }
    }

    private var hint: String? {
        if !newPassword.isEmpty, !EmailLoginForm.isValidPassword(newPassword) {
            return String(localized: "Пароль — не короче \(EmailLoginForm.minPasswordLength) символов")
        }
        if !repeatPassword.isEmpty, repeatPassword != newPassword {
            return String(localized: "Пароли не совпадают")
        }
        return nil
    }

    private var needsCurrent: Bool { hasPassword && !didReset }

    private var isValid: Bool {
        EmailLoginForm.isValidPassword(newPassword)
            && newPassword == repeatPassword
            && (!needsCurrent || !currentPassword.isEmpty)
    }

    private func save() {
        guard isValid, !isSaving else { return }
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                try await session.setPassword(
                    current: needsCurrent ? currentPassword : nil,
                    new: newPassword
                )
                Haptics.success()
                dismiss()
            } catch {
                errorMessage = identityErrorText(error)
            }
        }
    }

    /// 409 last_identity — пароль здесь единственный вход, и сбрасывать его
    /// нельзя: аккаунт остался бы без доступа. Текст даёт `identityErrorText`.
    private func resetPassword() {
        isSaving = true
        Task {
            defer { isSaving = false }
            do {
                try await session.unlink(.password)
                didReset = true
                currentPassword = ""
            } catch {
                errorMessage = identityErrorText(error)
            }
        }
    }
}

private struct PasswordFieldStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .scaledFont(size: 17)
            .foregroundStyle(Color.ink)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .background(
                Color.surface,
                in: RoundedRectangle(cornerRadius: 12, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(Color.hairline, lineWidth: 1)
            }
    }
}

#Preview {
    ChangePasswordSheet(hasPassword: true)
        .environment(SessionStore())
}
