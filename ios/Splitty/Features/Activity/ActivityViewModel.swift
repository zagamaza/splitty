import Foundation
import Observation

/// VM вкладки «Уведомления»: лента операций всех групп с пагинацией offset/limit.
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
    /// Закреплённые карточки приглашений над лентой.
    var invites: [InviteCard] = []
    /// Непрочитанное: pending-приглашения + непрочитанные added + новые события.
    private(set) var unreadCount = 0
    /// Время формирования последнего ответа — его же отправляем при отметке
    /// прочитанного, чтобы не погасить то, что пришло позже.
    private var seenThrough: Date?
    /// true — показан офлайн-кеш (сеть недоступна), не свежие данные.
    private(set) var isFromCache = false
    /// Когда данные последний раз пришли С СЕРВЕРА — для подписи о свежести.
    private(set) var lastUpdatedAt: Date?
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

    /// Отметить прочитанным всё, что было в последнем ответе.
    ///
    /// Отправляем СЕРВЕРНЫЙ `seenThrough` из ответа, а не текущее время: между
    /// ответом и этим вызовом мог прийти новый расход, и «сейчас» погасило бы
    /// его, так и не показав человеку.
    func markSeen(session: SessionStore) async {
        // Публикуем счётчик, только когда за ним стоит настоящий ответ (сетевой
        // или из кеша). Лента могла не загрузиться — нет сети, ошибка сервера,
        // отменённый .task, — и тогда unreadCount ноль просто потому, что его
        // никто не считал: обнулив бейдж, приложение соврало бы о пустых
        // входящих, а отметку прочитанного при этом никто не отправлял.
        guard let through = seenThrough else { return }
        // Счётчик из последнего ответа — источник правды для бейджа, и записать
        // его надо ДО раннего выхода: если отметку уже поставили с другого
        // устройства, unreadCount придёт нулём, отмечать будет нечего, а бейдж
        // так и висел бы до следующего возврата из фона.
        session.unreadNotifications = unreadCount
        guard unreadCount > 0 else { return }
        do {
            try await session.api.markNotificationsSeen(through: through)
            unreadCount = invites.filter { $0.status == .pending }.count
            session.unreadNotifications = unreadCount
        } catch {
            // Не показываем алерт: отметка прочитанного — фоновое действие,
            // человек её не запрашивал явно.
            if error.isTaskCancellation { return }
        }
    }

    /// Принять приглашение вернуться в группу.
    func acceptInvite(_ card: InviteCard, session: SessionStore) async {
        await actOnInvite(card, session: session) {
            try await session.api.acceptInvite(roomId: card.roomId)
        }
    }

    /// Отклонить приглашение.
    func declineInvite(_ card: InviteCard, session: SessionStore) async {
        await actOnInvite(card, session: session) {
            try await session.api.declineInvite(roomId: card.roomId)
        }
    }

    /// Выйти из группы прямо с карточки «вас добавили».
    ///
    /// Кнопка обязана быть здесь: человека добавили, не спросив, и если
    /// единственное действие — «Открыть», отказаться можно только разыскав
    /// настройки группы.
    func leaveFromCard(_ card: InviteCard, session: SessionStore) async {
        await actOnInvite(card, session: session) {
            try await session.api.leaveRoom(roomId: card.roomId)
        }
    }

    private func actOnInvite(
        _ card: InviteCard,
        session: SessionStore,
        _ action: () async throws -> Void
    ) async {
        do {
            try await action()
            invites.removeAll { $0.roomId == card.roomId }
            // Данные комнат изменились — списки групп и друзей перечитаются.
            session.noteDataChanged()
        } catch {
            if error.isTaskCancellation { return }
            // Тот же текст, что в настройках группы: 409 сюда приходит только
            // с кнопки «Выйти», и он обязан объяснять путь наружу.
            errorMessage = leaveErrorText(error)
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
        await loadNextPage(repo: repo)
    }

    private func loadNextPage(repo: DataRepo) async {

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
            errorMessage = humanErrorText(error)
        }
    }

    private func reload(repo: DataRepo) async {
        do {
            let result = try await repo.notificationFeedFirstPage(limit: Self.pageSize) { [weak self] cached in
                // Кеш мгновенно — только пока в памяти нет более свежих данных.
                guard let self, self.items.isEmpty else { return }
                self.items = cached.items
                self.invites = cached.invites
                self.unreadCount = cached.unreadCount
                self.isFromCache = true
                self.hasMore = false
                self.state = .loaded
            }
            let feed = result.value
            let page = feed.items
            items = page
            invites = feed.invites
            unreadCount = feed.unreadCount
            seenThrough = feed.seenThrough
            isFromCache = result.isFromCache
            if !result.isFromCache { lastUpdatedAt = Date() }
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
                state = .failed(humanErrorText(error))
            } else {
                errorMessage = humanErrorText(error)
            }
        }
    }
}
