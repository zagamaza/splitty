import XCTest
@testable import Splitty

/// Флаг «приветствие показано» — по номеру аккаунта, а не на устройство.
@MainActor
final class WelcomeSeenTests: XCTestCase {

    private let key = "splitty.welcomeSeenAccounts"

    override func setUp() {
        super.setUp()
        UserDefaults.standard.removeObject(forKey: key)
    }

    override func tearDown() {
        UserDefaults.standard.removeObject(forKey: key)
        super.tearDown()
    }

    func testFreshAccountHasNotSeenIt() {
        let session = SessionStore()
        XCTAssertFalse(session.hasSeenWelcome(userId: 1))
    }

    func testMarkedAccountDoesNotSeeItAgain() {
        let session = SessionStore()
        session.markWelcomeSeen(userId: 1)
        XCTAssertTrue(session.hasSeenWelcome(userId: 1))
    }

    /// Второй человек на том же телефоне обязан увидеть приветствие: иначе он
    /// молча теряет единственное объяснение продукта.
    func testAnotherAccountOnSameDeviceSeesIt() {
        let session = SessionStore()
        session.markWelcomeSeen(userId: 1)
        XCTAssertFalse(session.hasSeenWelcome(userId: 2))
    }
}
