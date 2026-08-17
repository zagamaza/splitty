import XCTest
@testable import Splitty

/// Пустые состояния объясняют, а не отрицают.
///
/// Пустой экран — единственное место, где человек точно читает текст. Тратить
/// его на констатацию пустоты («Пока нет друзей») расточительно, а для новичка
/// это ещё и первая фраза приложения, ссылающаяся на незнакомое ему понятие.
final class EmptyStateTextsTests: XCTestCase {

    /// Ни один заголовок пустого состояния не начинается с отрицания.
    func testNoEmptyStateStartsWithDenial() throws {
        let root = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent()   // SplittyTests
            .deletingLastPathComponent()   // ios
            .appendingPathComponent("Splitty/Features")

        let denials = ["Пока нет", "Нет ", "No "]
        var offenders: [String] = []

        let files = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil)?
            .compactMap { $0 as? URL }
            .filter { $0.pathExtension == "swift" } ?? []

        for file in files {
            let text = try String(contentsOf: file, encoding: .utf8)
            for line in text.split(separator: "\n") where line.contains("ContentUnavailableView") == false {
                guard line.contains("Label(\"") else { continue }
                for denial in denials where line.contains("Label(\"\(denial)") {
                    offenders.append("\(file.lastPathComponent): \(line.trimmingCharacters(in: .whitespaces))")
                }
            }
        }

        XCTAssertTrue(
            offenders.isEmpty,
            "пустое состояние снова начинается с отрицания:\n" + offenders.joined(separator: "\n")
        )
    }
}
