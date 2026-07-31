import CryptoKit
import XCTest
@testable import Splitty

/// Nonce для Sign in with Apple (AppleNonce.swift): генерация сырого значения
/// и его SHA256-hex. Чистая логика — без сети и без ASAuthorization.
final class AppleNonceTests: XCTestCase {
    // MARK: random(length:) — длина и алфавит

    func testRandomHasRequestedLength() {
        XCTAssertEqual(AppleNonce.random().count, AppleNonce.defaultLength)
        XCTAssertEqual(AppleNonce.random(length: 1).count, 1)
        XCTAssertEqual(AppleNonce.random(length: 64).count, 64)
    }

    func testDefaultLengthIsEnoughEntropy() {
        // 32 символа алфавита из 65 — около 192 бит. Меньше 16 символов
        // делало бы перебор осмысленным, поэтому длина зафиксирована тестом.
        XCTAssertGreaterThanOrEqual(AppleNonce.defaultLength, 32)
    }

    func testRandomUsesExpectedAlphabet() {
        // Алфавит — буквы, цифры и `-._`: всё безопасно в JSON, JWT и URL.
        XCTAssertEqual(AppleNonce.alphabet.count, 65)
        let allowed = Set(AppleNonce.alphabet)
        for _ in 0..<50 {
            let nonce = AppleNonce.random()
            XCTAssertTrue(
                nonce.allSatisfy { allowed.contains($0) },
                "nonce содержит символ вне алфавита: \(nonce)"
            )
        }
    }

    func testRandomIsUnpredictable() {
        // Совпадение двух значений означало бы, что защита от повторного
        // использования чужого токена не работает вовсе.
        XCTAssertNotEqual(AppleNonce.random(), AppleNonce.random())

        var seen = Set<String>()
        for _ in 0..<200 {
            seen.insert(AppleNonce.random())
        }
        XCTAssertEqual(seen.count, 200)
    }

    func testRandomCoversAlphabetBroadly() {
        // Грубая проверка, что байты раскладываются по алфавиту, а не
        // вырождаются в пару символов (например, при сломанной выборке).
        var used = Set<Character>()
        for _ in 0..<100 {
            used.formUnion(AppleNonce.random())
        }
        XCTAssertGreaterThan(used.count, 50)
    }

    // MARK: sha256Hex(_:) — то, что уходит в request.nonce

    func testSha256HexMatchesKnownVector() {
        // Эталон SHA256("abc") — общеизвестный вектор.
        XCTAssertEqual(
            AppleNonce.sha256Hex("abc"),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        )
        // И пустой строки — граничный случай.
        XCTAssertEqual(
            AppleNonce.sha256Hex(""),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        )
    }

    func testSha256HexIsDeterministic() {
        let nonce = AppleNonce.random()
        XCTAssertEqual(AppleNonce.sha256Hex(nonce), AppleNonce.sha256Hex(nonce))
        XCTAssertNotEqual(AppleNonce.sha256Hex(nonce), AppleNonce.sha256Hex(nonce + "x"))
    }

    func testSha256HexIsLowercaseHexOfFixedLength() {
        // Сервер сравнивает строки побайтово (constant-time), поэтому регистр
        // и длина — часть контракта, а не косметика.
        let hex = AppleNonce.sha256Hex(AppleNonce.random())
        XCTAssertEqual(hex.count, 64)
        XCTAssertTrue(hex.allSatisfy { $0.isHexDigit && !$0.isUppercase })
    }

    func testSha256HexHashesUTF8Bytes() {
        // Контракт с сервером: хешируется UTF-8 представление строки.
        let value = "nonce-Ю-._"
        let expected = SHA256.hash(data: Data(value.utf8))
            .map { String(format: "%02x", $0) }
            .joined()
        XCTAssertEqual(AppleNonce.sha256Hex(value), expected)
    }
}
