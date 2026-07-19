import SwiftUI
import UIKit

// MARK: - Масштабируемый шрифт (Dynamic Type)

/// Системный шрифт, масштабируемый настройкой размера текста: `size` —
/// базовый размер при стандартной настройке, растёт/уменьшается вместе с
/// `relativeTo`. Жёсткий `.font(.system(size:))` Dynamic Type игнорирует —
/// использовать этот модификатор для всех кастомных размеров.
private struct ScaledFontModifier: ViewModifier {
    @ScaledMetric private var size: CGFloat
    private let weight: Font.Weight
    private let design: Font.Design

    init(size: CGFloat, weight: Font.Weight, design: Font.Design, relativeTo style: Font.TextStyle) {
        _size = ScaledMetric(wrappedValue: size, relativeTo: style)
        self.weight = weight
        self.design = design
    }

    func body(content: Content) -> some View {
        content.font(.system(size: size, weight: weight, design: design))
    }
}

extension View {
    /// Шрифт с поддержкой Dynamic Type (см. `ScaledFontModifier`).
    func scaledFont(
        size: CGFloat,
        weight: Font.Weight = .regular,
        design: Font.Design = .rounded,
        relativeTo style: Font.TextStyle = .body
    ) -> some View {
        modifier(ScaledFontModifier(size: size, weight: weight, design: design, relativeTo: style))
    }
}

// MARK: - Карточка-поверхность

/// Премиум-карточка: фон `surface`, скругление 20pt (continuous);
/// светлая тема — мягкая тень, тёмная — hairline-бордер без тени.
struct SurfaceCardModifier: ViewModifier {
    var padding: CGFloat
    @Environment(\.colorScheme) private var colorScheme

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
        content
            .padding(padding)
            .background(Color.surface, in: shape)
            .overlay {
                if colorScheme == .dark {
                    shape.strokeBorder(Color.hairline, lineWidth: 1)
                }
            }
            .shadow(
                color: colorScheme == .dark ? .clear : Color.black.opacity(0.06),
                radius: 14, x: 0, y: 6
            )
    }
}

extension View {
    /// Оформляет содержимое как мягкую карточку на фоне `Color.bg`.
    /// Использовать для секций-«карточек» вместо системных Form/List-фонов.
    func surfaceCard(padding: CGFloat = 16) -> some View {
        modifier(SurfaceCardModifier(padding: padding))
    }
}

// MARK: - Кнопки

