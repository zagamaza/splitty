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
        design: Font.Design = .default,
        relativeTo style: Font.TextStyle = .body
    ) -> some View {
        modifier(ScaledFontModifier(size: size, weight: weight, design: design, relativeTo: style))
    }
}

// MARK: - Карточка-поверхность

/// Лист бумаги: фон `surface`, почти прямые углы (3pt), без мягкой тени.
/// Светлая тема — едва заметная подложка, тёмная — hairline-рамка, чтобы
/// бумага отделялась от стола.
struct SurfaceCardModifier: ViewModifier {
    var padding: CGFloat
    @Environment(\.colorScheme) private var colorScheme

    @Environment(\.receiptSheetSections) private var inSheet

    @ViewBuilder
    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 3, style: .continuous)
        if inSheet {
            // Секция общего листа: своя бумага и тень не нужны.
            content.padding(padding)
        } else {
        content
            .padding(padding)
            .background(Color.surface, in: shape)
            .overlay {
                if colorScheme == .dark {
                    shape.strokeBorder(Color.hairline, lineWidth: 1)
                }
            }
            .shadow(
                color: colorScheme == .dark ? .clear : Color.black.opacity(0.05),
                radius: 2, x: 0, y: 2
            )
        }
    }
}

extension View {
    /// Оформляет содержимое как лист бумаги на столе `Color.bg`.
    /// Использовать для секций вместо системных Form/List-фонов.
    func surfaceCard(padding: CGFloat = 16) -> some View {
        modifier(SurfaceCardModifier(padding: padding))
    }
}

// MARK: - Кнопки

/// Основное действие: прямоугольник во всю ширину (углы 3pt), высота 54pt,
/// заливка `accent`, текст `onAccent`, pressed — scale 0.98.
struct PrimaryPillButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: 16, weight: .semibold)
            .multilineTextAlignment(.center)
            .foregroundStyle(Color.onAccent)
            .padding(.horizontal, 18)
            .frame(maxWidth: .infinity, minHeight: 54)
            .background(
                (configuration.isPressed ? Color.accentPressed : Color.accent)
                    .opacity(isEnabled ? 1 : 0.45),
                in: RoundedRectangle(cornerRadius: 3, style: .continuous)
            )
            .scaleEffect(!reduceMotion && configuration.isPressed ? 0.98 : 1)
            .animation(.spring(duration: 0.25), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == PrimaryPillButtonStyle {
    /// Основной CTA-стиль («Сохранить», «Записать платёж», «Войти»…).
    static var primaryPill: PrimaryPillButtonStyle { PrimaryPillButtonStyle() }
}

/// Вторичная кнопка-чип: тонкая рамка, углы 3pt; `isSelected` — чернильная
/// рамка и подложка (выбор группы/участника, фильтры).
struct SoftChipButtonStyle: ButtonStyle {
    var isSelected: Bool = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
            // Чип не переносит текст по буквам в тесных строках («Погасить»).
            .lineLimit(1)
            .fixedSize(horizontal: true, vertical: false)
            .foregroundStyle(Color.ink)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(
                Color.ink.opacity(isSelected ? 0.1 : (configuration.isPressed ? 0.08 : 0)),
                in: RoundedRectangle(cornerRadius: 3, style: .continuous)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 3, style: .continuous)
                    .strokeBorder(isSelected ? Color.ink : Color.hairline, lineWidth: 1)
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

/// Сумма по правилам чека: системный шрифт + monospacedDigit,
/// семантическая окраска, `.contentTransition(.numericText())` при изменении.
/// Всегда показывает модуль суммы — знак передаётся цветом/контекстом
/// (единая конвенция проекта; единственная точка цветового правила денег).
struct MoneyText: View {
    /// Семантическая роль суммы (определяет цвет).
    enum Role {
        /// По знаку: `> 0` — positive, `< 0` — negative, `0` — inkSecondary.
        case auto
        /// «Вам должны»/получено — всегда positive.
        case positive
        /// «Вы должны»/долг — всегда negative.
        case negative
        /// Обычная сумма без семантики долга («Всего потрачено») — ink.
        case neutral
    }

    /// Округлённая сумма — она же семантика знака (кому должны).
    let amount: Int
    /// Точная величина в минорных единицах; nil — вызывающий её не знает, и
    /// показывается целое.
    var exactMinorValue: Int?
    var role: Role = .auto
    var size: CGFloat = 17
    var weight: Font.Weight = .semibold
    /// Валюта суммы (код контракта); символ рисуется как у рублей: «1 234 $».
    var currency: String = "RUB"

    /// Точная величина в копейках: переданная, иначе выведенная из целой.
    var exactMinor: Int { exactMinorValue ?? amount * minorFactor }

    init(
        _ amount: Int,
        exactMinor: Int? = nil,
        role: Role = .auto,
        size: CGFloat = 17,
        weight: Font.Weight = .semibold,
        currency: String = "RUB"
    ) {
        self.amount = amount
        self.exactMinorValue = exactMinor
        self.role = role
        self.size = size
        self.weight = weight
        self.currency = currency
    }

    private var color: Color {
        switch role {
        case .positive: return .positive
        case .negative: return .negative
        case .neutral: return .ink
        case .auto:
            // Знак берётся у ТОЧНОЙ величины: у округлённой долг 0,25 читается
            // нулём, и честный плюс красился бы «в расчёте».
            if exactMinor > 0 { return .positive }
            if exactMinor < 0 { return .negative }
            return .inkSecondary
        }
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        // Сумма не переносится никогда: «38 / 700 / ₽» в три строки читается
        // как три числа. При крупном системном шрифте она ужимается.
        Text(money(minor: abs(exactMinor), currency: currency))
            .scaledFont(size: size, weight: weight)
            .monospacedDigit()
            .lineLimit(1)
            .minimumScaleFactor(0.5)
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
                MoneyText(
                    primary.sum, exactMinor: primary.exactMinor,
                    size: primarySize, currency: primary.currency
                )
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
                    Text(verbatim: "·")
                        .font(.system(size: secondarySize, weight: .semibold, design: .default))
                        .foregroundStyle(Color.inkSecondary)
                }
                MoneyText(
                    total.sum, exactMinor: total.exactMinor,
                    size: secondarySize, currency: total.currency
                )
            }
        }
    }
}

