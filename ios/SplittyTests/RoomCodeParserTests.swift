import XCTest
@testable import Splitty

/// Разбор ссылки-приглашения (RoomCodeParser.swift). Это единственный парсер
/// кода в приложении: через него ходят и экран «Присоединиться», и обработчик
/// диплинка, поэтому каждый поддерживаемый формат зафиксирован тестом.
final class RoomCodeParserTests: XCTestCase {
    /// Валидный код — 24 hex-символа (mongo ObjectID).
    private let code = "68a1b2c3d4e5f60718293a4b"

    // MARK: Новый формат — universal link и кастомная схема

    func testParsesUniversalLink() {
        XCTAssertEqual(RoomCodeParser.roomId(from: "https://splitty.app/join/\(code)"), code)
    }

    func testParsesCustomScheme() {
        // Кнопка «Открыть в приложении» на странице /join.
        XCTAssertEqual(RoomCodeParser.roomId(from: "splitty://join/\(code)"), code)
    }

    func testParsesURLValue() throws {
        // Перегрузка для URL — ею пользуется обработчик диплинка.
        let url = try XCTUnwrap(URL(string: "https://splitty.app/join/\(code)"))
        XCTAssertEqual(RoomCodeParser.roomId(from: url), code)
    }

    func testIgnoresLinkTail() {
        // Слеш, query и фрагмент — «хвост» ссылки, а не часть кода.
        XCTAssertEqual(RoomCodeParser.roomId(from: "https://splitty.app/join/\(code)/"), code)
        XCTAssertEqual(RoomCodeParser.roomId(from: "https://splitty.app/join/\(code)?utm=tg"), code)
        XCTAssertEqual(RoomCodeParser.roomId(from: "https://splitty.app/join/\(code)#top"), code)
    }

    // MARK: Легаси-формат бота

    func testParsesTelegramStartLink() {
        XCTAssertEqual(
            RoomCodeParser.roomId(from: "https://t.me/split_money_bot?start=room\(code)"),
            code
        )
        // Без схемы — ровно так ссылку и присылают текстом.
        XCTAssertEqual(
            RoomCodeParser.roomId(from: "t.me/split_money_bot?start=room\(code)"),
            code
        )
    }

    func testParsesRoomPrefixedCode() {
        XCTAssertEqual(RoomCodeParser.roomId(from: "room\(code)"), code)
    }

    // MARK: Голый код

    func testParsesBareCode() {
        XCTAssertEqual(RoomCodeParser.roomId(from: code), code)
    }

    func testTrimsWhitespace() {
        // Вставка из буфера обычно приезжает с переводом строки.
        XCTAssertEqual(RoomCodeParser.roomId(from: "  \(code)\n"), code)
    }

    func testUppercaseCodeIsNormalized() {
        // ObjectID из Go всегда в нижнем регистре — приводим и ручной ввод,
        // чтобы один код не выглядел двумя разными.
        XCTAssertEqual(RoomCodeParser.roomId(from: code.uppercased()), code)
    }

    // MARK: Мусор → nil

    func testRejectsForeignURL() {
        XCTAssertNil(RoomCodeParser.roomId(from: "https://example.com/hello"))
        XCTAssertNil(RoomCodeParser.roomId(from: "https://splitty.app/about"))
    }

    func testRejectsGarbage() {
        XCTAssertNil(RoomCodeParser.roomId(from: ""))
        XCTAssertNil(RoomCodeParser.roomId(from: "   "))
        XCTAssertNil(RoomCodeParser.roomId(from: "привет"))
        XCTAssertNil(RoomCodeParser.roomId(from: "https://splitty.app/join/"))
    }

    func testRejectsWrongLength() {
        // Недобранный код — не код: сервер всё равно ответит 404, а экран
        // может сказать «не похоже на код» ещё до запроса.
        XCTAssertNil(RoomCodeParser.roomId(from: String(code.dropLast())))
        XCTAssertNil(RoomCodeParser.roomId(from: code + "ab"))
        XCTAssertNil(RoomCodeParser.roomId(from: "https://splitty.app/join/\(code)ab"))
    }

