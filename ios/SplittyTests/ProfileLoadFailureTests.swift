import XCTest
@testable import Splitty

/// Профиль, который не загрузился.
///
/// Раньше экран навсегда оставался скелетоном и показывал ВСЕ способы входа как
/// «Не привязан» — то есть врал про безопасность аккаунта: человек мог решить,
/// что вход через Apple отвалился, и пойти привязывать его заново.
@MainActor
final class ProfileLoadFailureTests: XCTestCase {

    func testFreshSessionDoesNotClaimFailure() {
        let session = SessionStore()

        XCTAssertFalse(
            session.profileLoadFailed,
            "новая сессия сразу заявляет о сбое — экран покажет ошибку до первой попытки"
        )
    }

    /// Неавторизованная сессия молчит: refreshMe выходит сразу, и признак сбоя
    /// не должен появляться (иначе экран входа мигал бы ошибкой профиля).
    func testUnauthenticatedRefreshDoesNotFlagFailure() async {
        let session = SessionStore()

        await session.refreshMe()

        XCTAssertFalse(session.profileLoadFailed, "сбой профиля отмечен без единого запроса")
    }
}
