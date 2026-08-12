import SwiftUI
import XCTest
@testable import Splitty

/// Контраст текста в светлой теме.
///
/// Акцентный изумруд на белом даёт 3.39:1. Крупной сумме и заливке кнопки этого
/// достаточно, а подписи в 12–15 пунктов — нет: минимум 4.5:1. Разница видна не
/// всем и не всегда: на солнце и на дешёвой матрице такая подпись пропадает.
final class ContrastTests: XCTestCase {

    private func luminance(_ hex: Int) -> Double {
        func channel(_ raw: Int) -> Double {
            let v = Double(raw) / 255
            return v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel((hex >> 16) & 0xFF)
            + 0.7152 * channel((hex >> 8) & 0xFF)
            + 0.0722 * channel(hex & 0xFF)
    }

    private func ratio(_ a: Int, _ b: Int) -> Double {
        let la = luminance(a), lb = luminance(b)
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    /// Значения светлой темы из `Theme.swift` — держим их здесь явно: тест
    /// обязан заметить, если кто-то осветлит токен обратно.
    private let white = 0xFFFFFF
    private let accentText = 0x0A6E4C
    private let negativeText = 0xAF3F1C
    private let accent = 0x0E9F6E

    func testTextTokensPassAA() {
        XCTAssertGreaterThanOrEqual(
            ratio(accentText, white), 4.5,
            "акцентный текст не проходит по контрасту — мелкая подпись нечитаема"
        )
        XCTAssertGreaterThanOrEqual(
            ratio(negativeText, white), 4.5,
            "негативный текст не проходит по контрасту — мелкая подпись нечитаема"
        )
    }

    /// Крупные суммы и заливка кнопки остаются на прежнем акценте: смысл токена
    /// в том, чтобы поменять цвет ТОЛЬКО там, где кегль мелкий.
    func testPlainAccentStaysAsItWas() {
        XCTAssertLessThan(ratio(accent, white), 4.5, "цвет акцента изменился — проверьте, что это осознанно")
    }
}
