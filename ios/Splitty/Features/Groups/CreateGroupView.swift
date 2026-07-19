import SwiftUI

/// Создание группы: поле «Название», CTA «Создать».
struct CreateGroupView: View {
    private let onCreated: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var isSaving = false
    @State private var alertMessage: String?
    @FocusState private var isNameFocused: Bool

    init(onCreated: @escaping () -> Void) {
        self.onCreated = onCreated
    }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Название", text: $name)
                        .scaledFont(size: 17)
                        .focused($isNameFocused)
                        .submitLabel(.done)
                        .onSubmit { Task { await create() } }
                        .surfaceCard()
                    Text("Например: «Поездка в Стамбул» или «Квартира».")
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                        .padding(.horizontal, 4)
                    Button {
                        Task { await create() }
                    } label: {
                        if isSaving {
                            HStack {
                                ProgressView()
                                    .tint(.white)
                                Text("Создание…")
                            }
                        } else {
                            Text("Создать")
                        }
                    }
                    .buttonStyle(.primaryPill)
                    .disabled(trimmedName.isEmpty || isSaving)
                    .padding(.top, 8)
                }
                .padding(16)
            }
            .background(Color.bg)
            .navigationTitle("Новая группа")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
            }
            .onAppear { isNameFocused = true }
            .errorAlert($alertMessage)
        }
        .presentationDetents([.medium, .large])
        // Введённое название нельзя потерять случайным смахиванием sheet.
        .interactiveDismissDisabled(!name.isEmpty)
    }

    private func create() async {
        guard !trimmedName.isEmpty, !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            _ = try await session.api.createRoom(name: trimmedName)
            // Единая инвалидация: список групп перезагрузится по dataVersion.
            session.noteDataChanged()
            Haptics.success()
            onCreated()
            dismiss()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }
}
