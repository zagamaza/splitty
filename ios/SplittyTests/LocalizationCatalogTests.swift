import XCTest
@testable import Splitty

/// Пришивает String Catalog к сборке: недостающий перевод обязан ронять набор,
/// а не тихо показывать русский текст под английским интерфейсом. Русский —
/// исходный язык, поэтому ключ каталога и есть русская строка: пропущенный
/// перевод внешне неотличим от нормальной работы, и поймать его может только
/// такая проверка.
final class LocalizationCatalogTests: XCTestCase {
    private static let languages = ["ru", "en", "es", "de", "fr", "ja", "zh-Hans", "ko", "pt-BR", "it"]

    /// Обязательные формы множественного числа по языку (CLDR). У японского,
    /// китайского и корейского форма ОДНА: требовать у них `one` — значит
    /// требовать несуществующую грамматику, а разрешать лишние формы значит
    /// пропускать мусор, который переводчик скопировал из соседнего языка.
    private static func pluralForms(_ language: String) -> Set<String> {
        switch language {
        case "ru": return ["one", "few", "many", "other"]
        case "ja", "zh-Hans", "ko": return ["other"]
        default: return ["one", "other"]
        }
    }

    /// Полный список проблем уходит в файл: обрезание до десяти строк делало
    /// тест бесполезным ровно тогда, когда работы много.
    private func report(_ problems: [String], _ label: String) -> String {
        let byLanguage = Dictionary(grouping: problems) { problem -> String in
            // Язык стоит либо в конце строки, либо перед пояснением/формой.
            Self.languages.first {
                problem.hasSuffix(" \($0)") || problem.contains(" \($0) ") || problem.contains(" \($0)/")
            } ?? "?"
        }
        let counts = byLanguage.map { "\($0.key): \($0.value.count)" }.sorted().joined(separator: ", ")
        let path = NSTemporaryDirectory() + "splitty-i18n-\(label).txt"
        try? problems.sorted().joined(separator: "\n").write(toFile: path, atomically: true, encoding: .utf8)
        return "проблем \(problems.count) [\(counts)]\nполный список: \(path)\n"
            + problems.sorted().prefix(40).joined(separator: "\n")
    }

    /// Каталог в исходниках — читается по пути этого файла (тесты идут на
    /// симуляторе, файловая система общая с машиной сборки).
    private static let catalogURL = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()   // SplittyTests
        .deletingLastPathComponent()   // ios
        .appendingPathComponent("Splitty/Localizable.xcstrings")

    // MARK: Собранный бандл

    /// Ключи одного языка из собранного приложения: .strings плюс .stringsdict
    /// (множественные формы уезжают именно туда).
    private func bundleKeys(_ language: String) throws -> Set<String> {
        var keys = Set<String>()
        for ext in ["strings", "stringsdict"] {
            guard let url = Bundle.main.url(
                forResource: "Localizable", withExtension: ext,
                subdirectory: nil, localization: language
            ) else { continue }
            let dict = try XCTUnwrap(NSDictionary(contentsOf: url) as? [String: Any],
                                     "\(language).lproj/Localizable.\(ext) не читается")
            keys.formUnion(dict.keys)
        }
        return keys
    }

    func testAppBundleCarriesEveryLocalization() throws {
        XCTAssertEqual(Bundle.main.bundleIdentifier, "com.zagir.splitty",
                       "тесты обязаны идти с приложением-хостом, иначе локализация не проверяется")
        for language in Self.languages {
            XCTAssertFalse(try bundleKeys(language).isEmpty,
                           "в бандле нет \(language).lproj — каталог не собрался в этот язык")
        }
    }

    /// Главная проверка: каждый ключ, который просит приложение, отвечает
    /// на всех пяти языках. Русский — тоже: он лежит в каталоге явно, а не
    /// откатом на ключ.
    func testEveryKeyResolvesInEveryLanguage() throws {
        let reference = try bundleKeys("ru")
        XCTAssertGreaterThan(reference.count, 400, "подозрительно короткая таблица ключей")
        for language in Self.languages {
            let keys = try bundleKeys(language)
            let missing = reference.subtracting(keys).sorted()
            XCTAssertTrue(missing.isEmpty,
                          "нет перевода на \(language) (\(missing.count) ключей): "
                            + report(missing.map { "\($0): \(language) отсутствует" }, "bundle-\(language)"))
        }
    }

    // MARK: Исходный каталог

    private func catalog() throws -> [String: [String: Any]] {
        let data = try Data(contentsOf: Self.catalogURL)
        let json = try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(json["sourceLanguage"] as? String, "ru")
        return try XCTUnwrap(json["strings"] as? [String: [String: Any]])
    }

