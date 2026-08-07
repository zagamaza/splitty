import XCTest
@testable import Splitty

/// Разбор возврата из Telegram Login Widget (`splitty://tg-callback`).
///
/// Тесты появились после того, как ровно эта логика сломалась на Android:
/// там декодер принимал ТОЛЬКО base64url, падал на стандартном алфавите и
/// возвращал nil молча — человек оставался на экране входа без единого намёка.
/// Здесь нормализация есть с самого начала, и она должна такой остаться.
final class TelegramWebAuthTests: XCTestCase {

    private let payloadJSON = """
    {"id":147181773,"first_name":"Загир","last_name":null,"username":"zagir",\
    "photo_url":"https://t.me/i/userpic/320/zagir.jpg","auth_date":1750000000,"hash":"abc123"}
    """

    private var standardBase64: String {
        Data(payloadJSON.utf8).base64EncodedString()
    }

    private func callbackURL(query: String) -> URL {
        URL(string: "splitty://tg-callback?\(query)")!
    }

    func testDecodesResultFromQuery() throws {
        let encoded = standardBase64.addingPercentEncoding(withAllowedCharacters: .alphanumerics)!
        let payload = try TelegramWebAuth.decode(callbackURL: callbackURL(query: "tgAuthResult=\(encoded)"))

        XCTAssertEqual(payload.id, 147_181_773)
        XCTAssertEqual(payload.firstName, "Загир")
        XCTAssertNil(payload.lastName)
        XCTAssertEqual(payload.username, "zagir")
        XCTAssertEqual(payload.authDate, 1_750_000_000)
        XCTAssertEqual(payload.hash, "abc123")
    }

    /// Telegram кладёт результат во fragment, сервер — в query. Работать
    /// обязаны оба: иначе вход ломается на половине путей возврата.
    func testDecodesResultFromFragment() throws {
        let encoded = standardBase64.addingPercentEncoding(withAllowedCharacters: .alphanumerics)!
        let url = URL(string: "splitty://tg-callback#tgAuthResult=\(encoded)")!

        XCTAssertEqual(try TelegramWebAuth.decode(callbackURL: url).id, 147_181_773)
    }

    /// Главное, ради чего тест и написан: алфавит base64 может приехать любой.
    func testAcceptsBothBase64Alphabets() throws {
        let urlSafe = standardBase64
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
        let withoutPadding = urlSafe.replacingOccurrences(of: "=", with: "")

        for variant in [standardBase64, urlSafe, withoutPadding] {
            let encoded = variant.addingPercentEncoding(withAllowedCharacters: .alphanumerics)!
            let payload = try TelegramWebAuth.decode(callbackURL: callbackURL(query: "tgAuthResult=\(encoded)"))
            XCTAssertEqual(payload.id, 147_181_773, "не разобрали вариант «\(variant.suffix(6))»")
        }
    }

    /// Мусор обязан стать ОШИБКОЙ, а не тихим nil: молчание на этом месте —
    /// самый дорогой в отладке вид поломки.
    func testGarbageThrowsBadResponse() {
        let cases = [
            "",                                  // результата нет вовсе
            "tgAuthResult=",                     // пустой результат
            "tgAuthResult=не-base64",            // не декодируется
            "tgAuthResult=\(Data("{}".utf8).base64EncodedString())",  // не тот JSON
        ]
        for query in cases {
            XCTAssertThrowsError(
                try TelegramWebAuth.decode(callbackURL: callbackURL(query: query)),
                "«\(query)» обязан бросить ошибку"
            )
        }
    }
}
