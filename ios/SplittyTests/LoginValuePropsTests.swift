import XCTest
@testable import Splitty

/// Что обещает экран входа.
///
/// Раньше это была одна строка «Делите расходы с друзьями» — она описывает
/// любое приложение категории и не отвечает ни на один вопрос человека.
/// Три пункта отвечают: что я записываю, что это даёт, переводит ли
/// приложение деньги. Последний снимает самый частый страх, и снимать его
/// надо ДО регистрации.
final class LoginValuePropsTests: XCTestCase {

    func testThreePropsExactly() {
        XCTAssertEqual(LoginValueProp.all.count, 3, "экран входа снова объясняет продукт не тремя пунктами")
    }

    func testEveryPropHasTitleAndDetail() {
        for prop in LoginValueProp.all {
            XCTAssertFalse(prop.title.isEmpty, "пункт \(prop.id) без заголовка")
            XCTAssertFalse(prop.detail.isEmpty, "пункт \(prop.id) без пояснения")
        }
    }

    /// Самый частый страх: «оно спишет деньги?». Ответ обязан быть на входе.
    func testMoneyDisclaimerIsPresent() {
        let money = LoginValueProp.all.first { $0.id == "money" }
        XCTAssertNotNil(money, "с экрана входа пропал пункт про то, что деньги передаёт человек сам")
    }

    /// Ради чего приложение вообще нужно — тоже на входе, а не после регистрации.
    func testNettingPromiseIsPresent() {
        let once = LoginValueProp.all.first { $0.id == "once" }
        XCTAssertNotNil(once, "с экрана входа пропало главное обещание — меньше переводов")
    }
}