    /// Каждая запись каталога переведена на все пять языков, значения непустые,
    /// а у строк с числом заданы все формы: русскому нужны one/few/many, иначе
    /// «2 участник» и «5 участника» вернутся с первым же двузначным счётчиком.
    func testCatalogIsFullyTranslated() throws {
        var problems: [String] = []
        for (key, entry) in try catalog() {
            guard let localizations = entry["localizations"] as? [String: [String: Any]] else {
                problems.append("\(key): нет localizations")
                continue
            }
            for language in Self.languages {
                guard let unit = localizations[language] else {
                    problems.append("\(key): нет \(language)")
                    continue
                }
                if let plural = (unit["variations"] as? [String: Any])?["plural"] as? [String: Any] {
                    let required = Self.pluralForms(language)
                    for form in required.sorted() where plural[form] == nil {
                        problems.append("\(key): \(language) без формы \(form)")
                    }
                    for (form, value) in plural {
                        if !required.contains(form) {
                            problems.append("\(key): \(language)/\(form) — лишняя форма, в этом языке её нет")
                        }
                        let string = (value as? [String: Any])?["stringUnit"] as? [String: Any]
                        if (string?["value"] as? String)?.isEmpty != false {
                            problems.append("\(key): \(language)/\(form) пустой")
                        }
                        if string?["state"] as? String != "translated" {
                            problems.append("\(key): \(language)/\(form) не переведён")
                        }
                    }
                } else {
                    let string = unit["stringUnit"] as? [String: Any]
                    if (string?["value"] as? String)?.isEmpty != false {
                        problems.append("\(key): \(language) пустой")
                    }
                    if string?["state"] as? String != "translated" {
                        problems.append("\(key): \(language) не переведён")
                    }
                }
            }
        }
        XCTAssertTrue(problems.isEmpty, report(problems, "catalog"))
    }

    /// Каталог и собранный бандл описывают один и тот же набор ключей: лишняя
    /// запись — мусор от удалённого экрана, недостающая — незакрытый ключ.
    func testCatalogMatchesBundle() throws {
        let catalogKeys = Set(try catalog().keys)
        let bundle = try bundleKeys("ru")
        XCTAssertEqual(catalogKeys.subtracting(bundle).sorted(), [], "в бандле нет ключей каталога")
        XCTAssertEqual(bundle.subtracting(catalogKeys).sorted(), [], "в каталоге нет ключей бандла")
    }

    // MARK: Исходники

    /// Корень исходников приложения — рядом с каталогом.
    private static let sourcesURL = catalogURL
        .deletingLastPathComponent()   // Splitty

    /// Литералы, которые обязаны быть в каталоге: у `String(localized:)` это
    /// весь смысл вызова, у SwiftUI-инициализаторов голый литерал — тоже ключ.
    private static let localizedCall = try! NSRegularExpression(
        pattern: #"String\(\s*localized:\s*"([^"\\\n]*)"\s*\)"#
    )
    private static let swiftUILiteral = try! NSRegularExpression(
        pattern: #"\b(?:Text|Label|Button|Toggle|TextField|SecureField|Section|Picker|Link|NavigationLink|Stepper|Menu)\(\s*"([^"\\\n]*[А-Яа-яЁё₽][^"\\\n]*)""#
    )

    private func swiftSources() throws -> [(path: String, text: String)] {
        let manager = FileManager.default
        let enumerator = try XCTUnwrap(manager.enumerator(atPath: Self.sourcesURL.path))
        var files: [(String, String)] = []
        for case let name as String in enumerator where name.hasSuffix(".swift") {
            let url = Self.sourcesURL.appendingPathComponent(name)
            files.append((name, try String(contentsOf: url, encoding: .utf8)))
        }
        XCTAssertGreaterThan(files.count, 30, "исходники не найдены — проверь путь")
        return files
    }

