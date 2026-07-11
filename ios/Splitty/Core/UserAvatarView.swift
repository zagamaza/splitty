import SwiftUI

/// Аватар пользователя: круг с мягким градиентом и белыми инициалами (rounded).
/// Пастельная пара цветов детерминирована от `user.id`,
/// инициалы — первые буквы слов `displayName`.
struct UserAvatarView: View {
    let user: User
    var size: CGFloat = 40

    /// Пастельные пары градиентов (светлый → чуть глубже), подобраны так,
    /// чтобы белые инициалы читались; индекс выбирается по id пользователя.
    private static let gradients: [(top: UInt32, bottom: UInt32)] = [
        (0x5EC9A7, 0x3AA98F), // мята
        (0x7FA8EC, 0x5B87D8), // голубой
        (0xA78BDB, 0x8B6FC5), // лаванда
        (0xE58BB0, 0xD16A96), // розовый
        (0xEDA06E, 0xDD8452), // персик
        (0xD9B45B, 0xC49A3C), // песочный
        (0x6BBBDF, 0x4A9EC7), // небо
        (0xE58B7E, 0xD16C5D), // коралл
        (0x9DBE6C, 0x82A650), // оливковый
        (0x8E9DBB, 0x7183A6), // серо-синий
    ]

    /// Индекс палитры: биты id перемешиваются (SplitMix64) — иначе «круглые»
    /// соседние id (100, 200, 300…) дают один и тот же градиент.
    private var gradientIndex: Int {
        var x = UInt64(bitPattern: Int64(user.id))
        x ^= x >> 30
        x = x &* 0xBF58_476D_1CE4_E5B9
        x ^= x >> 27
        x = x &* 0x94D0_49BB_1331_11EB
        x ^= x >> 31
        return Int(x % UInt64(Self.gradients.count))
    }

    private var gradient: LinearGradient {
        let pair = Self.gradients[gradientIndex]
        return LinearGradient(
            colors: [Color(hex: pair.top), Color(hex: pair.bottom)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )
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
            .fill(gradient)
            .frame(width: size, height: size)
            .overlay {
                Text(initials)
                    .font(.system(size: size * 0.4, weight: .semibold, design: .rounded))
                    .foregroundStyle(.white)
                    .minimumScaleFactor(0.5)
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
}
