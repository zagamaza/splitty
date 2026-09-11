import XCTest
@testable import Splitty

/// Отметка «приветствие показано» — по УСТАНОВКЕ, а не по номеру аккаунта.
///
/// Приветствие идёт до входа, и аккаунта в этот момент ещё не существует.
@MainActor
final class WelcomeSeenTests: XCTestCase {

    private let key = "splitty.introSeen"
    private let legacyKey = "splitty.welcomeSeenAccounts"
    private let tokenKey = "splitty.apiToken"

    /// Токен тоже чистится, и это не перестраховка: сохранённая сессия —
    /// ВТОРОЕ условие миграции, и оставшийся в Keychain симулятора токен от
    /// соседнего теста делал бы «чистую установку» уже отмеченной.
    override func setUp() {
        super.setUp()
        reset()
    }

    override func tearDown() {
        reset()
        super.tearDown()
    }

    private func reset() {
        UserDefaults.standard.removeObject(forKey: key)
        UserDefaults.standard.removeObject(forKey: legacyKey)
        KeychainStore.delete(key: tokenKey)
    }

    func testFreshInstallHasNotSeenIt() {
        XCTAssertFalse(SessionStore().hasSeenIntro)
    }

    func testMarkedInstallDoesNotSeeItAgain() {
        let session = SessionStore()
        session.markIntroSeen()
        XCTAssertTrue(session.hasSeenIntro)
        // Переживает перезапуск: отметка лежит в UserDefaults, а не в памяти.
        XCTAssertTrue(SessionStore().hasSeenIntro)
    }

    /// Перенос прежней отметки: этот человек приветствие уже закрывал.
    func testLegacyMarkIsMigrated() {
        UserDefaults.standard.set(["1"], forKey: legacyKey)
        XCTAssertTrue(SessionStore().hasSeenIntro)
    }

    /// Главный случай миграции, и он НЕ покрывается прежним хранилищем.
    ///
    /// Прежняя отметка ставилась только при закрытии показанного приветствия, а
    /// показывалось оно лишь тому, у кого нет ни одной тусы. Значит у ветерана,
    /// заведшего тусу раньше, чем приветствие появилось, хранилище пустое. Пока
    /// он авторизован, это незаметно; стоит токену протухнуть — и он получил бы
    /// рассказ «что такое группа» как новичок.
    ///
    /// Признак «эта установка уже была вошедшей» здесь один — сам токен.
    func testVeteranWithSessionButWithoutLegacyMarkIsMigrated() {
        XCTAssertFalse(SessionStore().hasSeenIntro, "предусловие: чистая установка")
        // Отметка могла записаться при проверке предусловия — снимаем её, чтобы
        // дальше сработал именно токен, а не она.
        UserDefaults.standard.removeObject(forKey: key)

        _ = KeychainStore.save("token-from-previous-build", key: tokenKey)

        XCTAssertTrue(
            SessionStore().hasSeenIntro,
            "ветеран с пустым прежним хранилищем получит приветствие после разлогина"
        )
    }
}
