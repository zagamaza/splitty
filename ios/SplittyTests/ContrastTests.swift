import SwiftUI
import XCTest
@testable import Splitty

/// Контраст текста «Общего чека» в обеих темах.
///
/// Текст лежит либо на столе (`bg`), либо на бумаге (`surface`), и на обоих
/// мелкая подпись обязана давать 4.5:1. Стол темнее бумаги, поэтому проверка
/// только по бумаге пропустила бы серую подпись, нечитаемую на фоне экрана.
/// Цвета берутся из самих токенов, разрешённых под тему, а не переписываются
/// сюда числами.
final class ContrastTests: XCTestCase {

    private func luminance(_ color: Color, _ style: UIUserInterfaceStyle) -> Double {
        let resolved = UIColor(color).resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        resolved.getRed(&r, green: &g, blue: &b, alpha: &a)
        func channel(_ v: CGFloat) -> Double {
            let v = Double(v)
            return v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
    }

    private func ratio(_ a: Color, _ b: Color, _ style: UIUserInterfaceStyle) -> Double {
        let la = luminance(a, style), lb = luminance(b, style)
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    private let styles: [UIUserInterfaceStyle] = [.light, .dark]

    func testSmallTextPassesAAOnTableAndPaper() {
        let texts: [(String, Color)] = [
            ("ink", .ink), ("inkSecondary", .inkSecondary), ("accentText", .accentText),
            ("negativeText", .negativeText), ("positive", .positive), ("stamp", .stamp),
        ]
        for style in styles {
            for (name, text) in texts {
                for (bgName, bg) in [("bg", Color.bg), ("surface", Color.surface)] {
                    let r = ratio(text, bg, style)
                    XCTAssertGreaterThanOrEqual(
                        r, 4.5, "\(style == .dark ? "тёмная" : "светлая"): \(name) на \(bgName) даёт \(r):1"
                    )
                }
            }
        }
    }

    func testButtonLabelPassesAAOnActionFill() {
        for style in styles {
            XCTAssertGreaterThanOrEqual(ratio(.onAccent, .accent, style), 4.5,
                                        "подпись основной кнопки нечитаема")
        }
    }
}
