import CryptoKit
import XCTest
@testable import Splitty

/// Nonce для Sign in with Apple (AppleNonce.swift): генерация сырого значения
/// и его SHA256-hex. Чистая логика — без сети и без ASAuthorization.
final class AppleNonceTests: XCTestCase {
    // MARK: random() — длина и кодировка

    func testRandomIsHexOfFixedLength() {
        // 32 байта = 64 hex-символа. Длина зафиксирована тестом, потому что
        // энтропия nonce — единственное, что защищает вход от повторного
        // использования чужого токена.
        XCTAssertGreaterThanOrEqual(AppleNonce.byteCount, 16)
        XCTAssertEqual(AppleNonce.random().count, AppleNonce.byteCount * 2)
    }

    func testRandomUsesLowercaseHexOnly() {
        // Hex безопасен и в JSON-теле, и в JWT-claim, и в URL: экранирование
        // по пути до Apple и обратно ничего не поменяет.
        for _ in 0..<50 {
            let nonce = AppleNonce.random()
            XCTAssertTrue(
                nonce.allSatisfy { $0.isASCII && $0.isHexDigit && !$0.isUppercase },
                "nonce содержит символ вне нижнего регистра hex: \(nonce)"
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

    func testRandomCoversHexDigitsBroadly() {
        // Грубая проверка, что байты действительно случайны, а не вырождаются
        // в пару символов (например, при сломанном источнике случайности).
        var used = Set<Character>()
        for _ in 0..<100 {
            used.formUnion(AppleNonce.random())
        }
        XCTAssertEqual(used.count, 16)
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
