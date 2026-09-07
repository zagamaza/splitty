import XCTest
@testable import Splitty

/// Просьба оценить приложение.
///
/// Ошибка здесь молчит в обе стороны: перекрутили отсечку — не спрашиваем
/// никогда и не узнаём об этом, недокрутили — спрашиваем на каждом расходе.
final class ReviewPromptTests: XCTestCase {

    private var defaults: UserDefaults!
    private let suite = "review-prompt-tests"

    override func setUp() {
        super.setUp()
        UserDefaults.standard.removePersistentDomain(forName: suite)
        defaults = UserDefaults(suiteName: suite)
    }

    override func tearDown() {
        UserDefaults.standard.removePersistentDomain(forName: suite)
        super.tearDown()
    }

    func testDoesNotAskAfterSingleExpense() {
        let prompt = ReviewPrompt(defaults: defaults)

        prompt.note(.expenseAdded)

        XCTAssertFalse(prompt.isEarned, "просим после первого же расхода — человек ещё ничего не увидел")
    }

    func testAsksAfterThreeExpenses() {
        let prompt = ReviewPrompt(defaults: defaults)

        prompt.note(.expenseAdded)
        prompt.note(.expenseAdded)
        prompt.note(.expenseAdded)

        XCTAssertTrue(prompt.isEarned, "три расхода — человек пользуется, а просьба так и не заслужена")
    }

    func testSettledDebtAsksImmediately() {
        let prompt = ReviewPrompt(defaults: defaults)

        prompt.note(.debtSettled)

        XCTAssertTrue(prompt.isEarned, "погашение долга — пик, ради которого приложение и ставили")
    }

    func testDoesNotAskTwiceInSameVersion() {
        let prompt = ReviewPrompt(defaults: defaults)
        prompt.note(.debtSettled)
        prompt.markAsked()

        prompt.note(.debtSettled)

        XCTAssertFalse(prompt.isEarned, "в одной версии спрашиваем дважды")
    }

    func testCounterSurvivesRestart() {
        ReviewPrompt(defaults: defaults).note(.expenseAdded)
        ReviewPrompt(defaults: defaults).note(.expenseAdded)

        let afterRestart = ReviewPrompt(defaults: defaults)
        afterRestart.note(.expenseAdded)

        XCTAssertTrue(afterRestart.isEarned, "счётчик обнуляется перезапуском — до трёх он не дорастёт никогда")
    }

    func testQuietPeriodBlocksNextVersion() {
        let prompt = ReviewPrompt(defaults: defaults)
        prompt.note(.debtSettled)
        prompt.markAsked()
        // Другая версия сборки: отсечка по версии больше не держит, держать
        // должна пауза.
        defaults.set("совсем другая версия", forKey: "splitty.review.askedVersion")

        prompt.note(.debtSettled)

        XCTAssertFalse(prompt.isEarned, "новая версия сбрасывает паузу — просьба уходит в квоту Apple впустую")
    }

    func testAsksAgainAfterQuietPeriod() {
        let prompt = ReviewPrompt(defaults: defaults)
        prompt.note(.debtSettled)
        prompt.markAsked()
        defaults.set("совсем другая версия", forKey: "splitty.review.askedVersion")
        let longAgo = Date.now.timeIntervalSince1970 - 121 * 24 * 60 * 60
        defaults.set(longAgo, forKey: "splitty.review.askedAt")

        prompt.note(.debtSettled)

        XCTAssertTrue(prompt.isEarned, "пауза прошла, версия сменилась — а просьба всё равно не заслужена")
    }
}
