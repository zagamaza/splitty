import SwiftUI
import UIKit

/// Присоединение к группе по коду приглашения (roomId).
/// Принимает «голый» код и любую ссылку-приглашение — разбор в `RoomCodeParser`.
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
    /// Разбор — в `RoomCodeParser`: те же правила применяет обработчик диплинка,
    /// и второй копии этих правил в проекте быть не должно.
    private var roomId: String {
        RoomCodeParser.roomId(from: code) ?? ""
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
                                    .foregroundStyle(Color.accentText)
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
                            .foregroundStyle(Color.negativeText)
                            .padding(.horizontal, 4)
                    }
                    // Форматов ссылки два (страница приглашения и легаси-ссылка
                    // бота), и подсказка обязана называть оба: человек вставляет
                    // то, что ему прислали, и не должен гадать, «та» ли это ссылка.
                    //
                    // Домен страницы приглашения НЕ называем: его знает только
                    // сервер (см. RoomDetail.inviteUrl), и вписанный сюда
                    // разошёлся бы с реальным на первой же смене — распознаётся
                    // ссылка всё равно по пути /join/, а не по хосту.
                    Text("Вставьте код из приглашения или ссылку целиком — и вида …/join/…, и t.me/split_money_bot?start=room…")
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
