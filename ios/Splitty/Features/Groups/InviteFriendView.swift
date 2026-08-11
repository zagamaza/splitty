import SwiftUI

/// Выбор друзей для приглашения в группу.
///
/// Друг — это человек, с которым уже была общая группа, значит его id известен
/// и вводить код никому не нужно: он просто получает уведомление. Тем, кого в
/// списке нет, остаётся ссылка (кнопка внизу).
struct InviteFriendView: View {
    let roomId: String
    /// Кто уже в этой группе — таких не показываем, приглашение им ничего бы
    /// не сделало.
    let existingMemberIds: Set<Int>
    let onInvited: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss

    @State private var friends: [FriendBalance] = []
    @State private var selected: Set<Int> = []
    @State private var isLoading = true
    @State private var isSending = false
    @State private var alertMessage: String?
    /// Итог приглашения, когда рассказать есть что (кто-то ждёт согласия,
    /// кого-то не получилось позвать). По «ОК» экран закрывается.
    @State private var resultMessage: String?
    @State private var isLinkPresented = false

    var body: some View {
        NavigationStack {
            content
                .background(Color.bg)
                .navigationTitle("Пригласить")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Отмена") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Пригласить") {
                            Task { await invite() }
                        }
                        .disabled(selected.isEmpty || isSending)
                    }
                }
                .task { await load() }
                .errorAlert($alertMessage)
                .alert(
                    "Приглашение",
                    isPresented: Binding(
                        get: { resultMessage != nil },
                        set: { if !$0 { resultMessage = nil } }
                    )
                ) {
                    Button("ОК") { dismiss() }
                } message: {
                    Text(resultMessage ?? "")
                }
                .sheet(isPresented: $isLinkPresented) {
                    // Ссылка — запасной канал для тех, с кем ещё не делили расходы.
                    InviteLinkSheet(roomId: roomId)
                }
        }
        .presentationDetents([.medium, .large])
    }

    @ViewBuilder
    private var content: some View {
        if isLoading {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if candidates.isEmpty {
                        emptyState
                    } else {
                        Text("Из ваших групп")
                            .sectionHeaderStyle()
                            .padding(.horizontal, 4)
                        VStack(spacing: 0) {
                            ForEach(candidates, id: \.user.id) { friend in
                                row(friend)
                                if friend.user.id != candidates.last?.user.id {
                                    Rectangle()
                                        .fill(Color.hairline)
                                        .frame(height: 1)
                                        .padding(.leading, 60)
                                }
                            }
                        }
                        .surfaceCard(padding: 0)
                    }

                    Button {
                        isLinkPresented = true
                    } label: {
                        Label("Отправить ссылку", systemImage: "link")
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.softChip)
                    Text("Ссылка нужна тем, с кем вы ещё не делили расходы.")
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                        .padding(.horizontal, 4)
                }
                .padding(16)
            }
        }
    }

    private var emptyState: some View {
        Text("Пока некого пригласить: здесь появятся люди, с которыми у вас были общие группы.")
            .font(.subheadline)
            .foregroundStyle(Color.inkSecondary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 4)
    }

    /// Кого показываем: друзья минус те, кто уже в этой группе, и минус
    /// удалённые. Удалённые остаются в `/friends` (их снимки в комнатах не
    /// исчезают, только анонимизируются), а приглашение им вернуло бы 404.
    private var candidates: [FriendBalance] {
        friends.filter { !existingMemberIds.contains($0.user.id) && !$0.user.deleted }
    }

    private func row(_ friend: FriendBalance) -> some View {
        Button {
            if selected.contains(friend.user.id) {
                selected.remove(friend.user.id)
            } else {
                selected.insert(friend.user.id)
            }
        } label: {
            HStack(spacing: 12) {
                UserAvatarView(user: friend.user, size: 36)
                Text(friend.user.displayName)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                Spacer(minLength: 8)
                Image(systemName: selected.contains(friend.user.id) ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(selected.contains(friend.user.id) ? Color.accent : Color.hairline)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        defer { isLoading = false }
        do {
            friends = try await session.api.friends()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }

    /// Приглашения уходят по одному, и часть может не дойти. Раньше любой сбой
    /// считался общим провалом: показывалась первая ошибка, а уже позванные
    /// люди не доезжали до списка — экран закрывался, не обновив ничего.
    private func invite() async {
        isSending = true
        defer { isSending = false }

        var added: [String] = []
        var pending: [String] = []
        var failed: [String] = []
        /// Причина последнего сбоя: «нет соединения» и «нет доступа» человеку
        /// подсказывают разное, и терять её нельзя.
        var reason: String?
        for userId in selected.sorted() {
            let name = friends.first { $0.user.id == userId }?.user.displayName ?? "Участник"
            do {
                let status = try await session.api.addMember(roomId: roomId, userId: userId)
                if status == .pending {
                    pending.append(name)
                } else {
                    added.append(name)
                }
                // Осталось в выборе — только то, что не прошло: повтор не
                // должен звать по второму разу уже позванных.
                selected.remove(userId)
            } catch {
                failed.append(name)
                reason = humanErrorText(error)
            }
        }

        guard !added.isEmpty || !pending.isEmpty else {
            alertMessage = "Не удалось пригласить: \(failed.joined(separator: ", "))"
                + (reason.map { ". \($0)" } ?? "")
            return
        }
        // Хоть кто-то позван — данные изменились, и звавший должен это увидеть,
        // даже если остальные приглашения упали.
        session.noteDataChanged()
        Haptics.success()
        onInvited()
        if let message = Self.resultText(added: added, pending: pending, failed: failed) {
            resultMessage = message
        } else {
            dismiss()
        }
    }

    /// Итог приглашения человеческим текстом; nil — всех просто добавили,
    /// объяснять нечего.
    ///
    /// `pending` — не отказ и не успех «добавлен»: человек уже выходил из
    /// группы, и вернуть его можно только с его согласия.
    static func resultText(added: [String], pending: [String], failed: [String]) -> String? {
        var lines: [String] = []
        if !pending.isEmpty {
            lines.append("Приглашение отправлено — ждём согласия: \(pending.joined(separator: ", "))")
        }
        if !failed.isEmpty {
            lines.append("Не удалось пригласить: \(failed.joined(separator: ", "))")
        }
        if lines.isEmpty {
            return nil
        }
        if !added.isEmpty {
            lines.insert("Добавлен(а) в группу: \(added.joined(separator: ", "))", at: 0)
        }
        return lines.joined(separator: "\n")
    }
}

/// Обёртка над существующим шитом ссылки — чтобы открыть его из выбора друзей.
private struct InviteLinkSheet: View {
    let roomId: String
    @Environment(SessionStore.self) private var session
    @State private var room: RoomDetail?

    var body: some View {
        Group {
            if let room {
                InviteGroupView(room: room)
            } else {
                ProgressView()
            }
        }
        .task {
            room = try? await session.api.room(id: roomId)
        }
    }
}
