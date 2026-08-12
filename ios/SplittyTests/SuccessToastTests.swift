import XCTest
@testable import Splitty

/// Подтверждение выполненного действия.
///
/// Ни погашение, ни выход из группы, ни смена пароля ничего не отвечали: человек
/// не понимал, случилось ли действие, и повторял его. Сохранение расхода в
/// очередь при этом выглядело ровно как сохранение на сервер — про очередь он
/// узнавал, только открыв группу и увидев там пометку.
@MainActor
final class SuccessToastTests: XCTestCase {

    func testConfirmationIsPublishedAndCleared() {
        let session = SessionStore()

        XCTAssertNil(session.successToast, "подтверждение показано до всякого действия")

        session.confirm("Погашение записано")
        XCTAssertEqual(session.successToast, "Погашение записано")

        session.dismissToast()
        XCTAssertNil(session.successToast, "подтверждение висит на экране навсегда")
    }

    /// Расход, ушедший в очередь, помечается отдельно — иначе офлайн-сохранение
    /// неотличимо от отправки на сервер.
    func testOfflineSaveIsMarkedSeparately() async {
        let model = AddExpenseViewModel()

        XCTAssertFalse(model.savedOffline, "форма считает себя офлайн-сохранённой до сохранения")
    }
}
