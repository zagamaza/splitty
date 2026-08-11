import SwiftUI
import UIKit

/// Приглашение в группу. Главное действие — поделиться ссылкой (друг
/// открывает бота и вступает в один тап). Копирование ссылки и код —
/// запасные варианты: код (24-символьный roomId) вводят через
/// «Присоединиться», если ссылку не открыть.
struct InviteGroupView: View {
    let room: RoomDetail

    @Environment(\.dismiss) private var dismiss
    /// Что последним скопировали — для короткой смены подписи на «Скопировано».
    @State private var copied: CopiedItem?

    private enum CopiedItem { case link, code }

    init(room: RoomDetail) {
        self.room = room
    }

    private var inviteLink: String {
        Self.inviteLink(for: room)
    }

    /// Ссылка-приглашение.
    ///
    /// Основная форма — `inviteUrl` с сервера (`https://<домен>/join/<id>`):
    /// страница вступления уводит в приложение по universal link, а тем, у кого
    /// его нет, показывает кнопки установки. Раздаёт такие ссылки только этот
    /// экран — без него у всей ветки диплинков (страница, схема `splitty://`,
    /// `RoomCodeParser`) просто нет входной точки.
    ///
    /// Пусто — публичный домен на сервере ещё не настроен; тогда легаси-ссылка
    /// бота: она работает и сейчас, а `RoomCodeParser` понимает оба формата.
    static func inviteLink(for room: RoomDetail) -> String {
        if let inviteUrl = room.inviteUrl, !inviteUrl.isEmpty {
            return inviteUrl
        }
        return "https://t.me/split_money_bot?start=room\(room.id)"
    }

    /// Текст для системного share — ссылка плюс код на случай, если ссылку
    /// не открыть (совпадает с прежним сообщением из настроек группы).
    private var inviteMessage: String {
        String(localized: "Присоединяйся к группе «\(room.name)» в Splitty: \(inviteLink)\nКод группы: \(room.id)")
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                // Главное действие — поделиться ссылкой.
                ShareLink(item: inviteMessage) {
                    Label("Поделиться ссылкой", systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.primaryPill)

                // Код — вторичный способ: одна строка, тап копирует.
                Button {
                    copy(room.id, as: .code)
                } label: {
                    HStack(spacing: 10) {
                        Text(room.id)
                            .font(.footnote.monospaced())
                            .foregroundStyle(Color.inkSecondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Spacer(minLength: 8)
                        Image(systemName: copied == .code ? "checkmark" : "doc.on.doc")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(Color.accent)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 14)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .surfaceCard(padding: 0)

                Spacer(minLength: 0)
            }
            .padding(20)
            .background(Color.bg)
            .navigationTitle("Пригласить в группу")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Готово") { dismiss() }
                }
            }
        }
        .presentationDetents([.height(240), .medium])
    }

    private func copy(_ text: String, as item: CopiedItem) {
        UIPasteboard.general.string = text
        Haptics.success()
        withAnimation { copied = item }
    }
}
