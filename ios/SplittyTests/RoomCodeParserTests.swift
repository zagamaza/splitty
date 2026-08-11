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

    func testSetStoresRoomId() {
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
    }

    func testIntentSurvivesReadingIt() {
        // Чтение намерение НЕ расходует: `RootView` читает его перед запросом,
        // а стирает по результату — иначе временный сбой сети («нет
        // соединения») уносил бы приглашение навсегда.
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
    }

    func testClearOnEmptyIsHarmless() {
        let pending = PendingJoin(defaults: defaults)
        pending.clear()
        XCTAssertNil(pending.roomId)
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

    func testOwnerSurvivesRestart() {
        // Владелец намерения обязан пережить смерть процесса ровно так же, как
        // и сам код комнаты: между протуханием сессии и следующим входом нас
        // штатно выгружают, и признак «кто тут был» в памяти уже потерян.
        PendingJoin(defaults: defaults).set("68a1b2c3d4e5f60718293a4b", ownerId: 7)
        XCTAssertEqual(PendingJoin(defaults: defaults).ownerUserId, 7)
    }

    func testReconcileAdoptsGuestIntent() {
        // Ссылку открыл гость — намерение достаётся первому вошедшему: это и
        // есть штатный путь приглашения.
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b")
        pending.reconcileOwner(7)
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
        XCTAssertEqual(pending.ownerUserId, 7)
    }

    func testReconcileKeepsOwnIntent() {
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b", ownerId: 7)
        pending.reconcileOwner(7)
        XCTAssertEqual(pending.roomId, "68a1b2c3d4e5f60718293a4b")
    }

    func testReconcileDropsForeignIntent() {
        // Приглашение предыдущего владельца устройства: без этой ветки новый
        // человек молча вступал бы в чужую приватную группу.
        let pending = PendingJoin(defaults: defaults)
        pending.set("68a1b2c3d4e5f60718293a4b", ownerId: 7)
        pending.reconcileOwner(8)
        XCTAssertNil(pending.roomId)
        XCTAssertNil(pending.ownerUserId)
    }

    func testReconcileOnEmptyDoesNotCreateOwner() {
        // Намерения нет — сводить нечего, и «владелец без намерения» на диске
        // потом ошибочно считался бы чужим для следующей ссылки.
        let pending = PendingJoin(defaults: defaults)
        pending.reconcileOwner(7)
        XCTAssertNil(pending.roomId)
        XCTAssertNil(pending.ownerUserId)
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

/// Исход попытки вступления: что стоит повторять, а что нет (PendingJoin.swift).
/// От этого зависит, переживёт ли приглашение неудачу — см.
/// `RootView.joinPendingRoom`.
final class TerminalJoinFailureTests: XCTestCase {
    func testMissingOrForbiddenRoomIsTerminal() {
        // Комнаты нет или доступ закрыт — повторять нечего, намерение стираем.
        XCTAssertTrue(APIError.server(status: 404, code: "not_found", message: "").isTerminalJoinFailure)
        XCTAssertTrue(APIError.server(status: 403, code: "forbidden", message: "").isTerminalJoinFailure)
    }

    func testTransientFailuresKeepIntent() {
        // Нет сети и 5xx — временное: приглашение обязано пережить сбой,
        // иначе открытая в метро ссылка теряется навсегда.
        XCTAssertFalse(APIError.transport(URLError(.notConnectedToInternet)).isTerminalJoinFailure)
        XCTAssertFalse(APIError.server(status: 500, code: "internal", message: "").isTerminalJoinFailure)
        XCTAssertFalse(APIError.server(status: 503, code: "", message: "").isTerminalJoinFailure)
        // 401 — тоже не терминальный: человек переавторизуется и дойдёт.
        XCTAssertFalse(APIError.server(status: 401, code: "unauthorized", message: "").isTerminalJoinFailure)
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

/// Коды распознавания и троттлинга в текстах ошибок.
///
/// Фолбэк срабатывает, только когда тело ответа пустое — то есть ответил
/// прокси, а не приложение. Именно в этом случае человеку и доставалась
/// голая «Ошибка сервера (429)», из которой ничего не следует.
final class ParseErrorCodeTests: XCTestCase {

    private func text(_ status: Int, _ code: String) -> String {
        humanErrorText(APIError.server(status: status, code: code, message: ""))
    }

    func testParseAndThrottlingCodesHaveTheirOwnTexts() {
        XCTAssertEqual(text(413, "too_large"), "Слишком большой запрос")
        XCTAssertEqual(text(415, "unsupported_media"), "Неподдерживаемый формат файла")
        XCTAssertEqual(text(429, "rate_limited"), "Слишком много запросов. Попробуйте позже")
        XCTAssertEqual(text(503, "ai_disabled"), "Распознавание сейчас недоступно")
    }

    func testUnknownCodeStillFallsBackToStatus() {
        XCTAssertEqual(text(500, "нет_такого_кода"), "Ошибка сервера (500)")
    }

    func testServerMessageWinsOverFallback() {
        // Сервер почти всегда шлёт свой текст — он и должен побеждать.
        let withBody = APIError.server(status: 429, code: "rate_limited", message: "Подождите минуту")
        XCTAssertEqual(humanErrorText(withBody), "Подождите минуту")
    }
}