    /// Забытая строка не ломает ничего видимого: ключ отсутствует во ВСЕХ
    /// языках сразу, включая русский, и `String(localized:)` отдаёт сам ключ —
    /// то есть русский текст. Под русским интерфейсом это неотличимо от
    /// нормы, а немец с французом читают русский. Остальные проверки набора
    /// сверяют каталог с бандлом и потому такую строку не видят: её нет ни там,
    /// ни там. Поймать её можно только со стороны исходников.
    func testEveryLocalizedLiteralIsInCatalog() throws {
        let known = Set(try catalog().keys)
        var problems: [String] = []
        for (name, text) in try swiftSources() {
            for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                guard !trimmed.hasPrefix("//") else { continue }
                let string = String(line)
                let range = NSRange(string.startIndex..., in: string)
                for regex in [Self.localizedCall, Self.swiftUILiteral] {
                    for match in regex.matches(in: string, range: range) {
                        guard let keyRange = Range(match.range(at: 1), in: string) else { continue }
                        let key = String(string[keyRange])
                        guard !key.isEmpty, !known.contains(key) else { continue }
                        problems.append("\(name): «\(key)»")
                    }
                }
            }
        }
        XCTAssertTrue(problems.isEmpty,
                      "нет в Localizable.xcstrings — под любым языком покажется русский:\n"
                      + problems.sorted().prefix(15).joined(separator: "\n"))
    }

    // MARK: Тексты

    /// Русская ветка не поменялась: ключ и его русское значение совпадают.
    /// Локализация была извлечением, а не переписыванием формулировок.
    func testRussianValuesEqualKeys() throws {
        for (key, entry) in try catalog() {
            guard let unit = (entry["localizations"] as? [String: [String: Any]])?["ru"] else { continue }
            guard let value = (unit["stringUnit"] as? [String: Any])?["value"] as? String else { continue }
            XCTAssertEqual(value, key, "русский текст разошёлся с ключом")
        }
    }

    /// Строки с подстановкой несут одни и те же спецификаторы во всех языках:
    /// потерянный %@ — это пустое место вместо имени или суммы в бою.
    func testFormatSpecifiersMatchAcrossLanguages() throws {
        let pattern = try NSRegularExpression(pattern: "%(?:\\d+\\$)?(?:@|lld)")
        // Номер аргумента отбрасываем: «%1$@» и «%@» — один и тот же тип,
        // а порядок в переводе имеет право отличаться от исходного.
        func specs(_ text: String) -> [String] {
            let range = NSRange(text.startIndex..., in: text)
            return pattern.matches(in: text, range: range).map { match -> String in
                let raw = String(text[Range(match.range, in: text)!])
                guard let tail = raw.split(separator: "$").last, raw.contains("$") else { return raw }
                return "%" + tail
            }.sorted()
        }
        for (key, entry) in try catalog() {
            let expected = specs(key)
            guard !expected.isEmpty else { continue }
            let localizations = entry["localizations"] as? [String: [String: Any]] ?? [:]
            for (language, unit) in localizations {
                var values: [String] = []
                if let plural = (unit["variations"] as? [String: Any])?["plural"] as? [String: Any] {
                    values = plural.values.compactMap {
                        (($0 as? [String: Any])?["stringUnit"] as? [String: Any])?["value"] as? String
                    }
                } else if let value = (unit["stringUnit"] as? [String: Any])?["value"] as? String {
                    values = [value]
                }
                for value in values {
                    XCTAssertEqual(specs(value), expected,
                                   "\(key) → \(language): набор подстановок не совпадает («\(value)»)")
                }
            }
        }
    }

    // MARK: Живая выборка

    /// Формы числа доезжают до экрана: и через `String(localized:)`, и через
    /// `Text(LocalizedStringKey)` — оба пути ходят в один .stringsdict.
    /// Набор идёт под ru (см. scheme), поэтому ждём русские формы.
    func testPluralFormsResolveAtRuntime() {
        XCTAssertEqual(String(localized: "\(1) участников"), "1 участник")
        XCTAssertEqual(String(localized: "\(2) участников"), "2 участника")
        XCTAssertEqual(String(localized: "\(5) участников"), "5 участников")
        XCTAssertEqual(String(localized: "\(21) участников"), "21 участник")
        XCTAssertEqual(String(localized: "\(14) участников"), "14 участников")

        XCTAssertEqual(String(localized: "без учёта \(1) неотправленных операций"),
                       "без учёта 1 неотправленной операции")
        XCTAssertEqual(String(localized: "без учёта \(3) неотправленных операций"),
                       "без учёта 3 неотправленных операций")
    }

    /// Резолв через рантайм, а не только через файлы: под ru и en одни и те же
    /// вызовы обязаны давать разный текст — значит, каталог реально подхвачен.
    func testRuntimeResolutionDiffersBetweenRussianAndEnglish() throws {
        let samples = ["Группы", "Все долги погашены", "Нет долгов"]
        for language in ["ru", "en"] {
            let url = try XCTUnwrap(Bundle.main.url(forResource: language, withExtension: "lproj"))
            let bundle = try XCTUnwrap(Bundle(url: url))
            for key in samples {
                let value = bundle.localizedString(forKey: key, value: "∅", table: "Localizable")
                XCTAssertNotEqual(value, "∅", "\(language): ключ «\(key)» не найден")
                if language == "en" {
                    XCTAssertNotEqual(value, key, "английский текст остался русским: «\(key)»")
                } else {
                    XCTAssertEqual(value, key)
                }
            }
        }
    }
}
