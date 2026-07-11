import Foundation
import Observation

/// VM вкладки «Активность»: лента операций всех групп с пагинацией offset/limit.
/// Первая страница читается через `DataRepo` (офлайн-кеш): кеш показывается
/// мгновенно, сетевая ошибка при наличии кеша — без алерта. Следующие
/// страницы не кешируются (офлайн лента ограничена первой страницей).
@MainActor
@Observable
final class ActivityViewModel {
    /// Состояние загрузки ленты.
    enum LoadState {
        case idle
        case loading
        case loaded
        case failed(String)
    }

    private static let pageSize = 30
    /// За сколько строк до конца списка начинать подгрузку следующей страницы.
    private static let prefetchThreshold = 5

    var state: LoadState = .idle
    var items: [ActivityItem] = []
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Ошибка обновления/подгрузки, когда лента уже показана (для alert).
    var errorMessage: String?
    private(set) var isLoadingMore = false
    private var hasMore = true

    /// Первичная загрузка; при повторном появлении вкладки — тихое обновление.
    func load(repo: DataRepo) async {
        switch state {
        case .loading:
            return
        case .loaded:
            await reload(repo: repo)
        case .idle, .failed:
            state = .loading
            await reload(repo: repo)
        }
    }

    /// Pull-to-refresh: перезагрузка первой страницы без скрытия ленты.
    func refresh(repo: DataRepo) async {
        await reload(repo: repo)
    }

    /// Подгружает следующую страницу, когда пользователь долистал до `item`.
    func loadMoreIfNeeded(repo: DataRepo, current item: ActivityItem) async {
        guard case .loaded = state, hasMore, !isLoadingMore else { return }
        guard let index = items.firstIndex(where: { $0.id == item.id }),
              index >= items.count - Self.prefetchThreshold else { return }

        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await repo.api.activity(limit: Self.pageSize, offset: items.count)
            // Страховка от дублей при сдвиге offset (новые операции сверху).
            let known = Set(items.map(\.id))
            items += page.filter { !known.contains($0.id) }
            hasMore = page.count == Self.pageSize
        } catch {
            // Отмена .task (строка уехала за экран, ушли с вкладки) — не ошибка,
            // молча выходим без ложного «Нет соединения с сервером».
            if error.isTaskCancellation { return }
            errorMessage = error.localizedDescription
        }
    }

    private func reload(repo: DataRepo) async {
        do {
            let result = try await repo.activityFirstPage(limit: Self.pageSize) { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.items.isEmpty else { return }
                self.items = cached
                self.isFromCache = true
                self.hasMore = false
                self.state = .loaded
            }
            let page = result.value
            items = page
            isFromCache = result.isFromCache
            // Из кеша дальше не листаем (следующие страницы не кешируются) —
            // иначе офлайн-прокрутка до конца ленты давала бы ложный алерт.
            hasMore = result.isFromCache ? false : page.count == Self.pageSize
            state = .loaded
        } catch {
            if error.isTaskCancellation {
                // Отмена посреди первичной загрузки: откатываемся в idle,
                // чтобы следующий .task загрузил ленту заново.
                if case .loading = state { state = .idle }
                return
            }
            if items.isEmpty {
                state = .failed(error.localizedDescription)
            } else {
                errorMessage = error.localizedDescription
            }
        }
    }
}