    func testRejectsNonAsciiHexDigits() {
        // Полноширинные формы Character.isHexDigit считает hex-цифрами —
        // такой «код» ушёл бы на сервер мусором.
        let fullwidth = String(repeating: "\u{FF10}", count: RoomCodeParser.codeLength)
        XCTAssertNil(RoomCodeParser.roomId(from: fullwidth))
    }

    func testMarkerWinsOverBareParsing() {
        // В ссылке есть и маркер, и посторонние hex-подобные куски —
        // код берётся строго после маркера.
        XCTAssertEqual(
            RoomCodeParser.roomId(from: "https://abc.example/x?start=room\(code)"),
            code
        )
    }
}

/// Отложенное намерение вступить в группу (PendingJoin.swift).
final class PendingJoinTests: XCTestCase {
    private var defaults: UserDefaults!
    private let suite = "splitty.tests.pendingJoin"

    override func setUp() {
        super.setUp()
        UserDefaults.standard.removePersistentDomain(forName: suite)
        defaults = UserDefaults(suiteName: suite)
    }

    override func tearDown() {
        UserDefaults.standard.removePersistentDomain(forName: suite)
        defaults = nil
        super.tearDown()
    }

    func testSetThenTakeReturnsRoomId() {
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.take(), "68a1b2c3d4e5f60718293a4b")
    }

    func testTakeClearsIntent() {
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b")
        _ = pending.take()
        // Второй take — уже nil: вступление выполняется ровно один раз.
        XCTAssertNil(pending.roomId)
        XCTAssertNil(pending.take())
    }

    func testTakeOnEmptyIsNil() {
        XCTAssertNil(PendingJoin(defaults: defaults).take())
    }

    func testIntentSurvivesRestart() {
        // Вход через Google/Apple уводит в другое приложение, и нас там могут
        // выгрузить — намерение обязано пережить перезапуск.
        PendingJoin(defaults: defaults).set("68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(PendingJoin(defaults: defaults).roomId, "68a1b2c3d4e5f60718293a4b")
    }

    func testClearRemovesFromStorage() {
        PendingJoin(defaults: defaults).set("68a1b2c3d4e5f60718293a4b")
        PendingJoin(defaults: defaults).clear()
        XCTAssertNil(PendingJoin(defaults: defaults).roomId)
    }

    @MainActor
    func testLogoutClearsSharedIntent() {
        // Иначе следующий вошедший на устройстве человек молча вступил бы
        // в группу предыдущего.
        defer { PendingJoin.shared.clear() }
        PendingJoin.shared.set("68a1b2c3d4e5f60718293a4b")
        SessionStore().logout()
        XCTAssertNil(PendingJoin.shared.roomId)
    }
}

/// Человеческие тексты ошибок вступления по ссылке (PendingJoin.swift).
final class JoinLinkErrorTextTests: XCTestCase {
    func testNotFoundExplainsDeletedRoom() {
        let text = joinLinkErrorText(
            APIError.server(status: 404, code: "not_found", message: "комната не найдена")
        )
        XCTAssertTrue(text.contains("удалили"), text)
    }

    func testForbiddenExplainsNoAccess() {
        let text = joinLinkErrorText(
            APIError.server(status: 403, code: "forbidden", message: "вы не участник этой комнаты")
        )
        XCTAssertTrue(text.contains("Нет доступа"), text)
    }

    func testTransportFallsBackToHumanText() {
        // Сетевые сбои уже переведены на человеческий в humanErrorText —
        // второй словарь для них не заводим.
        let text = joinLinkErrorText(
            APIError.transport(URLError(.notConnectedToInternet))
        )
        XCTAssertEqual(text, humanErrorText(APIError.transport(URLError(.notConnectedToInternet))))
        XCTAssertTrue(text.contains("интернет"), text)
    }

    func testUnknownServerErrorKeepsServerMessage() {
        let text = joinLinkErrorText(
            APIError.server(status: 500, code: "internal", message: "не удалось присоединиться к комнате")
        )
        XCTAssertEqual(text, "не удалось присоединиться к комнате")
    }
}
