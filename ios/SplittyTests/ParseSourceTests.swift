import XCTest
@testable import Splitty

/// Плашка распознавания говорит правду об источнике.
///
/// Раньше после фото чека экран сообщал «Распознано голосом» и следом предлагал
/// добавить фото — то самое, которое человек только что добавил. Подсказка
/// «не то?» вела в тупик: единственный предложенный путь исправления был уже
/// пройден.
@MainActor
final class ParseSourceTests: XCTestCase {

    func testDefaultSourceIsVoice() {
        let model = AddExpenseViewModel()

        XCTAssertEqual(model.lastParseSource, .voice)
    }

    /// Разбор без медиа (текстовая правка) источник не меняет: последним
    /// реальным вводом остаётся то, чем пользовались до этого.
    func testTextOnlyParseKeepsPreviousSource() async {
        let model = AddExpenseViewModel()
        let api = APIClient(baseURL: URL(string: "http://localhost:1")!, token: nil)

        await model.parse(api: api, text: "кофе 300")

        XCTAssertEqual(model.lastParseSource, .voice)
    }
}
