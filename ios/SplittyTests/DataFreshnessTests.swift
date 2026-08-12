import XCTest
@testable import Splitty

/// Свежесть показанных данных.
///
/// Признак «данные из кеша» вычислялся и раньше, но никуда не попадал: человек
/// смотрел на старые суммы, ничего об этом не зная, — и «неправильный» баланс
/// выглядел как ошибка расчёта, а не как отсутствие связи.
@MainActor
final class DataFreshnessTests: XCTestCase {

    /// Свежая модель не выглядит как кеш и не выдумывает время обновления:
    /// иначе подпись «обновлялось только что» появлялась бы до первой загрузки.
    func testFreshModelHasNoUpdateTimeAndNoCacheFlag() {
        let model = GroupsListViewModel()

        XCTAssertFalse(model.isFromCache, "свежая модель выглядит как кеш")
        XCTAssertNil(model.lastUpdatedAt, "время обновления взялось из ниоткуда")
    }
}