// MARK: - Заголовок секции

extension View {
    /// Заголовок секции — печатный реквизит: моноширинный 12pt, вторичный цвет.
    /// Регистр текста НЕ меняет (важно для UI-тестов); капс — у `receiptLabel()`.
    func sectionHeaderStyle() -> some View {
        modifier(SectionHeaderModifier())
    }

    /// Секции экрана настроек — разделами ОДНОГО листа чека: карточки секций
    /// теряют свою бумагу, заголовки получают пунктир сверху и выравнивание
    /// по строкам. Строки внутри секций уже имеют поля 16, поэтому у листа 6.
    func receiptSheetSections() -> some View {
        environment(\.receiptSheetSections, true)
            .receiptPaper(padding: 6)
    }
}

private struct ReceiptSheetSectionsKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    /// true — внутри `receiptSheetSections()`.
    var receiptSheetSections: Bool {
        get { self[ReceiptSheetSectionsKey.self] }
        set { self[ReceiptSheetSectionsKey.self] = newValue }
    }
}

private struct SectionHeaderModifier: ViewModifier {
    @Environment(\.receiptSheetSections) private var inSheet

    func body(content: Content) -> some View {
        let styled = content
            .scaledFont(size: 12, weight: .medium, design: .monospaced, relativeTo: .footnote)
            .foregroundStyle(Color.inkSecondary)
            .kerning(0.3)
        if inSheet {
            // Заголовок стоит с отступом 4 от края секции, строки — 16: здесь
            // добираем до строк, а пунктир отделяет раздел от предыдущего.
            VStack(alignment: .leading, spacing: 14) {
                ReceiptDivider()
                styled.padding(.leading, 12)
            }
            .padding(.top, 4)
        } else {
            styled
        }
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
