import XCTest
@testable import Splitty

/// Правила показа приветствия.
final class WelcomeGateTests: XCTestCase {

    func testShownToFreshInstall() {
        XCTAssertTrue(shouldShowIntro(hasSeen: false, hasPendingDeeplink: false))
    }

    /// Второй запуск: эта установка всё это уже читала.
    func testNotShownTwice() {
        XCTAssertFalse(shouldShowIntro(hasSeen: true, hasPendingDeeplink: false))
    }

    /// Пришёл по ссылке приглашения — ведём в тусу, а не в рассказ о продукте.
    ///
    /// Показать приглашённому четыре страницы вместо тусы, в которую его
    /// позвали, значит потерять переход.
    func testDeeplinkWins() {
        XCTAssertFalse(shouldShowIntro(hasSeen: false, hasPendingDeeplink: true))
    }

    /// Намерение побеждает и тогда, когда приветствие уже на экране.
    ///
    /// Персистентное намерение читается синхронно и к первому кадру на месте, а
    /// свежий тап по universal link приходит в `.onOpenURL` уже ПОСЛЕ отрисовки.
    /// Поэтому решение показать приветствие не защёлкивается: гейт — чистая
    /// функция от текущего состояния, и появившееся намерение закрывает
    /// приветствие само.
    func testDeeplinkArrivingLaterClosesIntro() {
        XCTAssertTrue(shouldShowIntro(hasSeen: false, hasPendingDeeplink: false))
        XCTAssertFalse(shouldShowIntro(hasSeen: false, hasPendingDeeplink: true))
    }
}
