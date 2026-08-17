import XCTest
@testable import Splitty

/// Правила показа приветствия.
final class WelcomeGateTests: XCTestCase {

    func testShownToNewAccountWithoutGroups() {
        XCTAssertTrue(shouldShowWelcome(hasSeen: false, groupCount: 0, hasPendingDeeplink: false))
    }

    /// Второй запуск: человек уже всё это читал.
    func testNotShownTwice() {
        XCTAssertFalse(shouldShowWelcome(hasSeen: true, groupCount: 0, hasPendingDeeplink: false))
    }

    /// У кого есть группы — объяснять нечего, а поверх работы это раздражает.
    func testNotShownWhenGroupsExist() {
        XCTAssertFalse(shouldShowWelcome(hasSeen: false, groupCount: 2, hasPendingDeeplink: false))
    }

    /// Пришёл по ссылке приглашения — ведём в группу, а не в рассказ о продукте.
    func testDeeplinkWins() {
        XCTAssertFalse(shouldShowWelcome(hasSeen: false, groupCount: 0, hasPendingDeeplink: true))
    }
}
