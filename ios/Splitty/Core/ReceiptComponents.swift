import SwiftUI
import UIKit

// Элементы «Общего чека»: бумага с перфорированным краем, пунктир, строка
// позиции, итог, штамп состояния, текстовая ссылка, реквизит.
// Референс: docs/design/receipt/receipt/*.html.

// MARK: - Системная обвязка

/// Навбары и таббар в палитре чека: стол вместо системного фона, тонкая черта,
/// заголовок по центру — моноширинный реквизит, крупный — плотный гротеск.
enum ReceiptChrome {
    @MainActor
    static func configure() {
        let table = UIColor(Color.bg)
        let ink = UIColor(Color.ink)
        let secondary = UIColor(Color.inkSecondary)
        let line = UIColor(Color.hairline)

        let nav = UINavigationBarAppearance()
        nav.configureWithOpaqueBackground()
        nav.backgroundColor = table
        nav.shadowColor = .clear
        nav.titleTextAttributes = [
            .foregroundColor: ink,
            .font: UIFontMetrics(forTextStyle: .footnote)
                .scaledFont(for: .monospacedSystemFont(ofSize: 13, weight: .medium)),
            .kern: 0.6,
        ]
        nav.largeTitleTextAttributes = [
            .foregroundColor: ink,
            .font: UIFontMetrics(forTextStyle: .largeTitle)
                .scaledFont(for: .systemFont(ofSize: 34, weight: .bold)),
            .kern: -1.2,
        ]
        UINavigationBar.appearance().standardAppearance = nav
        UINavigationBar.appearance().scrollEdgeAppearance = nav
        UINavigationBar.appearance().compactAppearance = nav

        let tab = UITabBarAppearance()
        tab.configureWithOpaqueBackground()
        tab.backgroundColor = table
        tab.shadowColor = line
        for item in [tab.stackedLayoutAppearance, tab.inlineLayoutAppearance, tab.compactInlineLayoutAppearance] {
            item.normal.iconColor = secondary
            item.normal.titleTextAttributes = [.foregroundColor: secondary]
            item.selected.iconColor = ink
            item.selected.titleTextAttributes = [
                .foregroundColor: ink,
                .font: UIFont.systemFont(ofSize: 10, weight: .bold),
            ]
        }
        UITabBar.appearance().standardAppearance = tab
        UITabBar.appearance().scrollEdgeAppearance = tab
    }
}

// MARK: - Реквизит

/// Можно ли печатать реквизит капсом с разрядкой. У японского, китайского и
/// корейского регистра нет, а разрядка рвёт иероглифы — им остаётся обычный
/// моноширинный текст.
enum ReceiptCaps {
    static func allowsCaps(languageCode: String?) -> Bool {
        guard let code = languageCode?.lowercased() else { return true }
        return !["ja", "zh", "ko"].contains(code)
    }
}

private struct ReceiptLabelModifier: ViewModifier {
    var size: CGFloat
    @Environment(\.locale) private var locale

    func body(content: Content) -> some View {
        let caps = ReceiptCaps.allowsCaps(languageCode: locale.language.languageCode?.identifier)
        content
            .scaledFont(size: size, weight: .medium, design: .monospaced, relativeTo: .caption2)
            .textCase(caps ? .uppercase : nil)
            .kerning(caps ? 1 : 0)
            .foregroundStyle(Color.inkSecondary)
    }
}

extension View {
    /// Печатный реквизит чека: «ПОЗИЦИЯ / КТО ПЛАТИЛ», «ВАШ ОСТАТОК», даты.
    func receiptLabel(size: CGFloat = 11) -> some View {
        modifier(ReceiptLabelModifier(size: size))
    }
}

// MARK: - Бумага

/// Нижний край чека: прямоугольник, у которого низ — мелкая пила, как у
/// оторванной ленты. Высота зубца — `tooth`.
struct PerforatedEdgeShape: Shape {
    var tooth: CGFloat = 6

