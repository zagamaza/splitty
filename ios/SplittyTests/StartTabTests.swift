import XCTest
@testable import Splitty

/// Стартовая вкладка.
///
/// Новый аккаунт открывался на «Друзьях» — разделе, где у него по определению
/// пусто, и первой фразой приложения было «Пока нет друзей». Группы — то, ради
/// чего человек пришёл, и начинать надо с них.
final class StartTabTests: XCTestCase {

    func testAppOpensOnGroups() {
        XCTAssertEqual(MainTabView.Tab.initial, .groups, "приложение снова открывается не на группах")
    }
}
