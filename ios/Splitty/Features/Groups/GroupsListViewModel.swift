import Foundation
import Observation

/// VM вкладки «Группы»: активные и архивные группы, суммарный баланс.
/// Читает через `DataRepo` (офлайн-кеш): кеш показывается мгновенно,
/// сетевая ошибка при наличии кеша — без алерта (данные помечаются isFromCache).
@MainActor
@Observable
final class GroupsListViewModel {
    /// Состояние первичной загрузки списка.
    enum State {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    private(set) var state: State = .idle
    /// Активные (неархивные) группы, новые первыми.
    private(set) var rooms: [RoomSummary] = []
    /// Архивные группы (грузятся при заходе в раздел «Архив»).
    private(set) var archivedRooms: [RoomSummary] = []
    private(set) var isArchiveLoading = false
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Когда данные последний раз пришли С СЕРВЕРА. Нужен подписи о свежести:
    /// признак «из кеша» вычислялся и раньше, но никуда не попадал — человек
    /// смотрел на старые суммы, ничего об этом не зная.
    private(set) var lastUpdatedAt: Date?
    /// Текст ошибки для alert (обновления поверх загруженного списка, мутации).
    var alertMessage: String?

    /// Суммарный баланс по всем активным группам ПО ВАЛЮТАМ (суммы разных
    /// валют не складываются): без нулей, по убыванию |суммы| — первая
    /// валюта «основная». Пусто — все долги погашены.
    var totals: [CurrencySum] {
        aggregateByCurrency(rooms.map { CurrencySum(currency: $0.currency, sum: $0.myBalance) })
    }

    /// Загрузка/обновление списка групп. Спиннер — только пока список пуст
    /// и кеша нет, дальше обновляемся тихо, ошибки показываем alert'ом.
    func load(repo: DataRepo) async {
        if case .loaded = state {} else {
            state = .loading
        }
        do {
            let result = try await repo.rooms(archived: false) { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.rooms.isEmpty else { return }
                self.rooms = cached
                self.isFromCache = true
                self.state = .loaded
            }
            rooms = result.value
            isFromCache = result.isFromCache
            if !result.isFromCache { lastUpdatedAt = Date() }
            state = .loaded
            // Список пуст — проверяем архив: заархивировав ПОСЛЕДНЮЮ группу,
            // человек терял единственный вход в архив, и достать её обратно
            // было нельзя. Строка «Архив» рисуется по этому списку
            if rooms.isEmpty {
                await loadArchive(repo: repo)
            }
        } catch {
            if error.isTaskCancellation {
                // Отмена .task (ушли с экрана) — не ошибка; из loading
                // откатываемся в idle, чтобы следующий .task загрузил заново.
                if rooms.isEmpty { state = .idle }
                return
            }
            if rooms.isEmpty {
                state = .failed(humanErrorText(error))
            } else {
                alertMessage = humanErrorText(error)
            }
        }
    }

    /// Загрузка архивных групп (для экрана «Архив»).
    func loadArchive(repo: DataRepo) async {
        if archivedRooms.isEmpty {
            isArchiveLoading = true
        }
        defer { isArchiveLoading = false }
        do {
            let result = try await repo.rooms(archived: true) { [weak self] cached in
                guard let self, self.archivedRooms.isEmpty else { return }
                self.archivedRooms = cached
                self.isArchiveLoading = false
            }
            archivedRooms = result.value
        } catch {
            if error.isTaskCancellation { return }
            alertMessage = humanErrorText(error)
        }
    }

    /// Убирает группу в архив и обновляет оба списка.
    ///
    /// Архивация здесь своя, а не через экран настроек группы: попасть в неё
    /// можно было только открыв тусу и пройдя две вкладки вглубь.
    func archive(repo: DataRepo, roomId: String) async {
        do {
            try await repo.api.archiveRoom(id: roomId)
            Analytics.shared.track(.roomArchived)
            rooms = try await repo.rooms(archived: false).value
            archivedRooms = try await repo.rooms(archived: true).value
            state = .loaded
        } catch {
            if error.isTaskCancellation { return }
            alertMessage = humanErrorText(error)
        }
    }

    /// Возвращает группу из архива и обновляет оба списка.
    func unarchive(repo: DataRepo, roomId: String) async {
        do {
            try await repo.api.unarchiveRoom(id: roomId)
            Analytics.shared.track(.roomUnarchived)
            archivedRooms = try await repo.rooms(archived: true).value
            rooms = try await repo.rooms(archived: false).value
            state = .loaded
        } catch {
            if error.isTaskCancellation { return }
            alertMessage = humanErrorText(error)
        }
    }
}