    func path(in rect: CGRect) -> Path {
        var path = Path()
        let bottom = rect.maxY - tooth
        path.move(to: CGPoint(x: rect.minX, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.maxX, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.maxX, y: bottom))
        let step = tooth * 2
        var x = rect.maxX
        var down = true
        while x > rect.minX {
            let next = max(rect.minX, x - step / 2)
            path.addLine(to: CGPoint(x: next, y: down ? rect.maxY : bottom))
            down.toggle()
            x = next
        }
        path.addLine(to: CGPoint(x: rect.minX, y: bottom))
        path.closeSubpath()
        return path
    }
}

private struct ReceiptPaperModifier: ViewModifier {
    var padding: CGFloat
    var perforated: Bool
    var paper: Color
    @Environment(\.colorScheme) private var colorScheme

    func body(content: Content) -> some View {
        let tooth: CGFloat = perforated ? 6 : 0
        content
            .padding(padding)
            .padding(.bottom, tooth)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                PerforatedEdgeShape(tooth: tooth)
                    .fill(paper)
                    .shadow(
                        color: colorScheme == .dark ? .clear : Color.black.opacity(0.06),
                        radius: 3, x: 0, y: 3
                    )
                    .overlay {
                        if colorScheme == .dark {
                            PerforatedEdgeShape(tooth: tooth).stroke(Color.hairline, lineWidth: 1)
                        }
                    }
                    .accessibilityHidden(true)
            }
    }
}

extension View {
    /// Лист чека: бумага во всю ширину, поля `padding`, перфорированный низ.
    /// Край — фон, в дерево доступности не попадает.
    func receiptPaper(padding: CGFloat = 22, perforated: Bool = true, paper: Color = .surface) -> some View {
        modifier(ReceiptPaperModifier(padding: padding, perforated: perforated, paper: paper))
    }
}

/// Корешок счёта в списке: бумага с пунктиром снизу и полукруглыми
/// вырезами по нижним углам — отрывная часть ленты.
struct ReceiptStubShape: Shape {
    var notch: CGFloat = 7

    func path(in rect: CGRect) -> Path {
        var path = Path(rect)
        path.addEllipse(in: CGRect(x: rect.minX - notch, y: rect.maxY - notch, width: notch * 2, height: notch * 2))
        path.addEllipse(in: CGRect(x: rect.maxX - notch, y: rect.maxY - notch, width: notch * 2, height: notch * 2))
        return path
    }
}

extension View {
    /// Корешок счёта: `closed` — бумага закрытого счёта.
    func receiptStub(closed: Bool = false, padding: CGFloat = 20) -> some View {
        self
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                ZStack(alignment: .bottom) {
                    // Чётно-нечётная заливка вырезает круги из бумаги, а обрезка
                    // по прямоугольнику убирает их половины, торчащие наружу.
                    ReceiptStubShape()
                        .fill(closed ? Color.paperClosed : Color.surface, style: FillStyle(eoFill: true))
                        .clipShape(Rectangle())
                    ReceiptDivider(style: .dashed).padding(.horizontal, 8)
                }
                .accessibilityHidden(true)
            }
    }
}

// MARK: - Линии

/// Разделитель внутри чека.
struct ReceiptDivider: View {
    enum Style { case dashed, total, hair }
    var style: Style = .dashed

    var body: some View {
        switch style {
        case .dashed:
            Line()
                .stroke(Color.dash, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                .frame(height: 1)
                .accessibilityHidden(true)
        case .total:
            Rectangle().fill(Color.ruleStrong).frame(height: 2).accessibilityHidden(true)
        case .hair:
            Rectangle().fill(Color.hairline).frame(height: 1).accessibilityHidden(true)
        }
    }

    private struct Line: Shape {
        func path(in rect: CGRect) -> Path {
            var path = Path()
            path.move(to: CGPoint(x: rect.minX, y: rect.midY))
            path.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
            return path
        }
    }
}

// MARK: - Строки

/// Позиция чека: слева название и мелкая подпись, справа сумма.
struct ReceiptLine<Trailing: View>: View {
    let title: String
    var caption: String?
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .scaledFont(size: 15, weight: .regular)
                    .foregroundStyle(Color.ink)
                    .fixedSize(horizontal: false, vertical: true)
                if let caption, !caption.isEmpty {
                    Text(caption)
                        .scaledFont(size: 12, relativeTo: .caption)
                        .foregroundStyle(Color.inkSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            trailing()
        }
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
    }
}

/// Итог чека: жирная черта, реквизит слева, крупная сумма справа.
struct ReceiptTotal<Amount: View>: View {
    let label: String
    @ViewBuilder var amount: () -> Amount

