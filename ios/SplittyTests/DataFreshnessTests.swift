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

    /// Подпись видна не только в списке групп: на карточке группы, друзьях и
    /// активности признак считался, но человеку не доставался.
    func testEveryCachedScreenCarriesTheUpdateTime() {
        XCTAssertNil(GroupDetailViewModel().lastUpdatedAt)
        XCTAssertNil(FriendsViewModel().lastUpdatedAt)
        XCTAssertNil(ActivityViewModel().lastUpdatedAt)

        XCTAssertFalse(GroupDetailViewModel().isFromCache)
        XCTAssertFalse(FriendsViewModel().isFromCache)
        XCTAssertFalse(ActivityViewModel().isFromCache)
    }

    /// Текст без времени обновления не притворяется свежим: кеш из прошлого
    /// запуска — это «связи нет», а не «обновлялось только что».
    func testCacheNoteWithoutTimeSaysThereIsNoConnection() {
        let text = cacheNoteText(updatedAt: nil)

        XCTAssertTrue(text.contains("связи с сервером нет"), text)
    }

    func testCacheNoteWithTimeMentionsIt() {
        let text = cacheNoteText(updatedAt: Date(timeIntervalSinceNow: -3600))

        XCTAssertTrue(text.contains("обновлялись"), text)
        XCTAssertFalse(text.contains("связи с сервером нет"), text)
    }
}
