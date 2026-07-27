import SwiftUI
import UIKit

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

    private var trimmedCode: String {
        code.trimmingCharacters(in: .whitespacesAndNewlines)
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
                    HStack(spacing: 8) {
                        TextField("Ссылка или код", text: $code)
                            .scaledFont(size: 17)
                            .focused($isCodeFocused)
                            .autocorrectionDisabled()
                            .textInputAutocapitalization(.never)
                            .submitLabel(.join)
                            .onSubmit { Task { await join() } }
                        // Код никто не набирает руками — его присылают ссылкой.
                        // Кнопка вставляет из буфера в один тап.
                        if UIPasteboard.general.hasStrings {
                            Button {
                                if let pasted = UIPasteboard.general.string {
                                    code = pasted
                                }
                            } label: {
                                Label("Вставить", systemImage: "doc.on.clipboard")
                                    .labelStyle(.titleAndIcon)
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(Color.accent)
                            }
                            .fixedSize()
                        }
                    }
                    .surfaceCard()
                    // Ввели текст, а код не распознан — объясняем, почему
                    // кнопка неактивна, вместо молчаливого disabled.
                    if !trimmedCode.isEmpty, roomId.isEmpty {
                        Text("Не похоже на код или ссылку группы")
                            .font(.footnote)
                            .foregroundStyle(Color.negative)
                            .padding(.horizontal, 4)
                    }
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
            .errorAlert($alertMessage)
        }
        .presentationDetents([.medium, .large])
        // Введённый код нельзя потерять случайным смахиванием sheet.
        .interactiveDismissDisabled(!code.isEmpty)
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
            alertMessage = humanErrorText(error)
        }
    }
}
