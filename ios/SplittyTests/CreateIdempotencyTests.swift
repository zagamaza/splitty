import XCTest
@testable import Splitty

/// Ключ идемпотентности создания расхода и правило «что переживает в очереди».
///
/// Сервер мог записать расход и не успеть ответить. Повтор с НОВЫМ ключом завёл
/// бы второй такой же расход, а отказ вовсе — потерял бы работу человека.
final class CreateIdempotencyTests: XCTestCase {

    private func payload(description: String = "Ужин", sum: Int = 1_200) -> OutboxPayload {
        OutboxPayload(
            description: description,
            sum: sum,
            donorId: 1,
            recipientIds: [1, 2],
            recipientSums: nil
        )
    }

    func testRepeatWithSameContentKeepsKey() {
        var idempotency = CreateIdempotency()
        let first = idempotency.key(for: payload())
        let second = idempotency.key(for: payload())
        XCTAssertEqual(first, second, "повтор ушёл с новым ключом — сервер заведёт второй такой же расход")
    }

    func testChangedContentGetsNewKey() {
        var idempotency = CreateIdempotency()
        let first = idempotency.key(for: payload(sum: 1_200))
        let second = idempotency.key(for: payload(sum: 1_500))
        XCTAssertNotEqual(first, second, "исправленная сумма ушла со старым ключом — вернётся первая операция")
    }

    func testServerErrorsGoToOutboxOnlyFrom500() {
        XCTAssertTrue(APIError.transport(URLError(.notConnectedToInternet)).deservesOutbox)
        XCTAssertTrue(APIError.server(status: 500, code: "internal", message: "").deservesOutbox)
        XCTAssertTrue(APIError.server(status: 503, code: "internal", message: "").deservesOutbox)
        XCTAssertFalse(
            APIError.server(status: 400, code: "validation", message: "").deservesOutbox,
            "отказ по данным спрятался в очередь — человек его не увидит и не исправит"
        )
        XCTAssertFalse(APIError.server(status: 409, code: "conflict", message: "").deservesOutbox)
    }
}
