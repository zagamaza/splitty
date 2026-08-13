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

    /// Секции, суженные до перечисленных операций; пустые месяцы выпадают.
    ///
    /// Отдельной функцией — ради теста: фильтр включается с другой вкладки, и
    /// «показали не то» заметить глазами тем труднее, чем длиннее список.
    nonisolated static func sectionsKeepingOnly(_ sections: [MonthSection], ids: Set<String>) -> [MonthSection] {
        sections.compactMap { section in
            let ops = section.operations.filter { ids.contains($0.id) }
            return ops.isEmpty ? nil : MonthSection(id: section.id, title: section.title, operations: ops)
        }
    }

    private(set) var state: State = .loading
    private(set) var room: RoomDetail?
    private(set) var sections: [MonthSection] = []
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Текст ошибки для alert (обновления поверх загруженных данных).
    var alertMessage: String?

    /// Поколение загрузки: .task, .refreshable и onChange(dataVersion) могут
    /// идти одновременно, и «последний ответивший побеждает» возвращал на экран
    /// УСТАРЕВШИЕ балансы сразу после мутации. Применяем только самую свежую.
    private var loadGeneration = 0

    /// Загрузка/обновление экрана. Спиннер — только при первой загрузке без кеша.
    func load(repo: DataRepo, roomId: String) async {
        loadGeneration += 1
        let generation = loadGeneration
        if room == nil {
            state = .loading
        }
        do {
            let result = try await repo.room(id: roomId) { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.room == nil, generation == self.loadGeneration else { return }
                self.apply(cached, isFromCache: true)
            }
            guard generation == loadGeneration else { return }
            apply(result.value, isFromCache: result.isFromCache)
            if !result.isFromCache {
                await markSeen(repo: repo, room: result.value)
            }
        } catch {
            guard generation == loadGeneration else { return }
            // Отмена .task (ушли с экрана) — не ошибка.
            if error.isTaskCancellation { return }
            if room == nil {
                state = .failed(humanErrorText(error))
            } else {
                alertMessage = humanErrorText(error)
            }
        }
    }

    /// Открытая группа прочитана: гасим счётчик на её карточке в списке.
    ///
    /// Отправляем `seenThrough` ИЗ ОТВЕТА, а не своё «сейчас», — иначе погас бы
    /// и расход, добавленный между ответом и отметкой. Кешированную комнату не
    /// отмечаем: её `seenThrough` описывает прошлый визит, а офлайн запрос
    /// всё равно не уйдёт.
    ///
    /// Best-effort и молча: человек этого действия не просил, и алерт поверх
    /// открытой группы был бы шумом — счётчик погаснет при следующем заходе.
    /// Список обновится сам: у вкладки «Группы» перезагрузка висит на .task,
    /// который срабатывает и при возврате с этого экрана.
    private func markSeen(repo: DataRepo, room: RoomDetail) async {
        guard let through = room.seenThrough else { return }
        try? await repo.api.markRoomSeen(roomId: room.id, through: through)
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
