import SwiftUI
import UIKit

// MARK: - Hex-инициализаторы

extension UIColor {
    /// Инициализация из hex-значения вида 0x0E9F6E.
    convenience init(hex: UInt32) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: 1
        )
    }
}

extension Color {
    /// Инициализация цвета из hex-значения вида 0x1CC29F.
    /// В экранах напрямую НЕ использовать — только семантические токены ниже.
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }

    /// Адаптивный цвет: значение подбирается по текущей теме (light/dark).
    init(light: UInt32, dark: UInt32) {
        self.init(uiColor: UIColor { trait in
            trait.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light)
        })
    }
}

// MARK: - Семантические токены (премиум-палитра «финтех-минимализм»)

// 90% интерфейса — нейтральные bg/surface/ink; цвет (accent/negative) —
// только смысловой: CTA, позитивные и негативные суммы, активные состояния.
extension Color {
    /// Фон всех экранов (вместо системного).
    static let bg = Color(light: 0xF6F7F9, dark: 0x0C0F13)
    /// Фон карточек и «плавающих» поверхностей (см. `.surfaceCard()`).
    static let surface = Color(light: 0xFFFFFF, dark: 0x171C23)
    /// Основной текст.
    static let ink = Color(light: 0x101828, dark: 0xF2F4F7)
    /// Вторичный текст: подписи, даты, «расчёт», нулевые суммы.
    static let inkSecondary = Color(light: 0x667085, dark: 0x98A2B3)
    /// Акцент (изумруд): CTA, позитивные суммы («вам должны»), активный таб.
    static let accent = Color(light: 0x0E9F6E, dark: 0x34D399)
    /// Негатив (приглушённый коралл): долги, «вы должны».
    static let negative = Color(light: 0xDC5A2E, dark: 0xFB923C)
    /// Тонкие разделители внутри карточек и hairline-бордеры в тёмной теме.
    static let hairline = Color(light: 0xEAECF0, dark: 0x232A33)
    /// Бумага чека: тёплый почти-белый в светлой теме (чтобы карточка-чек
    /// читалась как бумага, а не как обычная surface), чуть светлее surface в тёмной.
    static let receiptPaper = Color(light: 0xFDFCF9, dark: 0x1B2129)
    /// Нажатое состояние акцента (тёмный изумруд): pressed CTA, градиенты.
    static let accentPressed = Color(light: 0x0B7C56, dark: 0x2BB985)
    /// Цвет бренда Telegram — не токен темы, одинаков в обеих.
    static let telegramBlue = Color(light: 0x2AABEE, dark: 0x2AABEE)

    /// Цвета кнопки Google заданы их гайдлайнами — менять нельзя.
    static let googleSurface = Color(light: 0xFFFFFF, dark: 0x131314)
    static let googleBorder = Color(light: 0x747775, dark: 0x8E918F)
    static let googleLabel = Color(light: 0x1F1F1F, dark: 0xE3E3E3)

    /// Заливка баров графиков (дашборд «Итоги»). Отдельный от UI-акцента
    /// цвет данных: в тёмной теме — валидированный для заливок на тёмной
    /// поверхности #0EA97A (UI-accent #34D399 для баров НЕ использовать).
    static let chartAccent = Color(light: 0x0E9F6E, dark: 0x0EA97A)

    /// Категориальная палитра участников дашборда «Итоги»: 6 адаптивных пар
    /// light/dark, валидированных на цветослепоту (CVD) и контраст к поверхностям.
    /// ПОРЯДОК ФИКСИРОВАН и палитра НИКОГДА не циклится: цвет назначается по
    /// user.id ASC (см. `MemberPalette`), 7-й участник и дальше — `inkSecondary`.
    /// Один человек — один и тот же цвет во ВСЕХ графиках дашборда.
    static let chartCategorical: [Color] = [
        Color(light: 0x0E9F6E, dark: 0x0EA97A), // изумруд
        Color(light: 0xD97706, dark: 0xC77D08), // янтарь
        Color(light: 0x2F6FE4, dark: 0x4478DB), // синий
        Color(light: 0xDB2777, dark: 0xC94E7F), // розовый
        Color(light: 0x0891B2, dark: 0x0E8FA8), // бирюза
        Color(light: 0x8B5CF6, dark: 0x8E6BE0), // фиолет
    ]
}

// MARK: - Легаси-алиасы (палитра Splitwise)

// На них ссылаются ещё не мигрированные экраны — проект обязан компилироваться.
// В новом коде использовать семантические токены выше.
extension Color {
    /// Легаси: брендовый зелёный → семантический акцент.
    static let swGreen = Color.accent
    /// Легаси: тёмный зелёный (нажатые состояния, градиенты) → accentPressed.
    static let swGreenDark = Color.accentPressed
    /// Легаси: оранжевый «вы должны» → negative.
    static let swOrange = Color.negative
    /// Легаси: серый вторичный текст → inkSecondary.
    static let swGrayText = Color.inkSecondary
    /// Легаси: основной тёмный текст → ink.
    static let swDark = Color.ink
}

// MARK: - Легаси-стиль кнопки

/// Легаси-стиль основной кнопки («Записать платёж» и т.п.):
/// теперь отрисовывается как премиум-CTA (см. `PrimaryPillButtonStyle`).
struct SWPrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        PrimaryPillButtonStyle().makeBody(configuration: configuration)
    }
}

extension ButtonStyle where Self == SWPrimaryButtonStyle {
    /// Стиль основной кнопки Splitty (легаси-имя, новый вид).
    static var swPrimary: SWPrimaryButtonStyle { SWPrimaryButtonStyle() }
}
