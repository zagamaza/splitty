import XCTest
@testable import Splitty

/// Русский текст, который уедет на экран мимо каталога.
///
/// На iOS русский — исходный язык, и ключ каталога это сама русская строка.
/// Поэтому литерал в позиции `LocalizedStringKey` работает: Xcode вытащит его в
/// каталог, а `LocalizationCatalogTests` уронит сборку, пока перевода нет.
/// Опасны ДРУГИЕ позиции: там, где тип объявлен как `String`, каталог не
/// участвует вовсе, и строка остаётся русской на любом языке. Заметить это
/// можно только глазами, переключив язык устройства.
///
/// Проверки грубые нарочно: любой кириллический литерал — повод либо провести
/// его через каталог, либо внести в [allowed] с объяснением, почему человек его
/// не увидит. Список исключений — это и есть список известного долга.
final class HardcodedTextTests: XCTestCase {

    /// Строки, которые на экран не попадают: демо-данные превью и диагностика.
    private static let allowed: [String: String] = [
        "Загир Нурмухаметов": "демо-данные #Preview в UserAvatarView",
        "Алмаз": "демо-данные #Preview в UserAvatarView",
        "Стамбул": "демо-данные #Preview в FriendDetailView",
        "Дача": "демо-данные #Preview в FriendDetailView",
        "Бали": "демо-данные #Preview в FriendDetailView",
        "Дизайн-система": "имя #Preview: видно в Xcode, не в приложении",
        "Некорректная дата RFC3339: \u{1}": "debugDescription DecodingError — уходит в лог, не на экран",
    ]

    /// Метка на месте интерполяции: `\(sum)` в литерале превращается в
    /// подстановку каталога, и сравнивать надо уже с ней.
    private static let hole = "\u{1}"

    private static let sourcesURL = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()   // SplittyTests
        .deletingLastPathComponent()   // ios
        .appendingPathComponent("Splitty")

    private static let catalogURL = sourcesURL.appendingPathComponent("Localizable.xcstrings")

    // MARK: Разбор исходников

    /// Строковые литералы куска исходника вместе с номером строки, на которой
    /// литерал начался.
    ///
    /// Разбираем ЦЕЛИКОМ, а не построчно: интерполяция переносится на
    /// следующую строку («Активна до \(дата)» занимает две), и построчный
    /// разбор давал обрывки, по которым каталог не найти. Интерполяции
    /// схлопываются в [hole] — внутри `\(name ?? "")` есть свои кавычки, и
    /// наивный разбор оборвал бы литерал на них.
    private func literals(in text: String) -> [(line: Int, text: String)] {
        var out: [(Int, String)] = []
        var current = ""
        var startLine = 1
        var line = 1
        var inString = false
        var interpolation = 0
        var i = text.startIndex

        while i < text.endIndex {
            let c = text[i]
            if c == "\n" { line += 1 }

            if interpolation > 0 {
                if c == "(" { interpolation += 1 }
                if c == ")" {
                    interpolation -= 1
                    if interpolation == 0 { current += Self.hole }
                }
                i = text.index(after: i)
                continue
            }
            if inString {
                if c == "\\" {
                    let next = text.index(after: i)
                    guard next < text.endIndex else { break }
                    if text[next] == "(" {
                        interpolation = 1
                        i = text.index(after: next)
                        continue
                    }
                    switch text[next] {
                    case "n": current.append("\n")
                    case "t": current.append("\t")
                    case "\"": current.append("\"")
                    case "\\": current.append("\\")
                    default: current.append(text[next])
                    }
                    i = text.index(after: next)
                    continue
                }
                if c == "\"" || c == "\n" {
                    // Незакрытая кавычка до конца строки — не литерал, а разметка
                    // (например, кавычка внутри комментария). Такой кусок бросаем.
                    if c == "\"" { out.append((startLine, current)) }
                    inString = false
                    current = ""
                    i = text.index(after: i)
                    continue
                }
                current.append(c)
            } else if c == "\"" {
                inString = true
                startLine = line
                current = ""
            }
            i = text.index(after: i)
        }
        return out.map { (line: $0.0, text: $0.1) }
    }

    /// Строки-комментарии стираются, но НЕ схлопываются: номера строк должны
    /// остаться прежними, иначе отчёт указывает не туда.
    private func withoutComments(_ text: String) -> String {
        text.components(separatedBy: "\n")
            .map { isComment($0) ? "" : $0 }
            .joined(separator: "\n")
    }

    private func hasCyrillic(_ s: String) -> Bool {
        s.unicodeScalars.contains { ("\u{0410}"..."\u{044F}").contains($0) || $0 == "\u{0401}" || $0 == "\u{0451}" }
    }

