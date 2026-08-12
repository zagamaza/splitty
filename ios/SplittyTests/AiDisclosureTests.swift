import XCTest
@testable import Splitty

/// Пояснение о том, что распознавание идёт на сервере.
///
/// Куда уходят голос и фото чека, человек мог узнать только из политики, которую
/// никто не открывает. Показываем один раз перед первой отправкой: повторять на
/// каждом расходе — значит приучить закрывать не читая.
final class AiDisclosureTests: XCTestCase {

    private var defaults: UserDefaults!
    private let suite = "ai-disclosure-tests"

    override func setUp() {
        super.setUp()
        UserDefaults.standard.removePersistentDomain(forName: suite)
        defaults = UserDefaults(suiteName: suite)
    }

    override func tearDown() {
        UserDefaults.standard.removePersistentDomain(forName: suite)
        super.tearDown()
    }

    func testDisclosureIsNotSeenByDefault() {
        XCTAssertFalse(
            defaults.bool(forKey: AddExpenseView.aiDisclosureKey),
            "флаг взведён до первого показа — пояснение не увидит никто"
        )
    }

    func testDisclosureIsRememberedOnce() {
        defaults.set(true, forKey: AddExpenseView.aiDisclosureKey)

        XCTAssertTrue(
            defaults.bool(forKey: AddExpenseView.aiDisclosureKey),
            "пояснение показывается снова и снова — человек привыкнет закрывать не читая"
        )
    }
}
