import XCTest
@testable import Splitty

/// Два правила, которые до этого жили внутри вью и проверялись только глазами,
/// хотя на Android у обоих есть тесты.
///
/// Первое: выход из группы спрашивают. Он необратим — вернуться можно только по
/// новому приглашению участника, — а кнопка стоит вплотную к «Открыть».
/// Второе: вход по коду доступен всегда. Раньше он жил только в пустом
/// состоянии, и человек с одной группой попасть в него не мог никак, хотя код
/// присылают как раз тем, у кого группы уже есть.
final class InviteActionsTests: XCTestCase {

    func testLeaveAsksFirstAndOtherActionsDoNot() {
        XCTAssertTrue(inviteActionNeedsConfirm(.leave), "выход из группы происходит по одному тапу")
        XCTAssertFalse(inviteActionNeedsConfirm(.accept))
        XCTAssertFalse(inviteActionNeedsConfirm(.decline))
    }

    func testJoinByCodeIsOfferedWithGroupsAndWithout() {
        XCTAssertTrue(
            groupsAddMenuActions(hasGroups: true).contains(.joinByCode),
            "вход по коду недостижим, когда группы уже есть"
        )
        XCTAssertTrue(groupsAddMenuActions(hasGroups: false).contains(.joinByCode))
    }

    func testCreateGroupStaysInTheMenu() {
        for hasGroups in [true, false] {
            XCTAssertTrue(
                groupsAddMenuActions(hasGroups: hasGroups).contains(.create),
                "создание группы потерялось при переезде в меню"
            )
        }
    }
}
