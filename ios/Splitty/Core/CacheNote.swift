import SwiftUI

/// Подпись о свежести показанных данных.
///
/// Признак «данные из кеша» вычислялся на всех экранах, но выводился только в
/// списке групп: на карточке группы, друзьях и активности человек смотрел на
/// старые суммы, ничего об этом не зная, — и «неправильный» баланс выглядел
/// ошибкой расчёта, а не отсутствием связи. Текст один на все экраны: правило
/// одно, и узнавать его в разных формулировках человек не должен.
func cacheNoteText(updatedAt: Date?) -> String {
    guard let updatedAt else {
        // Кеш есть, а времени обновления нет — данные пришли из прошлого
        // запуска, и врать «обновлялись только что» нельзя.
        return String(localized: "Данные сохранённые: связи с сервером нет")
    }
    return String(localized: "Данные сохранённые, обновлялись \(DateFmt.relative(updatedAt))")
}

/// Строка подписи с иконкой отсутствия связи; ничего не рисует на свежих данных.
struct CacheNote: View {
    let isFromCache: Bool
    let updatedAt: Date?

    var body: some View {
        if isFromCache {
            Label {
                Text(cacheNoteText(updatedAt: updatedAt))
            } icon: {
                Image(systemName: "wifi.slash")
            }
            .scaledFont(size: 12.5, relativeTo: .footnote)
            .foregroundStyle(Color.inkSecondary)
        }
    }
}
