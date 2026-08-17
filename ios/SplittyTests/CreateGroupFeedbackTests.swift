import XCTest
@testable import Splitty

/// Создание группы обязано что-то менять на экране.
///
/// На «Друзьях» кнопка пустого состояния открывала шит, создавала группу и
/// возвращала ровно тот же пустой экран: новая группа без участников друзей не
/// даёт. Люди жали её по три раза подряд, не понимая, работает ли она. Поэтому
/// каждый вызов `CreateGroupView` обязан что-то делать с результатом —
/// пустой колбэк `{ _ in }` допустим только там, где новая группа сразу видна
/// в списке.
final class CreateGroupFeedbackTests: XCTestCase {

    func testFriendsScreenDoesSomethingWithTheCreatedGroup() throws {
        let source = try String(
            contentsOf: URL(fileURLWithPath: #filePath)
                .deletingLastPathComponent()
                .deletingLastPathComponent()
                .appendingPathComponent("Splitty/Features/Friends/FriendsListView.swift"),
            encoding: .utf8
        )

        XCTAssertTrue(
            source.contains("CreateGroupView"),
            "экран друзей больше не создаёт группу — тест пора переписать"
        )
        XCTAssertFalse(
            source.contains("CreateGroupView { _ in }") || source.contains("CreateGroupView {}"),
            "созданную группу с «Друзей» снова никто не открывает: экран не изменится"
        )
    }
}
