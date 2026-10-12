import SwiftUI

/// Аватар пользователя: бумажный круг с инициалами чернилами — первые
/// буквы слов `displayName`.
struct UserAvatarView: View {
    let user: User
    var size: CGFloat = 40
    /// Чей аватар грузить с сервера; nil — не грузить вовсе. Аватары групп
    /// строятся на ХЭШЕ id комнаты: он попадает в диапазон настоящих telegram
    /// id, и GET /users/{хэш}/avatar при совпадении рисовал бы группе ФОТО
    /// ПОСТОРОННЕГО человека.
    var avatarUserId: Int?

    /// Фото профиля Telegram (кеш в SessionStore.avatars); пока грузится
    /// или фото нет — инициалы.
    @Environment(SessionStore.self) private var session

    init(user: User, size: CGFloat = 40) {
        self.user = user
        self.size = size
        self.avatarUserId = user.id
    }

    /// Явный вариант для нечеловеческих аватаров (группа): `avatarUserId: nil`.
    init(user: User, size: CGFloat = 40, avatarUserId: Int?) {
        self.user = user
        self.size = size
        self.avatarUserId = avatarUserId
    }

    private var initials: String {
        let letters = user.displayName
            .split(separator: " ")
            .prefix(2)
            .compactMap(\.first)
        return letters.isEmpty ? "?" : String(letters).uppercased()
    }

    var body: some View {
        Circle()
            .fill(Color.paperClosed)
            .overlay(Circle().strokeBorder(Color.hairline, lineWidth: 1))
            .frame(width: size, height: size)
            .overlay {
                if let avatarUserId, let image = session.avatars.images[avatarUserId] {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFill()
                        .frame(width: size, height: size)
                        .clipShape(Circle())
                } else {
                    Text(initials)
                        .font(.system(size: size * 0.38, weight: .semibold, design: .default))
                        .foregroundStyle(Color.ink)
                        .minimumScaleFactor(0.5)
                }
            }
            .task(id: avatarUserId) {
                guard let avatarUserId else { return }
                await session.avatars.load(avatarUserId, api: session.api)
            }
            .accessibilityLabel(user.displayName)
    }
}

#Preview {
    HStack(spacing: 12) {
        UserAvatarView(user: User(id: 1, username: "zagir", displayName: "Загир Нурмухаметов"))
        UserAvatarView(user: User(id: 42, username: nil, displayName: "Алмаз"), size: 56)
        UserAvatarView(user: User(id: 7, username: nil, displayName: ""), size: 32)
    }
    .padding()
    .background(Color.bg)
    .environment(SessionStore())
}
