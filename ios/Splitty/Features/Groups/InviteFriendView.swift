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

    /// Кого показываем: друзья минус те, кто уже в этой группе.
    private var candidates: [FriendBalance] {
        friends.filter { !existingMemberIds.contains($0.user.id) }
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

    private func invite() async {
        isSending = true
        defer { isSending = false }

        var failed: [String] = []
        for userId in selected {
            do {
                _ = try await session.api.addMember(roomId: roomId, userId: userId)
            } catch {
                failed.append(humanErrorText(error))
            }
        }
        if let first = failed.first {
            alertMessage = first
            return
        }
        session.noteDataChanged()
        Haptics.success()
        onInvited()
        dismiss()
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
