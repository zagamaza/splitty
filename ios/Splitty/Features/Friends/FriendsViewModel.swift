import Foundation
import Observation

/// VM вкладки «Друзья»: список друзей с нетто-балансами по всем группам.
/// Читает через `DataRepo` (офлайн-кеш): кеш показывается мгновенно,
/// сетевая ошибка при наличии кеша — без алерта (данные помечаются isFromCache).
@MainActor
@Observable
final class FriendsViewModel {
    /// Состояние загрузки списка.
    enum LoadState {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    var state: LoadState = .idle
    var friends: [FriendBalance] = []
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Когда данные последний раз пришли С СЕРВЕРА — для подписи о свежести.
    private(set) var lastUpdatedAt: Date?
    /// Ошибка обновления, когда список уже показан (для alert).
    var errorMessage: String?

    /// Нетто-баланс по всем друзьям ПО ВАЛЮТАМ (разные валюты не складываются):
    /// без нулей, по убыванию |суммы| — первая валюта «основная».
    /// Пусто — все долги погашены.
    var totals: [CurrencySum] {
        aggregateByCurrency(friends.flatMap(\.totalsByCurrency))
    }

    /// Первичная загрузка; при повторном появлении вкладки — тихое обновление.
    func load(repo: DataRepo) async {
        switch state {
        case .loading:
            return
        case .loaded:
            await fetch(repo: repo)
        case .idle, .failed:
            state = .loading
            await fetch(repo: repo)
        }
    }

    /// Pull-to-refresh: обновление без скрытия списка.
    func refresh(repo: DataRepo) async {
        await fetch(repo: repo)
    }

    private func fetch(repo: DataRepo) async {
        do {
            let result = try await repo.friends { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.friends.isEmpty else { return }
                self.friends = cached
                self.isFromCache = true
                self.state = .loaded
            }
            friends = result.value
            isFromCache = result.isFromCache
            if !result.isFromCache { lastUpdatedAt = Date() }
            state = .loaded
        } catch {
            if error.isTaskCancellation {
                // Отмена .task (ушли с вкладки) — не ошибка; из loading
                // откатываемся в idle, чтобы следующий .task загрузил заново.
                if case .loading = state { state = .idle }
                return
            }
            if friends.isEmpty {
                state = .failed(humanErrorText(error))
            } else {
                errorMessage = humanErrorText(error)
            }
        }
    }
}
