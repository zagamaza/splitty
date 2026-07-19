import SwiftUI

/// Чек-карточка: перфорированные края, пунктирные разделители строк, шапка
/// «Позиции · N», моноширинные суммы, стек аватарок участников с бейджами веса
/// (×N) и замком фикс-суммы, подсказка деления у каждой строки, подвал
/// Подытог → Сборы → Итого. Единый вид для детали операции (read-only) и
/// формы добавления (передай колбэки — строки и правило сбора станут тапабельными,
/// нераспознанные имена — красными чипами на сопоставление).
struct ReceiptView: View {
    let items: [OperationItem]
    let members: [User]
    let currency: String
    /// Тап по строке позиции (индекс в исходном `items`); nil — read-only.
    var onEditItem: ((Int) -> Void)? = nil
    /// Тап по чипу нераспознанного имени (индекс позиции, имя); nil — read-only.
    var onResolveUnknown: ((Int, String) -> Void)? = nil
    /// Тап по правилу деления сбора (индекс в исходном `items`); nil — read-only.
    var onToggleSurchargeRule: ((Int) -> Void)? = nil
    /// Индексы позиций, изменённых последней голосовой правкой — мягкая
    /// акцентная подсветка строки (гасится вью-моделью по таймеру).
    var highlightedIndices: Set<Int> = []

    /// (индекс в исходном массиве, позиция) — колбэки работают по исходным индексам.
    private var indexed: [(index: Int, item: OperationItem)] {
        items.enumerated().map { ($0.offset, $0.element) }
    }
    private var itemsOnly: [(index: Int, item: OperationItem)] {
        indexed.filter { !$0.item.isSurcharge }
    }
    private var surcharges: [(index: Int, item: OperationItem)] {
        indexed.filter { $0.item.isSurcharge }
    }
    private var subtotal: Int { itemsOnly.reduce(0) { $0 + $1.item.price } }
    private var surchargeTotal: Int { surcharges.reduce(0) { $0 + $1.item.price } }
    /// Есть позиции без цены — итог чека неполный (рисуем «Итого ≥»).
    private var hasPriceless: Bool { itemsOnly.contains { $0.item.price < 1 } }

    var body: some View {
        VStack(spacing: 0) {
            Perforation(edge: .top)
            VStack(spacing: 0) {
                header
                rowDivider
                ForEach(itemsOnly, id: \.index) { entry in
                    if entry.index != itemsOnly.first?.index { rowDivider }
                    itemRow(index: entry.index, item: entry.item)
                }
                if !surcharges.isEmpty {
                    rowDivider
                    footerLine("Подытог", subtotal, emphasis: .subtotal)
                    ForEach(surcharges, id: \.index) { entry in
                        surchargeRow(index: entry.index, item: entry.item)
                    }
                }
                totalRule
                // При позициях без цены итог неполный — «≥» честно говорит,
                // что число вырастет, когда цены будут указаны.
                footerLine(hasPriceless ? "Итого ≥" : "Итого",
                           subtotal + surchargeTotal, emphasis: .total)
            }
            .padding(.horizontal, 18)
            .padding(.top, 10)
            .padding(.bottom, 10)
            .background(Color.receiptPaper)
            Perforation(edge: .bottom)
        }
        .compositingGroup()
        .shadow(color: Color.black.opacity(0.07), radius: 16, x: 0, y: 8)
    }