    var body: some View {
        VStack(spacing: 14) {
            ReceiptDivider(style: .total)
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(label).receiptLabel()
                Spacer(minLength: 8)
                amount()
            }
            .accessibilityElement(children: .combine)
        }
    }
}

// MARK: - Сводка по валютам

/// Сводный остаток по валютам: каждая валюта — своей строкой со своей
/// подписью. Одна подпись по знаку первой валюты рядом с модулями всех сумм
/// говорила «вам должны» и про валюту, в которой должны вы.
struct BalanceSummaryLines: View {
    /// Ненулевые итоги (`aggregateByCurrency`), основная валюта первой.
    let totals: [CurrencySum]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(totals, id: \.self) { total in
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline) {
                        caption(total)
                        Spacer(minLength: 8)
                        amount(total)
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        caption(total)
                        amount(total)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private func caption(_ total: CurrencySum) -> some View {
        Text(total.exactMinor > 0 ? String(localized: "Вам должны") : String(localized: "Вы должны"))
            .scaledFont(size: 17, weight: .semibold)
            .foregroundStyle(Color.ink)
    }

    private func amount(_ total: CurrencySum) -> some View {
        MoneyText(total.sum, exactMinor: total.exactMinor, size: 22, currency: total.currency)
    }
}

// MARK: - Штамп

/// Синий штамп истинного состояния («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК»). Рамка и
/// поворот — декор; текст читается один раз как обычная подпись.
struct StatusStamp: View {
    let text: String
    var angle: Double = -6

    var body: some View {
        Text(text)
            .scaledFont(size: 12, weight: .semibold, design: .monospaced, relativeTo: .caption)
            .kerning(1)
            .multilineTextAlignment(.center)
            .foregroundStyle(Color.stamp)
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .overlay(Rectangle().strokeBorder(Color.stamp, lineWidth: 2).accessibilityHidden(true))
            .rotationEffect(.degrees(angle))
            .padding(.vertical, 6)
    }
}

// MARK: - Текстовая ссылка

/// Вторичное действие чека: подчёркнутый текст («Все балансы», «Рассчитаться ↗»).
struct TextLinkButtonStyle: ButtonStyle {
    var size: CGFloat = 14
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: size, weight: .medium)
            .underline(true, color: Color.ink.opacity(0.6))
            .foregroundStyle(Color.ink.opacity(isEnabled ? (configuration.isPressed ? 0.6 : 1) : 0.4))
            .frame(minHeight: 44)
            .contentShape(Rectangle())
    }
}

extension ButtonStyle where Self == TextLinkButtonStyle {
    static var textLink: TextLinkButtonStyle { TextLinkButtonStyle() }
    static func textLink(size: CGFloat) -> TextLinkButtonStyle { TextLinkButtonStyle(size: size) }
}

// MARK: - Вкладка

/// Текстовая вкладка документа: выбранная — чернилами с чертой снизу.
struct ReceiptTab: View {
    let title: LocalizedStringKey
    let isOn: Bool
    var size: CGFloat = 14

    var body: some View {
        Text(title)
            .scaledFont(size: size, weight: isOn ? .semibold : .regular)
            .foregroundStyle(isOn ? Color.ink : Color.inkSecondary)
            .lineLimit(1)
            .fixedSize()
            .padding(.vertical, 10)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(isOn ? Color.ink : .clear)
                    .frame(height: 1.5)
                    .accessibilityHidden(true)
            }
            .contentShape(Rectangle())
    }
}
