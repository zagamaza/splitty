import XCTest
@testable import Splitty

/// Реквизит чека печатается капсом с разрядкой только там, где у языка есть
/// регистр: у японского, китайского и корейского его нет, а разрядка рвёт иероглифы.
final class ReceiptCapsTests: XCTestCase {
    func testCapsOnlyForCasedLanguages() {
        XCTAssertTrue(ReceiptCaps.allowsCaps(languageCode: "ru"))
        XCTAssertTrue(ReceiptCaps.allowsCaps(languageCode: "de"))
        XCTAssertTrue(ReceiptCaps.allowsCaps(languageCode: nil))
        XCTAssertFalse(ReceiptCaps.allowsCaps(languageCode: "ja"))
        XCTAssertFalse(ReceiptCaps.allowsCaps(languageCode: "zh"))
        XCTAssertFalse(ReceiptCaps.allowsCaps(languageCode: "KO"))
    }
}
