import Foundation

/// Единственный разборщик приглашения в группу.
///
/// Кода группы в приложение приходит четырьмя дорогами, и все они должны
/// сходиться сюда, а не в приватное свойство очередного экрана:
/// - universal link `https://<domain>/join/<roomId>` (страница приглашения,
///   `internal/rest/deeplink.go`);
/// - кастомная схема `splitty://join/<roomId>` — кнопка «Открыть в приложении»
///   на той же странице (тап по ссылке НА ТОТ ЖЕ домен iOS в приложение не
///   уводит, поэтому у кнопки своя схема);
/// - легаси-ссылка бота `https://t.me/<bot>?start=room<roomId>` (её всё ещё
///   раздаёт `InviteGroupView` и `internal/bot/all_room.go:77`);
/// - «голый» код, вставленный из буфера.
///
/// Раньше разбор жил приватным свойством внутри `JoinGroupView` и переиспользовать
/// его было нельзя — обработчик диплинка завёл бы вторую копию правил, которая
/// разошлась бы с первой на первой же смене формата ссылки.
enum RoomCodeParser {
    /// Длина кода: комнаты адресуются mongo ObjectID, а это ровно 24 hex-символа
    /// (`primitive.ObjectIDFromHex` в `internal/rest/handlers.go:76`). Проверка
    /// длины здесь — не педантизм: она отличает «пользователь ещё дописывает код»
    /// от «это вообще не код», и экран может сказать об этом словами вместо
    /// молчаливого запроса, который сервер отвергнет 404-м.
    static let codeLength = 24

    /// Маркеры, после которых в строке начинается код. Порядок важен:
    /// `start=room` проверяется первым, иначе ссылка бота с путём `/join/`
    /// в имени бота разобралась бы не с того места.
    private static let markers = ["start=room", "/join/"]

    /// Код группы из произвольного текста: ссылка любого из поддерживаемых
    /// видов или сам код. nil — в строке нет кода нужного формата.
    static func roomId(from raw: String) -> String? {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }

        for marker in markers {
            if let range = text.range(of: marker, options: .caseInsensitive) {
                return code(in: text[range.upperBound...])
            }
        }
        // Легаси-форма «room<hex>»: так код приходил из бота в буфер обмена.
        // Только с начала строки — в середине это уже было бы частью ссылки.
        if let range = text.range(of: "room", options: [.caseInsensitive, .anchored]) {
            return code(in: text[range.upperBound...])
        }
        return code(in: text[...])
    }

    /// То же самое для URL диплинка (`onOpenURL` / `NSUserActivity`).
    static func roomId(from url: URL) -> String? {
        roomId(from: url.absoluteString)
    }

    /// Берёт hex-префикс (обрезая «хвост» ссылки — слеш, `?utm=…`, `#`)
    /// и принимает его, только если это код целиком.
    private static func code(in text: Substring) -> String? {
        let hex = text.prefix(while: isHexDigit)
        guard hex.count == codeLength else { return nil }
        // ObjectID из Go всегда в нижнем регистре; приводим и вставленный
        // вручную, чтобы один и тот же код не выглядел двумя разными.
        return hex.lowercased()
    }

    /// Свой ASCII-предикат вместо `Character.isHexDigit`: последний считает
    /// hex-цифрами и полноширинные формы (`０`…`Ｆ`), а такой «код» ушёл бы
    /// на сервер мусором.
    private static func isHexDigit(_ c: Character) -> Bool {
        ("0"..."9").contains(c) || ("a"..."f").contains(c) || ("A"..."F").contains(c)
    }
}
