import XCTest
@testable import Splitty

/// Каноническое правило деления расхода (Money.swift: shares) —
/// единое для сервера и клиента: base = S / n, r = S % n;
/// получатель с индексом i платит base+1 при i < r, иначе base.
/// Используется ТОЛЬКО для подсказки предпросмотра в форме (splitHint);
/// позиции по операциям — из хранимых сумм (см. OperationModelTests).
final class SharesTests: XCTestCase {
    // MARK: shares(sum:count:)

    func testSharesEvenSplit() {
        XCTAssertEqual(shares(sum: 1200, count: 3), [400, 400, 400])
    }

    func testSharesRemainderGoesToFirstRecipients() {
        // 1000 / 3: base 333, остаток 1 — первому 334.
        XCTAssertEqual(shares(sum: 1000, count: 3), [334, 333, 333])
        // 100 / 3: первому 34.
        XCTAssertEqual(shares(sum: 100, count: 3), [34, 33, 33])
        // 5 / 3: первым двум по 2.
        XCTAssertEqual(shares(sum: 5, count: 3), [2, 2, 1])
    }

    func testSharesSingleRecipientTakesAll() {
        XCTAssertEqual(shares(sum: 999, count: 1), [999])
    }

    func testSharesSumLessThanCount() {
        XCTAssertEqual(shares(sum: 2, count: 3), [1, 1, 0])
    }

    func testSharesZeroCountIsEmpty() {
        XCTAssertEqual(shares(sum: 100, count: 0), [])
    }

    func testSharesAlwaysSumToTotal() {
        for sum in [1, 5, 13, 99, 100, 199, 1000, 12345] {
            for count in 1...9 {
                let parts = shares(sum: sum, count: count)
                XCTAssertEqual(parts.count, count, "S=\(sum) n=\(count)")
                XCTAssertEqual(parts.reduce(0, +), sum, "S=\(sum) n=\(count): сумма долей должна быть равна S")
                // Доли невозрастающие: остаток — первым.
                XCTAssertEqual(parts, parts.sorted(by: >), "S=\(sum) n=\(count)")
            }
        }
    }

    // MARK: rublesRange — подпись неровного деления

    func testRublesRange() {
        XCTAssertEqual(rublesRange(333, 334), "333–334 ₽")
        XCTAssertEqual(rublesRange(1333, 1334), "1 333–1 334 ₽")
    }
}
