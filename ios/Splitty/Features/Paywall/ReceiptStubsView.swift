import SwiftUI

/// Счётчик суточного лимита — пять отрывных корешков чека.
///
/// Сигнатурный элемент экрана оплаты и единственное место, где он позволяет
/// себе быть заметным. Смысл не в украшении: корешок — это ровно одно
/// распознавание, израсходованные оторваны по перфорации, и сколько осталось
/// видно без чтения цифр. Материал взят из мира приложения (`receiptPaper` —
/// та же «бумага чека», что у карточки расхода), а не придуман для этого экрана.
struct ReceiptStubsView: View {
    let limit: Int
    let used: Int

    private var remaining: Int { max(0, limit - used) }

    var body: some View {
        VStack(spacing: 10) {
            HStack(spacing: 6) {
                ForEach(0..<max(limit, 1), id: \.self) { index in
                    ReceiptStub(isTornOff: index < used)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Осталось \(remaining) из \(limit) распознаваний"))

            Text(remaining == 0
                 ? String(localized: "Распознавания на сегодня закончились")
                 : String(localized: "Осталось \(remaining) из \(limit)"))
                .scaledFont(size: 14, weight: .medium, relativeTo: .subheadline)
                .foregroundStyle(Color.inkSecondary)
        }
    }
}

/// Один корешок. Оторванный — с рваным краем и без заливки.
private struct ReceiptStub: View {
    let isTornOff: Bool

    var body: some View {
        TornEdgeShape(tornAtBottom: isTornOff)
            .fill(isTornOff ? Color.ink.opacity(0.07) : Color.receiptPaper)
            .overlay {
                TornEdgeShape(tornAtBottom: isTornOff)
                    .strokeBorder(
                        isTornOff ? Color.hairline : Color.accent.opacity(0.35),
                        lineWidth: 1
                    )
            }
            .frame(width: 26, height: 40)
            .opacity(isTornOff ? 0.55 : 1)
    }
}

/// Прямоугольник, у которого нижний край либо ровный, либо оторванный.
///
/// Рваный край рисуется зигзагом с фиксированным шагом, а не случайным: экран
/// перерисовывается на каждое изменение остатка, и «дрожащая» бумага читалась
/// бы как дефект, а не как приём.
private struct TornEdgeShape: InsettableShape {
    var tornAtBottom: Bool
    var insetAmount: CGFloat = 0

    func inset(by amount: CGFloat) -> TornEdgeShape {
        var copy = self
        copy.insetAmount += amount
        return copy
    }

    func path(in rect: CGRect) -> Path {
        let r = rect.insetBy(dx: insetAmount, dy: insetAmount)
        let corner: CGFloat = 4
        let tearHeight: CGFloat = 5
        let tearStep: CGFloat = 5

        var path = Path()
        path.move(to: CGPoint(x: r.minX, y: r.minY + corner))
        path.addQuadCurve(
            to: CGPoint(x: r.minX + corner, y: r.minY),
            control: CGPoint(x: r.minX, y: r.minY)
        )
        path.addLine(to: CGPoint(x: r.maxX - corner, y: r.minY))
        path.addQuadCurve(
            to: CGPoint(x: r.maxX, y: r.minY + corner),
            control: CGPoint(x: r.maxX, y: r.minY)
        )

        guard tornAtBottom else {
            path.addLine(to: CGPoint(x: r.maxX, y: r.maxY - corner))
            path.addQuadCurve(
                to: CGPoint(x: r.maxX - corner, y: r.maxY),
                control: CGPoint(x: r.maxX, y: r.maxY)
            )
            path.addLine(to: CGPoint(x: r.minX + corner, y: r.maxY))
            path.addQuadCurve(
                to: CGPoint(x: r.minX, y: r.maxY - corner),
                control: CGPoint(x: r.minX, y: r.maxY)
            )
            path.closeSubpath()
            return path
        }

        path.addLine(to: CGPoint(x: r.maxX, y: r.maxY - tearHeight))
        var x = r.maxX
        var up = false
        while x > r.minX {
            let nextX = max(r.minX, x - tearStep)
            let y = up ? r.maxY - tearHeight : r.maxY
            path.addLine(to: CGPoint(x: nextX, y: y))
            up.toggle()
            x = nextX
        }
        path.addLine(to: CGPoint(x: r.minX, y: r.minY + corner))
        path.closeSubpath()
        return path
    }
}

#Preview("Корешки") {
    VStack(spacing: 28) {
        ReceiptStubsView(limit: 5, used: 0)
        ReceiptStubsView(limit: 5, used: 3)
        ReceiptStubsView(limit: 5, used: 5)
    }
    .padding()
    .background(Color.bg)
}