    // MARK: header

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            Text("Позиции")
                .font(.system(size: 11, weight: .bold))
                .tracking(1.4)
                .textCase(.uppercase)
                .foregroundStyle(Color.ink.opacity(0.6))
            Spacer()
            Text("\(itemsOnly.count) поз.")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(Color.ink.opacity(0.6))
        }
        .padding(.bottom, 12)
    }

    // MARK: rows

    private func itemRow(index: Int, item: OperationItem) -> some View {
        let row = VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(item.name.isEmpty ? "Позиция" : item.name)
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(Color.ink)
                    .multilineTextAlignment(.leading)
                if item.qty > 1 {
                    Text("×\(item.qty)")
                        .font(.system(size: 12.5, weight: .regular, design: .monospaced))
                        .foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 8)
                if item.price < 1 {
                    // Цена не определена (модель услышала блюдо, но не цену):
                    // метка ведёт в шит позиции — там поле цены.
                    Text("цена?")
                        .font(.system(size: 11.5, weight: .bold, design: .rounded))
                        .foregroundStyle(Color.negative)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(Color.negative.opacity(0.1), in: Capsule())
                        .overlay(
                            Capsule().strokeBorder(
                                Color.negative,
                                style: StrokeStyle(lineWidth: 1, dash: [3, 3])
                            )
                        )
                        .modifier(SoftPulse(active: onEditItem != nil))
                } else {
                    Text(money(item.price, currency: currency))
                        .font(.system(size: 15, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Color.ink)
                }
            }
            HStack(spacing: 8) {
                avatarStack(index: index, item: item)
                Spacer(minLength: 8)
                Text(shareHint(item))
                    .font(.system(size: 11.5, design: .monospaced))
                    .foregroundStyle(Color.ink.opacity(0.6))
            }
        }
        .padding(.vertical, 13)
        .padding(.horizontal, highlightedIndices.contains(index) ? 8 : 0)
        .background(
            highlightedIndices.contains(index) ? Color.accent.opacity(0.12) : Color.clear,
            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
        )
        .padding(.horizontal, highlightedIndices.contains(index) ? -8 : 0)
        .animation(.easeOut(duration: 0.4), value: highlightedIndices)
        .contentShape(Rectangle())

        return Group {
            if let onEditItem {
                Button { onEditItem(index) } label: { row }
                    .buttonStyle(.plain)
            } else {
                row
            }
        }
    }

    private func avatarStack(index: Int, item: OperationItem) -> some View {
        let even = isEven(item)
        // С бейджами (веса/фиксы) аватарки не внахлёст — иначе бейдж
        // перекрывается соседней аватаркой и вес не читается.
        let hasBadges = !even || item.shareList.contains { $0.amount != nil }
        return HStack(spacing: 6) {
            HStack(spacing: hasBadges ? 3 : -7) {
                ForEach(Array(item.shareList.enumerated()), id: \.offset) { _, share in
                    ZStack(alignment: .bottomTrailing) {
                        avatar(share.userId)
                        if share.amount != nil {
                            badge(accent: true) {
                                Image(systemName: "lock.fill").font(.system(size: 7, weight: .bold))
                            }
                        } else if !even {
                            badge {
                                Text("\(share.weight)")
                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                            }
                        }
                    }
                }
            }
            ForEach(Array((item.unknown ?? []).enumerated()), id: \.offset) { _, name in
                unknownChip(index: index, name: name)
            }
        }
    }

    /// Красный пунктирный чип нераспознанного имени: «Саня ?». В интерактивном
    /// режиме тап открывает сопоставление участнику; мягко пульсирует, чтобы
    /// глаз сам нашёл, что требует ответа.
    private func unknownChip(index: Int, name: String) -> some View {
        let chip = HStack(spacing: 3) {
            Text(name)
            Text("?")
        }
        .font(.system(size: 11.5, weight: .bold, design: .rounded))
        .foregroundStyle(Color.negative)
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(Color.negative.opacity(0.1), in: Capsule())
        .overlay(
            Capsule().strokeBorder(
                Color.negative,
                style: StrokeStyle(lineWidth: 1, dash: [3, 3])
            )
        )
        .modifier(SoftPulse(active: onResolveUnknown != nil))

        return Group {
            if let onResolveUnknown {
                Button { onResolveUnknown(index, name) } label: { chip }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Кто это — \(name)?")
            } else {
                chip
            }
        }
    }

    @ViewBuilder private func avatar(_ id: Int) -> some View {
        if let u = members.first(where: { $0.id == id }) {
            UserAvatarView(user: u, size: 26)
                .overlay(Circle().strokeBorder(Color.receiptPaper, lineWidth: 2))
        } else {
            Circle().fill(Color.inkSecondary.opacity(0.25)).frame(width: 26, height: 26)
        }
    }

    private func badge<Content: View>(
        accent: Bool = false,
        @ViewBuilder _ content: () -> Content
    ) -> some View {
        content()
            .foregroundStyle(.white)
            .frame(width: 15, height: 15)
            .background(accent ? Color.accent : Color.ink, in: Circle())
            .overlay(Circle().strokeBorder(Color.receiptPaper, lineWidth: 1.5))
            .offset(x: 3, y: 2)
    }

    // MARK: surcharges

    private func surchargeRow(index: Int, item: OperationItem) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(item.name.isEmpty ? "Сбор" : item.name)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(Color.ink)
                if let p = item.percent {
                    Text("\(p)%")
                        .font(.system(size: 12.5, design: .monospaced))
                        .foregroundStyle(Color.ink.opacity(0.6))
                }
                Spacer(minLength: 8)
                Text(money(item.price, currency: currency))
                    .font(.system(size: 15, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Color.ink)
            }
            surchargeRule(index: index, item: item)
        }
        .padding(.vertical, 9)
    }

    /// Правило деления сбора. Интерактивно — чип-переключатель
    /// «⇄ Пропорционально съеденному / Поровну на всех»; read-only — тихая подпись.
    @ViewBuilder
    private func surchargeRule(index: Int, item: OperationItem) -> some View {
        let equally = item.split == OperationItem.splitEqually
        if let onToggleSurchargeRule {
            Button { onToggleSurchargeRule(index) } label: {
                HStack(spacing: 6) {
                    Image(systemName: "arrow.left.arrow.right")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Color.inkSecondary)
                    Group {
                        if equally {
                            Text("Поровну").fontWeight(.bold) + Text(" на всех")
                        } else {
                            Text("Пропорционально").fontWeight(.bold) + Text(" съеденному")
                        }
                    }
                    .font(.system(size: 11.5, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.ink)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Color.ink.opacity(0.05), in: Capsule())
                .overlay(Capsule().strokeBorder(Color.ink.opacity(0.12), lineWidth: 1))
                .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(equally ? "Сбор делится поровну. Сменить" : "Сбор делится пропорционально. Сменить")
        } else {
            Text(equally ? "делится поровну" : "делится пропорционально съеденному")
                .font(.system(size: 11.5, design: .monospaced))
                .foregroundStyle(Color.ink.opacity(0.6))
        }
    }

    // MARK: footer

    private func footerLine(_ title: String, _ amount: Int, emphasis: Emphasis) -> some View {
        HStack {
            Text(title)
                .font(.system(size: 12, weight: emphasis == .total ? .bold : .semibold))
                .tracking(emphasis == .total ? 1.2 : 0.4)
                .textCase(emphasis == .total ? .uppercase : nil)
                .foregroundStyle(emphasis == .total ? Color.ink : Color.inkSecondary)
            Spacer()
            Text(money(amount, currency: currency))
                .font(.system(size: emphasis == .total ? 19 : 14,
                              weight: emphasis == .total ? .bold : .semibold, design: .monospaced))
                .foregroundStyle(Color.ink)
        }
        .padding(.vertical, emphasis == .total ? 12 : 7)
    }

    private enum Emphasis { case subtotal, total }

    // MARK: separators

    private var rowDivider: some View {
        DashedRule().stroke(Color.ink.opacity(0.14), style: StrokeStyle(lineWidth: 1, dash: [3, 4]))
            .frame(height: 1)
    }
    private var totalRule: some View {
        Rectangle().fill(Color.ink).frame(height: 2).padding(.top, 8)
    }

    // MARK: helpers

    private func isEven(_ item: OperationItem) -> Bool {
        let ws = item.shareList.filter { $0.amount == nil }.map(\.weight)
        guard let f = ws.first else { return true }
        return !ws.contains { $0 != f }
    }

    private func shareHint(_ item: OperationItem) -> String {
        if item.hasUnknown { return "кто это — выберите" }
        let n = item.shareList.count
        if item.price < 1 { return n > 0 ? "укажите цену" : "" }
        if n == 0 { return "" }
        if n == 1 { return "целиком" }
        if item.shareList.contains(where: { $0.amount != nil }) {
            let fixed = item.shareList.reduce(0) { $0 + ($1.amount ?? 0) }
            let weighted = item.shareList.filter { $0.amount == nil }.count
            if weighted > 0 { return "\(money(fixed, currency: currency)) фиксом · остальное поровну" }
            return "точные суммы"
        }
        if isEven(item) {
            return "по \(perPersonText(item.price, parts: n)) × \(n)"
        }
        let units = item.shareList.reduce(0) { $0 + $1.weight }
        return "\(units) шт · \(perPersonText(item.price, parts: units)) за шт"
    }

    /// «По сколько с носа»: при неделящейся нацело цене — честный диапазон
    /// «33–34 ₽» (раньше «по 33 ₽ × 3» не сходилось с итогом строки 100 ₽).
    private func perPersonText(_ price: Int, parts: Int) -> String {
        let n = max(1, parts)
        let base = price / n
        guard price % n != 0 else { return money(base, currency: currency) }
        return "\(base)–\(money(base + 1, currency: currency))"
    }
}

