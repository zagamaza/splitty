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

// MARK: - Заголовок вкладки

/// Заголовок корня вкладки строкой контента. Системный large title на iOS 26
/// пропадал: в тёмной теме, после возврата из стека — место под ним
/// оставалось, а самого названия не было.
struct ScreenHeading: View {
    let title: LocalizedStringKey

    init(_ title: LocalizedStringKey) {
        self.title = title
    }

    var body: some View {
        Text(title)
            .scaledFont(size: 34, weight: .bold, relativeTo: .largeTitle)
            .kerning(-1.2)
            .foregroundStyle(Color.ink)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
            .collapsingTitleSource()
    }
}

/// Состояние без прокрутки (загрузка, ошибка) под тем же заголовком.
struct HeadedState<Content: View>: View {
    let title: LocalizedStringKey
    @ViewBuilder let content: Content

    init(_ title: LocalizedStringKey, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(spacing: 0) {
            ScreenHeading(title)
                .padding(.horizontal, 20)
                .padding(.top, 4)
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Color.bg)
    }
}

extension View {
    /// Заголовок листа с кнопками по обе стороны: системный обрезал длинный
    /// («Добавить в груп…») — этот в том же стиле ужимается, а не режется.
    func receiptSheetTitle(_ title: LocalizedStringKey) -> some View {
        navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text(title)
                        .scaledFont(size: 13, weight: .medium, design: .monospaced, relativeTo: .footnote)
                        .kerning(0.6)
                        .foregroundStyle(Color.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                        .accessibilityAddTraits(.isHeader)
                }
            }
    }
}

extension View {
    /// Корень вкладки: название остаётся системным для стека и VoiceOver,
    /// крупно рисуется `ScreenHeading` в контенте, а в навбар название
    /// приходит, когда крупный заголовок уехал под него (как large title).
    func receiptRootTitle(_ title: LocalizedStringKey) -> some View {
        navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .modifier(CollapsingTitleModifier(title: Text(title), placeholder: nil, forceCollapsed: false))
    }

    /// Экран с крупным заголовком в контенте (`collapsingTitleSource`): пока он
    /// виден, навбар показывает `placeholder` (реквизит вроде «Группа») или
    /// ничего; уехал под навбар — там остаётся `title`.
    /// `forceCollapsed` — крупного заголовка на экране сейчас нет вовсе
    /// (другая вкладка тусы): название показывается сразу.
    func collapsingTitle(_ title: Text, placeholder: Text? = nil, forceCollapsed: Bool = false) -> some View {
        modifier(CollapsingTitleModifier(title: title, placeholder: placeholder, forceCollapsed: forceCollapsed))
    }

    /// Крупный заголовок, по которому навбар решает, показывать ли название.
    func collapsingTitleSource() -> some View {
        background {
            GeometryReader { proxy in
                Color.clear.preference(key: CollapsingTitleBottomKey.self, value: proxy.frame(in: .global).maxY)
            }
        }
    }
}

private struct CollapsingTitleBottomKey: PreferenceKey {
    static var defaultValue: CGFloat? = nil
    static func reduce(value: inout CGFloat?, nextValue: () -> CGFloat?) {
        value = nextValue() ?? value
    }
}

/// Название в навбаре появляется, когда низ крупного заголовка ушёл выше
/// верха контента. Заголовок в ленивом списке, прокрученный далеко, выгружается
/// и перестаёт сообщать положение — тогда держится последнее известное, то
/// есть «уехал».
private struct CollapsingTitleModifier: ViewModifier {
    let title: Text
    let placeholder: Text?
    let forceCollapsed: Bool
    @State private var headingBottom: CGFloat?
    @State private var contentTop: CGFloat = 0

    private var isCollapsed: Bool {
        if forceCollapsed { return true }
        guard let headingBottom else { return false }
        return headingBottom < contentTop + 2
    }

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minY } action: { contentTop = $0 }
            .onPreferenceChange(CollapsingTitleBottomKey.self) { value in
                if let value { headingBottom = value }
            }
            .toolbar {
                ToolbarItem(placement: .principal) {
                    ZStack {
                        placeholder?.opacity(isCollapsed ? 0 : 1)
                        title.opacity(isCollapsed ? 1 : 0)
                    }
                    .scaledFont(size: 13, weight: .medium, design: .monospaced, relativeTo: .footnote)
                    .kerning(0.6)
                    .foregroundStyle(Color.ink)
                    .lineLimit(1)
                    .animation(.easeOut(duration: 0.18), value: isCollapsed)
                    // Название экрана VoiceOver берёт из navigationTitle, а
                    // крупный заголовок в контенте — заголовок раздела.
                    .accessibilityHidden(true)
                }
            }
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

// MARK: - Поле суммы

/// Крупная сумма в поле ввода, которая всегда помещается в лист целиком:
/// кегль уменьшается под число знаков и реальную ширину, а на крупном шрифте
/// символ валюты уходит над числом, чтобы не отнимать у него строку.
/// Внутренний скролл поля не годится — он прячет часть суммы, которую
/// человек подтверждает.
struct AmountEntryRow<Field: View>: View {
    let symbol: String
    let text: String
    let size: CGFloat
    let weight: Font.Weight
    @ViewBuilder let field: (Font) -> Field

