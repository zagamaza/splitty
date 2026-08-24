#if DEBUG
import Foundation

/// Заготовка «человек уже говорит» для витринного кадра.
///
/// Экран надиктовки нельзя снять честно: у симулятора нет микрофона, распознавать
/// нечего, и в кадр попадал композер в покое — а продаёт нас именно момент, когда
/// фраза на глазах превращается в текст. Поэтому оверлей записи поднимается с
/// готовым транскриптом и стартом четырнадцатью секундами раньше: кольцо лимита
/// уже прошло четверть, таймер идёт, слова набраны наполовину.
///
/// Живёт только в Debug и только по переменной запуска — в релизной сборке этого
/// кода нет вообще.
struct DemoRecording {
    let transcript: String
    let startedAt: Date

    /// Секунд «уже сказано» к моменту снимка. Отсчёт стартует на первой
    /// отрисовке композера, а до самого кадра тест ещё выбирает группу и ждёт
    /// анимации — эти семь секунд и дают на снимке около четырнадцати.
    private static let elapsed: TimeInterval = 7

    private static let ru = """
    Ужин в Кадыкёе. Дорада на гриле три двести — я и Марина. \
    Мидии по-измирски тысяча четыреста пятьдесят, это Марина с Кириллом
    """

    private static let en = """
    Dinner in Alfama. Grilled sea bass thirty two euros — me and Marina. \
    Mussels fourteen fifty, that's Marina and Kirill
    """

    /// Стартовое время считается один раз: пересчёт на каждой перерисовке
    /// обнулял бы таймер и кольцо прямо в момент съёмки.
    private static let stored: DemoRecording? = {
        guard ProcessInfo.processInfo.environment["SPLITTY_DEMO_RECORDING"] != nil else {
            return nil
        }
        let isRussian = Locale.current.language.languageCode?.identifier == "ru"
        return DemoRecording(
            transcript: isRussian ? ru : en,
            startedAt: Date().addingTimeInterval(-elapsed)
        )
    }()

    static var fromEnvironment: DemoRecording? { stored }
}
#endif
