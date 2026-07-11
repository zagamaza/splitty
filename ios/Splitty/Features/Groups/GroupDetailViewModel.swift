import Foundation
import Observation

/// VM экрана группы: детали комнаты и операции, сгруппированные по месяцам.
/// Читает через `DataRepo` (офлайн-кеш): кеш показывается мгновенно,
/// сетевая ошибка при наличии кеша — без алерта (данные помечаются isFromCache).
@MainActor
@Observable
final class GroupDetailViewModel {
    /// Состояние первичной загрузки экрана.
    enum State {
        case loading
        case loaded
        case failed(String)
    }

    /// Секция операций одного месяца («Июль 2026»), новые месяцы первыми.
    struct MonthSection: Identifiable {
        let id: Date
        let title: String
        let operations: [Operation]
    }

    private(set) var state: State = .loading
    private(set) var room: RoomDetail?
    private(set) var sections: [MonthSection] = []
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Текст ошибки для alert (обновления поверх загруженных данных).
    var alertMessage: String?

    /// Загрузка/обновление экрана. Спиннер — только при первой загрузке без кеша.
    func load(repo: DataRepo, roomId: String) async {
        if room == nil {
            state = .loading
        }
        do {
            let result = try await repo.room(id: roomId) { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.room == nil else { return }
                self.apply(cached, isFromCache: true)
            }
            apply(result.value, isFromCache: result.isFromCache)
        } catch {
            // Отмена .task (ушли с экрана) — не ошибка.
            if error.isTaskCancellation { return }
            if room == nil {
                state = .failed(error.localizedDescription)
            } else {
                alertMessage = error.localizedDescription
            }
        }
    }

    private func apply(_ room: RoomDetail, isFromCache: Bool) {
        self.room = room
        self.isFromCache = isFromCache
        sections = Self.groupByMonth(room.operations)
        state = .loaded
    }

    /// Долги комнаты с участием пользователя (для «Погасить долг»).
    func debtsInvolving(_ userId: Int) -> [Debt] {
        (room?.debts ?? []).filter { $0.debtor.id == userId || $0.lender.id == userId }
    }

    /// Долги, где пользователь — должник (для статуса «Вы должны … Имя»).
    func debtsOwedBy(_ userId: Int) -> [Debt] {
        (room?.debts ?? []).filter { $0.debtor.id == userId }
    }

    private static func groupByMonth(_ operations: [Operation]) -> [MonthSection] {
        let calendar = Calendar.current
        let groups = Dictionary(grouping: operations) { operation in
            calendar.date(
                from: calendar.dateComponents([.year, .month], from: operation.createdAt)
            ) ?? operation.createdAt
        }
        return groups.keys.sorted(by: >).map { month in
            MonthSection(
                id: month,
                title: DateFmt.monthYear(month),
                operations: (groups[month] ?? []).sorted { $0.createdAt > $1.createdAt }
            )
        }
    }
}