    @ScaledMetric private var scaledSize: CGFloat
    @ScaledMetric(relativeTo: .title) private var symbolSize: CGFloat = 28
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var width: CGFloat = 0

    init(symbol: String, text: String, size: CGFloat, weight: Font.Weight, @ViewBuilder field: @escaping (Font) -> Field) {
        self.symbol = symbol
        self.text = text
        self.size = size
        self.weight = weight
        self.field = field
        _scaledSize = ScaledMetric(wrappedValue: size, relativeTo: .title)
    }

    var body: some View {
        let stacked = typeSize.isAccessibilitySize
        Group {
            if stacked {
                VStack(spacing: 2) {
                    symbolText
                    field(.system(size: fittedSize(stacked: true), weight: weight).monospacedDigit())
                }
            } else {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    symbolText
                    field(.system(size: fittedSize(stacked: false), weight: weight).monospacedDigit())
                }
            }
        }
        .frame(maxWidth: .infinity)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
    }

    private var symbolText: some View {
        Text(symbol)
            .font(.system(size: symbolSize, weight: .medium))
            .foregroundStyle(Color.inkSecondary)
    }

    /// Кегль из настоящей ширины набранной строки: если при базовом кегле
    /// она не влезает, кегль уменьшается ровно настолько. Пока ширина не
    /// измерена — базовый кегль.
    private func fittedSize(stacked: Bool) -> CGFloat {
        guard width > 0 else { return scaledSize }
        let symbolWidth = stacked ? 0 : textWidth(symbol, font: .systemFont(ofSize: symbolSize, weight: .medium)) + 8
        let available = width - symbolWidth - 8
        let natural = textWidth(text.isEmpty ? "0" : text, font: .monospacedDigitSystemFont(ofSize: scaledSize, weight: uiWeight))
        guard natural > available else { return scaledSize }
        return max(12, scaledSize * available / natural)
    }

    private func textWidth(_ string: String, font: UIFont) -> CGFloat {
        ceil((string as NSString).size(withAttributes: [.font: font]).width)
    }

    private var uiWeight: UIFont.Weight {
        switch weight {
        case .heavy: .heavy
        case .bold: .bold
        case .semibold: .semibold
        case .medium: .medium
        default: .regular
        }
    }
}

// MARK: - Образец разбора

/// Сказанная фраза и чек, в который она превращается: пустая форма расхода
/// показывает его как образец, Plus — как то, за что платят. Только
/// содержимое: фон и рамку задаёт место, где образец стоит.
struct VoiceExampleReceipt: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "mic.fill")
                    .scaledFont(size: 13)
                    .foregroundStyle(Color.ink)
                Text("«Ужин 3200, делим на четверых»")
                    .scaledFont(size: 15, weight: .medium, relativeTo: .subheadline)
                    .foregroundStyle(Color.ink)
            }
            ReceiptDivider()
            HStack {
                Text("Ужин")
                    .scaledFont(size: 16, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Spacer()
                Text("3 200 ₽")
                    .scaledFont(size: 16, weight: .semibold)
                    .foregroundStyle(Color.ink)
                    .monospacedDigit()
            }
            HStack {
                Text("Поровну на четверых")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                Spacer()
                Text("по 800 ₽")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.ink)
                    .monospacedDigit()
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text("Сказанная фраза превращается в готовый расход, разделённый на четверых"))
    }
}

// MARK: - Штамп

/// Синий штамп истинного состояния («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК»). Рамка и
/// поворот — декор; текст читается один раз как обычная подпись.
struct StatusStamp: View {
    let text: String
    var angle: Double = -6
    @Environment(\.locale) private var locale

    var body: some View {
        let caps = ReceiptCaps.allowsCaps(languageCode: locale.language.languageCode?.identifier)
        Text(text)
            .scaledFont(size: 12, weight: .semibold, design: caps ? .monospaced : .default, relativeTo: .caption)
            .kerning(caps ? 1 : 0)
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

// MARK: - Действие рамкой

/// Второе по силе действие листа («Рассчитаться»): чернильная рамка во всю
/// ширину. Заливка остаётся одной кнопке экрана.
struct ReceiptOutlineButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaledFont(size: 16, weight: .semibold)
            .foregroundStyle(Color.ink)
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity, minHeight: 48)
            .padding(.horizontal, 12)
            .background(Color.ink.opacity(configuration.isPressed ? 0.06 : 0))
            .overlay(
                RoundedRectangle(cornerRadius: 3, style: .continuous)
                    .strokeBorder(Color.ink, lineWidth: 1)
            )
            .contentShape(Rectangle())
            .opacity(isEnabled ? 1 : 0.4)
    }
}

extension ButtonStyle where Self == ReceiptOutlineButtonStyle {
    static var receiptOutline: ReceiptOutlineButtonStyle { ReceiptOutlineButtonStyle() }
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
