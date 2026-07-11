import XCTest
@testable import Splitty

/// Тесты правила «можно ли редактировать офлайн» (зафиксированный дизайн v1):
/// офлайн заблокирована ТОЛЬКО правка синхронизированной операции;
/// создание нового расхода и правка локальной записи outbox работают офлайн.
final class OfflineEditPolicyTests: XCTestCase {
    // MARK: офлайн

    func testOfflineCreateIsAllowed() {
        XCTAssertFalse(
            AddExpenseViewModel.isSaveBlockedOffline(
                isOnline: false,
                isEditingSyncedOperation: false,
                isEditingLocalEntry: false
            ),
            "Офлайн-создание расхода должно быть разрешено (уходит в outbox)"
        )
    }

    func testOfflineEditOfLocalEntryIsAllowed() {
        XCTAssertFalse(
            AddExpenseViewModel.isSaveBlockedOffline(
                isOnline: false,
                isEditingSyncedOperation: false,
                isEditingLocalEntry: true
            ),
            "Правка неотправленной записи outbox офлайн должна быть разрешена"
        )
    }

    func testOfflineEditOfSyncedOperationIsBlocked() {
        XCTAssertTrue(
            AddExpenseViewModel.isSaveBlockedOffline(
                isOnline: false,
                isEditingSyncedOperation: true,
                isEditingLocalEntry: false
            ),
            "Синхронизированную операцию офлайн редактировать нельзя"
        )
    }

    // MARK: онлайн — ничего не блокируется

    func testOnlineNeverBlocks() {
        for synced in [false, true] {
            for local in [false, true] {
                XCTAssertFalse(
                    AddExpenseViewModel.isSaveBlockedOffline(
                        isOnline: true,
                        isEditingSyncedOperation: synced,
                        isEditingLocalEntry: local
                    ),
                    "Онлайн сохранение доступно всегда (synced=\(synced), local=\(local))"
                )
            }
        }
    }
}
