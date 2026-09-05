import XCTest
@testable import Splitty

/// Текст ошибки от сервера и язык интерфейса.
///
/// Бэкенд отвечает только по-русски: `Accept-Language` он не читает, а тексты
/// в `writeError` — русские литералы. При этом присланное конкретнее нашего:
/// под общим кодом `forbidden` сервер объясняет, ЧТО именно нельзя. Отсюда
/// правило: русскому интерфейсу — текст сервера, остальным — свой перевод по
/// коду. Набор идёт под `ru` (см. scheme), поэтому нерусская ветка проверяется
/// через шов `ServerTextLocale.override`.
final class ServerErrorLanguageTests: XCTestCase {

    override func tearDown() {
        ServerTextLocale.override = nil
        super.tearDown()
    }

    private func text(code: String, message: String, status: Int = 403) -> String {
        APIError.server(status: status, code: code, message: message).localizedDescription
    }

    func testRussianInterfaceKeepsTheSpecificServerText() {
        ServerTextLocale.override = "ru"
        // Наш ресурс на этот код сказал бы только «Нет доступа» — сервер
        // объясняет причину, и по-русски она читается.
        XCTAssertEqual(
            text(code: "forbidden", message: "Демонстрационный аккаунт удалить нельзя"),
            "Демонстрационный аккаунт удалить нельзя"
        )
    }

    func testOtherLanguagesGetTheTranslatedTextInstead() {
        for language in ["en", "de", "es", "fr"] {
            ServerTextLocale.override = language
            let shown = text(code: "forbidden", message: "Демонстрационный аккаунт удалить нельзя")
            XCTAssertEqual(shown, String(localized: "Нет доступа"),
                           "\(language): показан текст сервера вместо перевода")
            XCTAssertFalse(shown.contains("аккаунт"), "\(language): в тексте осталась кириллица сервера")
        }
    }

    func testUnknownCodeFallsBackToStatusRatherThanRussian() {
        ServerTextLocale.override = "de"
        // Кода клиент не знает, перевести нечем — но и русскую фразу немцу
        // показывать нельзя: остаётся номер статуса.
        XCTAssertEqual(
            text(code: "brand_new_code", message: "так делать пока нельзя", status: 409),
            String(localized: "Ошибка сервера (409)")
        )
    }

    /// Каждый код, который умеет слать бэкенд, переводится своим текстом.
    /// Список снят с `writeError()` в internal/rest.
    func testEveryBackendCodeHasItsOwnText() {
        ServerTextLocale.override = "en"
        let codes = [
            "internal", "validation", "not_found", "unavailable", "unauthorized",
            "rate_limited", "conflict", "forbidden", "unsupported_media", "too_large",
            "identity_taken", "has_operations", "email_taken", "stale_operation",
            "provider_rejected", "not_a_friend", "last_member", "last_identity",
            "invalid_password", "invalid_credentials", "invalid_code",
            "identity_already_linked", "ai_disabled", "room_too_large",
            "ai_quota_exceeded", "receipt_belongs_to_other_account", "subscriptions_disabled",
        ]
        let generic = String(localized: "Ошибка сервера (500)")
        let unmapped = codes.filter { text(code: $0, message: "", status: 500) == generic }
        XCTAssertEqual(unmapped, [], "коды без своего текста — человек увидит номер статуса")
    }
}