    private func isComment(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.hasPrefix("//") || t.hasPrefix("*") || t.hasPrefix("/*")
    }

    private func swiftFiles() throws -> [URL] {
        let e = FileManager.default.enumerator(at: Self.sourcesURL, includingPropertiesForKeys: nil)
        let files = (e?.allObjects as? [URL] ?? []).filter { $0.pathExtension == "swift" }
        XCTAssertFalse(files.isEmpty, "не нашли исходники в \(Self.sourcesURL.path)")
        return files
    }

    private func catalogKeys() throws -> Set<String> {
        let data = try Data(contentsOf: Self.catalogURL)
        let json = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let strings = try XCTUnwrap(json["strings"] as? [String: Any])
        return Set(strings.keys)
    }

    /// Ключ каталога в виде литерала: подстановки становятся [hole], чтобы
    /// «Убрать %@ из группы?» и `"Убрать \(name) из группы?"` сошлись.
    private func asLiteral(_ key: String) -> String {
        key.replacingOccurrences(
            of: "%(\\d+\\$)?(@|lld|ld|d|f|\\.\\df)",
            with: Self.hole,
            options: .regularExpression
        )
    }

    // MARK: Проверки

    /// Литерал, которого нет в каталоге, переводу не подлежит вовсе: ни один
    /// перевод к нему не привязан, и на любом языке он останется русским.
    func testCyrillicLiteralsAreCatalogKeys() throws {
        let known = Set(try catalogKeys().map(asLiteral))
        var found: [String] = []

        for file in try swiftFiles() {
            let text = withoutComments(try String(contentsOf: file, encoding: .utf8))
            for literal in literals(in: text) where hasCyrillic(literal.text) {
                if known.contains(literal.text) || Self.allowed[literal.text] != nil { continue }
                found.append("\(file.lastPathComponent):\(literal.line): «\(literal.text)»")
            }
        }
        XCTAssertTrue(
            found.isEmpty,
            "русский текст мимо каталога — на других языках он останется русским. "
                + "Проведи его через каталог или внеси в allowed с объяснением:\n"
                + found.sorted().joined(separator: "\n")
        )
    }

    /// Позиция, объявленная как `String`, каталог не зовёт. Ровно так пункт меню
    /// «Создать группу» и заголовок «Выйти из «…»?» оставались русскими,
    /// хотя ключи для них в каталоге ЛЕЖАЛИ и были переведены.
    func testStringTypedDeclarationsGoThroughCatalog() throws {
        let declaration = try NSRegularExpression(pattern: "->\\s*String\\b|:\\s*String\\s*[{=]")
        var found: [String] = []

        for file in try swiftFiles() {
            let text = withoutComments(try String(contentsOf: file, encoding: .utf8))
            let lines = text.components(separatedBy: "\n")

            // Строки кода внутри объявлений типа String: там каталог не работает.
            var inside = Set<Int>()
            var openedAt: Int?
            var depth = 0
            for (index, line) in lines.enumerated() {
                let range = NSRange(line.startIndex..., in: line)
                if openedAt == nil, declaration.firstMatch(in: line, range: range) != nil {
                    openedAt = depth
                }
                if openedAt != nil { inside.insert(index + 1) }
                depth += line.filter { $0 == "{" }.count - line.filter { $0 == "}" }.count
                if let start = openedAt, depth <= start, line.contains("}") { openedAt = nil }
            }

            for literal in literals(in: text) where hasCyrillic(literal.text) {
                guard inside.contains(literal.line), Self.allowed[literal.text] == nil else { continue }
                // `String(localized:)` бывает и строкой выше — длинный текст
                // переносят под открывающую скобку.
                let context = lines[max(0, literal.line - 2)...(literal.line - 1)].joined()
                if context.contains("String(localized:") || context.contains("LocalizedStringKey") { continue }
                found.append("\(file.lastPathComponent):\(literal.line): «\(literal.text)»")
            }
        }
        XCTAssertTrue(
            found.isEmpty,
            "русский литерал в позиции типа String — каталог сюда не заглядывает. "
                + "Оберни в String(localized:) или смени тип на LocalizedStringKey:\n"
                + found.sorted().joined(separator: "\n")
        )
    }

    /// Список исключений не пухнет молча: устаревшая запись обязана бросаться в глаза.
    func testAllowListHasNoStaleEntries() throws {
        var seen = Set<String>()
        for file in try swiftFiles() {
            let text = withoutComments(try String(contentsOf: file, encoding: .utf8))
            seen.formUnion(literals(in: text).map(\.text))
        }
        let stale = Self.allowed.keys.filter { !seen.contains($0) }.sorted()
        XCTAssertTrue(stale.isEmpty, "исключения больше не встречаются в коде, убери их: \(stale)")
    }
}
