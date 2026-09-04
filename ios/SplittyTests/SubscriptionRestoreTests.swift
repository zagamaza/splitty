import XCTest
@testable import Splitty

/// «Восстановить покупки» у человека с ПОДАРЕННЫМ Plus.
///
/// Раньше сообщение «Покупок для восстановления не нашлось» пряталось при
/// `tier == .plus`, и такой человек (а также comp-аккаунт ревьюера магазина)
/// тапал кнопку и получал молчаливый успех: восстановилось будто бы что-то,
/// чего он никогда не покупал.
@MainActor
final class SubscriptionRestoreTests: XCTestCase {
    private var urlSession: URLSession!

    override func setUp() {
        super.setUp()
        StubURLProtocol.handler = { request in
            let path = request.url?.path ?? ""
            // Тариф plus БЕЗ реквизитов покупки — ровно то, что отдаёт сервер
            // человеку с грантом: ни стора, ни продукта, ни ссылки.
            if path.hasSuffix("/me/subscription") {
                return (200, Data(#"{"tier":"plus","expiresAt":"2027-01-01T00:00:00Z"}"#.utf8))
            }
            let quota = #"{"tier":"plus","limit":-1,"used":0,"remaining":-1,"unlimited":true,"resetsAt":"2026-09-01T00:00:00Z"}"#
            return (200, Data(quota.utf8))
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        urlSession = URLSession(configuration: configuration)
    }

    override func tearDown() {
        StubURLProtocol.handler = nil
        urlSession = nil
        super.tearDown()
    }

    /// Покупок нет — так и говорим, каким бы ни был тариф.
    func testRestoreTellsThereAreNoPurchasesEvenWhenPlus() async {
        let store = SubscriptionStore(api: { [self] in
            APIClient(baseURL: URL(string: "https://api.example.test"),
                      token: "живой-токен",
                      urlSession: urlSession)
        })

        let closePaywall = await store.restore()

        XCTAssertNotNil(store.purchaseError,
                        "человеку с подаренным Plus не сказали, что покупок не нашлось")
        // Возвращаемое значение — про другое: это сигнал закрыть пейвол.
        // Оставить человека с Plus на экране с кнопками покупки нельзя.
        XCTAssertTrue(closePaywall, "пейвол не закрылся у человека с Plus")
        XCTAssertEqual(store.tier, .plus)
    }
}
