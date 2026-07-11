import XCTest
@testable import Splitty

/// Нормализация и валидация одноразового кода входа (LoginCode, LoginView.swift).
/// Чистая строковая логика — без сети и без SessionStore.
final class LoginCodeTests: XCTestCase {
    // MARK: normalize(_:) — верхний регистр + без пробельных символов

    func testNormalizeUppercases() {
        XCTAssertEqual(LoginCode.normalize("abcd2345"), "ABCD2345")
    }

    func testNormalizeStripsWhitespaceAndNewlines() {
        XCTAssertEqual(LoginCode.normalize("  ABCD 2345\n"), "ABCD2345")
        XCTAssertEqual(LoginCode.normalize("\tab cd\u{00A0}23 45 "), "ABCD2345")
    }

    func testNormalizeEmpty() {
        XCTAssertEqual(LoginCode.normalize(""), "")
        XCTAssertEqual(LoginCode.normalize("   \n\t"), "")
    }

    func testNormalizeKeepsNonWhitespaceAsIs() {
        // Не выкидываем «странные» символы — сервер сам ответит 401 invalid_code.
        XCTAssertEqual(LoginCode.normalize("ab-cd_23"), "AB-CD_23")
    }

    // MARK: isValid(_:) — кнопка активна от minLength значимых символов

    func testIsValidRejectsShortCodes() {
        XCTAssertFalse(LoginCode.isValid(""))
        XCTAssertFalse(LoginCode.isValid("ABC12"))
        // Пробелы не считаются: значимых символов всего 5.
        XCTAssertFalse(LoginCode.isValid(" A B C 1 2 "))
    }

    func testIsValidAcceptsMinLength() {
        XCTAssertEqual(LoginCode.minLength, 6)
        XCTAssertTrue(LoginCode.isValid("ABC123"))
    }

    func testIsValidAcceptsBotFormat() {
        // Формат кода бота: 8 символов, буквы+цифры, регистр не важен.
        XCTAssertTrue(LoginCode.isValid("ABCD2345"))
        XCTAssertTrue(LoginCode.isValid("abcd2345"))
        XCTAssertTrue(LoginCode.isValid(" abcd 2345 "))
    }
}