/// Основной CTA: pill во всю ширину, высота 54pt, фон `accent`,
/// белый semibold-текст, pressed — scale 0.98 со spring-анимацией.
struct PrimaryPillButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: 17, weight: .semibold)
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity, minHeight: 54)
            .background(
                (configuration.isPressed ? Color.accentPressed : Color.accent)
                    .opacity(isEnabled ? 1 : 0.45),
                in: Capsule()
            )
            .scaleEffect(!reduceMotion && configuration.isPressed ? 0.98 : 1)
            .animation(.spring(duration: 0.25), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == PrimaryPillButtonStyle {
    /// Основной CTA-стиль («Сохранить», «Записать платёж», «Войти»…).
    static var primaryPill: PrimaryPillButtonStyle { PrimaryPillButtonStyle() }
}

/// Вторичная кнопка-чип: мягкая серая pill; `isSelected` — акцентная заливка
/// (для выбора группы/участника в чипах).
struct SoftChipButtonStyle: ButtonStyle {
    var isSelected: Bool = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
            // Чип не переносит текст по буквам в тесных строках («Погасить»).
            .lineLimit(1)
            .fixedSize(horizontal: true, vertical: false)
            .foregroundStyle(isSelected ? Color.accent : Color.ink)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(
                isSelected
                    ? Color.accent.opacity(0.14)
                    : Color.ink.opacity(configuration.isPressed ? 0.12 : 0.06),
                in: Capsule()
            )
            .scaleEffect(!reduceMotion && configuration.isPressed ? 0.98 : 1)
            .animation(.spring(duration: 0.25), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == SoftChipButtonStyle {
    /// Вторичная кнопка-чип («Балансы», «Итоги», фильтры…).
    static var softChip: SoftChipButtonStyle { SoftChipButtonStyle() }
    /// Чип с выбранным состоянием (акцентная заливка).
    static func softChip(isSelected: Bool) -> SoftChipButtonStyle {
        SoftChipButtonStyle(isSelected: isSelected)
    }
}

// MARK: - Деньги

/// Сумма в рублях по правилам премиум-дизайна: rounded + monospacedDigit,
/// семантическая окраска, `.contentTransition(.numericText())` при изменении.
/// Всегда показывает модуль суммы — знак передаётся цветом/контекстом
/// (единая конвенция проекта; единственная точка цветового правила денег).
struct MoneyText: View {
    /// Семантическая роль суммы (определяет цвет).
    enum Role {
        /// По знаку: `> 0` — accent, `< 0` — negative, `0` — inkSecondary.
        case auto
        /// «Вам должны»/получено — всегда accent.
        case positive
        /// «Вы должны»/долг — всегда negative.
        case negative
        /// Обычная сумма без семантики долга («Всего потрачено») — ink.
        case neutral
    }

    let amount: Int
    var role: Role = .auto
    var size: CGFloat = 17
    var weight: Font.Weight = .semibold
    /// Валюта суммы (код контракта); символ рисуется как у рублей: «1 234 $».
    var currency: String = "RUB"

    init(
        _ amount: Int,
        role: Role = .auto,
        size: CGFloat = 17,
        weight: Font.Weight = .semibold,
        currency: String = "RUB"
    ) {
        self.amount = amount
        self.role = role
        self.size = size
        self.weight = weight
        self.currency = currency
    }

    private var color: Color {
        switch role {
        case .positive: return .accent
        case .negative: return .negative
        case .neutral: return .ink
        case .auto:
            if amount > 0 { return .accent }
            if amount < 0 { return .negative }
            return .inkSecondary
        }
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Text(money(abs(amount), currency: currency))
            .scaledFont(size: size, weight: weight)
            .monospacedDigit()
            .foregroundStyle(color)
            .contentTransition(reduceMotion ? .identity : .numericText(value: Double(amount)))
            .animation(.spring(duration: 0.35), value: amount)
    }
}

// MARK: - Суммы в нескольких валютах

/// Итог, где могут встретиться РАЗНЫЕ валюты (общий баланс, нетто друга):
/// суммы не складываются между валютами — основная валюта (наибольший |суммы|)
/// крупно, остальные — вторичной строкой мельче. `totals` — уже агрегированные
/// `aggregateByCurrency` (пустой список — «0 ₽» серым: полный расчёт).
struct MoneyTotalsText: View {
    let totals: [CurrencySum]
    var primarySize: CGFloat = 40
    var secondarySize: CGFloat = 15
    var alignment: HorizontalAlignment = .leading

    var body: some View {
        VStack(alignment: alignment, spacing: 4) {
            if let primary = totals.first {
                MoneyText(primary.sum, size: primarySize, currency: primary.currency)
            } else {
                MoneyText(0, size: primarySize)
            }
            if totals.count > 1 {
                secondaryLine
            }
        }
    }

    /// Остальные валюты одной тихой строкой: «+120 $ · −3 400 Rp» мельче.
    private var secondaryLine: some View {
        HStack(spacing: 6) {
            ForEach(Array(totals.dropFirst().enumerated()), id: \.element) { index, total in
                if index > 0 {
                    Text("·")
                        .font(.system(size: secondarySize, weight: .semibold, design: .rounded))
                        .foregroundStyle(Color.inkSecondary)
                }
                MoneyText(total.sum, size: secondarySize, currency: total.currency)
            }
        }
    }
}

// MARK: - Заголовок секции

extension View {
    /// Стиль заголовка секции: 13pt semibold rounded, вторичный цвет.
    /// Регистр текста НЕ меняет (важно для UI-тестов).
    func sectionHeaderStyle() -> some View {
        self
            .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
            .foregroundStyle(Color.inkSecondary)
            .kerning(0.5)
    }
}

// MARK: - Haptics

/// Хелперы тактильного отклика. Генераторы кэшированы: создание
/// UIFeedbackGenerator на каждое нажатие задерживало первый кадр после касания.
@MainActor
enum Haptics {
    private static let notificationGenerator = UINotificationFeedbackGenerator()
    private static let impactGenerator: UIImpactFeedbackGenerator = {
        let generator = UIImpactFeedbackGenerator(style: .light)
        generator.prepare()
        return generator
    }()

    /// Успешное сохранение/платёж/создание.
    static func success() {
        notificationGenerator.notificationOccurred(.success)
    }

    /// Лёгкий отклик на выбор/переключение (чипы, radio, чекбоксы).
    static func tap() {
        impactGenerator.impactOccurred()
        impactGenerator.prepare()
    }

    /// Действие недоступно/требует внимания (тап по заблокированной кнопке).
    static func warning() {
        notificationGenerator.notificationOccurred(.warning)
    }
}

// MARK: - Preview

#Preview("Дизайн-система") {
    ScrollView {
        VStack(spacing: 20) {
            VStack(alignment: .leading, spacing: 12) {
                Text("Общий баланс").sectionHeaderStyle()
                MoneyText(15200, size: 40)
                HStack {
                    MoneyText(-4300)
                    MoneyText(0)
                    MoneyText(990, role: .neutral)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .surfaceCard(padding: 20)

            Button("Записать платёж") {}
                .buttonStyle(.primaryPill)

            HStack {
                Button("Балансы") {}.buttonStyle(.softChip)
                Button("Итоги") {}.buttonStyle(.softChip(isSelected: true))
            }
        }
        .padding(20)
    }
    .background(Color.bg)
}