/// Мягкая пульсация (прозрачность 1 → 0.55) для элементов, требующих ответа.
/// Уважает reduce motion; `active: false` — статичный вид (read-only чек).
private struct SoftPulse: ViewModifier {
    let active: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dimmed = false

    func body(content: Content) -> some View {
        content
            .opacity(dimmed ? 0.55 : 1)
            .onAppear {
                guard active, !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 1).repeatForever(autoreverses: true)) {
                    dimmed = true
                }
            }
    }
}

/// Горизонтальная линия для пунктирных/сплошных разделителей чека.
private struct DashedRule: Shape {
    func path(in rect: CGRect) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: 0, y: rect.midY))
        p.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
        return p
    }
}

/// Перфорированный (зубчатый) край чека: полукруги цвета страницы, надрезающие
/// бумагу. Полоса — цвет бумаги, вырезы — фон под карточкой.
private struct Perforation: View {
    enum Edge { case top, bottom }
    let edge: Edge
    var body: some View {
        let d: CGFloat = 11
        GeometryReader { geo in
            let count = max(1, Int((geo.size.width / d).rounded()))
            HStack(spacing: 0) {
                ForEach(0..<count, id: \.self) { _ in
                    Circle().fill(Color.bg).frame(width: d, height: d)
                }
            }
            .frame(width: geo.size.width, alignment: .center)
            .offset(y: edge == .top ? -d / 2 : d / 2)
        }
        .frame(height: d / 2)
        .background(Color.receiptPaper)
        .clipped()
    }
}

