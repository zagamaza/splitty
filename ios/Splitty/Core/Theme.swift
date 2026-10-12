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

// MARK: - Семантические токены («Общий чек»)

// Туса — общий чек: тёплый «стол» `bg`, на нём бумага `surface`, чернила `ink`.
// Цвет — только смысловой: оранжевое действие (`accent`), синий штамп
// состояния (`stamp`), знак денег (`positive`/`negative`). Направление долга
// всегда говорится словами, цвет его лишь подкрепляет.
extension Color {
    /// Стол под чеком — фон всех экранов.
    static let bg = Color(light: 0xDEDBD2, dark: 0x1A1B16)
    /// Бумага: поверхность документа, карточек и листов.
    static let surface = Color(light: 0xFFFDF4, dark: 0x26271F)
    /// Бумага закрытого счёта (архив, неактивное).
    static let paperClosed = Color(light: 0xE9E8DD, dark: 0x202119)
    /// Основной текст — чернила.
    static let ink = Color(light: 0x262722, dark: 0xECEADF)
    /// Вторичный текст: реквизиты, даты, подписи, нулевые суммы.
    static let inkSecondary = Color(light: 0x5E5E54, dark: 0xA7A595)
    /// Действие: заливка основной кнопки, активные элементы управления.
    /// Текст на заливке — `onAccent`.
    static let accent = Color(light: 0xC4502D, dark: 0xC4502D)
    /// Текст и значки поверх `accent`.
    static let onAccent = Color(light: 0xFFFDF4, dark: 0xFFFDF4)
    /// Негатив: «вы должны», ошибки.
    static let negative = Color(light: 0xA23B2A, dark: 0xF08A6E)
    /// Позитив: «вам должны», «получено».
    static let positive = Color(light: 0x2E6B45, dark: 0x8CCB9F)
    /// Тонкие линии: разделители строк, рамки полей.
    static let hairline = Color(light: 0xCBC9BC, dark: 0x3A3B32)
    /// Пунктир между частями чека.
    static let dash = Color(light: 0xAAA999, dark: 0x5A5B50)
    /// Жирная итоговая черта над «ВСЕГО».
    static let ruleStrong = Color(light: 0x2A2D24, dark: 0xECEADF)
    /// Синий штамп состояния («ВЫ В РАСЧЁТЕ», «ЧЕРНОВИК»).
    static let stamp = Color(light: 0x36579C, dark: 0x8FA9E0)
    /// Бумага чека — та же, что `surface`: весь документ одна поверхность.
    static let receiptPaper = Color.surface
    /// Нажатое состояние действия.
    static let accentPressed = Color(light: 0xA6421F, dark: 0xA6421F)
    /// Действие ДЛЯ ТЕКСТА мелким кеглем: на столе и бумаге ≥ 4.5:1.
    static let accentText = Color(light: 0x9E3F1D, dark: 0xE57A55)
    /// Негатив ДЛЯ ТЕКСТА мелким кеглем.
    static let negativeText = Color(light: 0xA23B2A, dark: 0xF08A6E)
    /// Подложка записи голоса: тёмная лента под белым текстом расшифровки.
    static let recordingScrim = Color(light: 0x262722, dark: 0x0F100C)
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
