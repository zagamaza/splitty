import SwiftUI

/// Присоединение к группе по коду приглашения (roomId).
/// Принимает и «голый» код, и ссылку вида t.me/…?start=room<id>.
struct JoinGroupView: View {
    private let onJoined: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var code = ""
    @State private var isJoining = false
    @State private var alertMessage: String?
    @FocusState private var isCodeFocused: Bool

    init(onJoined: @escaping () -> Void) {
        self.onJoined = onJoined
    }

    /// Код группы, извлечённый из введённого текста (код или ссылка-приглашение).
    private var roomId: String {
        var text = code.trimmingCharacters(in: .whitespacesAndNewlines)
        if let range = text.range(of: "start=room") {
            text = String(text[range.upperBound...])
        } else if text.lowercased().hasPrefix("room") {
            text = String(text.dropFirst(4))
        }
        // Обрезаем возможный «хвост» ссылки.
        return String(text.prefix { $0.isHexDigit })
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Код группы", text: $code)
                        .font(.system(size: 17, design: .rounded))
                        .focused($isCodeFocused)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .submitLabel(.join)
                        .onSubmit { Task { await join() } }
                        .surfaceCard()
                    Text("Вставьте код из приглашения или целиком ссылку вида t.me/split_money_bot?start=room…")
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                        .padding(.horizontal, 4)
                    Button {
                        Task { await join() }
                    } label: {
                        if isJoining {
                            HStack {
                                ProgressView()
                                    .tint(.white)
                                Text("Присоединение…")
                            }
                        } else {
                            Text("Присоединиться")
                        }
                    }
                    .buttonStyle(.primaryPill)
                    .disabled(roomId.isEmpty || isJoining)
                    .padding(.top, 8)
                }
                .padding(16)
            }
            .background(Color.bg)
            .navigationTitle("Присоединиться")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
            }
            .onAppear { isCodeFocused = true }
            .alert("Ошибка", isPresented: alertPresented) {
                Button("Ок", role: .cancel) {}
            } message: {
                Text(alertMessage ?? "")
            }
        }
        .presentationDetents([.medium, .large])
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )
    }

    private func join() async {
        guard !roomId.isEmpty, !isJoining else { return }
        isJoining = true
        defer { isJoining = false }
        do {
            _ = try await session.api.joinRoom(id: roomId)
            // Единая инвалидация: список групп перезагрузится по dataVersion.
            session.noteDataChanged()
            Haptics.success()
            onJoined()
            dismiss()
        } catch {
            alertMessage = error.localizedDescription
        }
    }
}