// MARK: - «С кого сколько»

/// Разбивка itemized-черновика по людям: аватар, имя, тонкий бар доли от
/// максимума, сумма и «+N ₽ сбор». Подвал — «Сумма распределена полностью»,
/// когда доли сходятся с чеком до рубля (клиентское зеркало серверного расчёта).
struct PersonBreakdownCard: View {
    let shares: [PersonShare]
    let members: [User]
    let currency: String
    var meId: Int? = nil

    private var maxTotal: Int { max(shares.map(\.total).max() ?? 1, 1) }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("С кого сколько")
                .sectionHeaderStyle()
                .padding(.bottom, 6)
            ForEach(Array(shares.enumerated()), id: \.element.id) { index, share in
                if index > 0 {
                    Rectangle().fill(Color.hairline).frame(height: 1)
                }
                row(share)
            }
            balancedLine
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private func row(_ share: PersonShare) -> some View {
        HStack(spacing: 12) {
            if let user = members.first(where: { $0.id == share.userId }) {
                UserAvatarView(user: user, size: 36)
            } else {
                Circle().fill(Color.inkSecondary.opacity(0.25)).frame(width: 36, height: 36)
            }
            VStack(alignment: .leading, spacing: 7) {
                Text(name(share.userId))
                    .font(.system(size: 15, design: .rounded))
                    .foregroundStyle(Color.ink)
                    .lineLimit(1)
                bar(fraction: Double(share.total) / Double(maxTotal))
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 3) {
                MoneyText(share.total, role: .neutral, size: 15, currency: currency)
                if share.surchargePart > 0 {
                    Text("+\(money(share.surchargePart, currency: currency)) сбор")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(Color.inkSecondary)
                }
            }
        }
        .padding(.vertical, 9)
    }

    /// Тонкий бар доли участника (относительно наибольшей доли).
    private func bar(fraction: Double) -> some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.ink.opacity(0.06))
                Capsule().fill(Color.accent)
                    .frame(width: max(geo.size.width * fraction, 4))
            }
        }
        .frame(height: 3)
    }

    private var balancedLine: some View {
        HStack(spacing: 6) {
            Spacer(minLength: 0)
            Image(systemName: "checkmark")
                .font(.system(size: 11, weight: .bold))
            Text("Сумма распределена полностью")
                .font(.system(size: 12.5, weight: .semibold, design: .rounded))
        }
        .foregroundStyle(Color.accent)
        .padding(.top, 10)
        .overlay(alignment: .top) {
            Rectangle().fill(Color.hairline).frame(height: 1)
        }
        .padding(.top, 6)
    }

    private func name(_ id: Int) -> String {
        guard let user = members.first(where: { $0.id == id }) else { return "…" }
        return id == meId ? "\(user.displayName) (вы)" : user.displayName
    }
}
