import Foundation

/// Результат кешируемого GET: значение + признак «показан офлайн-кеш»
/// (сеть недоступна, значение — последний успешный ответ, НЕ свежие данные).
struct CachedResult<T> {
    let value: T
    let isFromCache: Bool
}

/// Слой чтения с офлайн-кешем поверх `APIClient` (ключевые GET: список комнат,
/// деталь комнаты, друзья, первая страница активности, статистика, валюты,
/// профиль). Мутации кеша не имеют — для них внутренний `api`.
///
/// Семантика каждого метода («кеш сразу → сеть → перезапись»):
/// 1) кеш, если есть, отдаётся МГНОВЕННО через `onCached` (экран рисуется без
///    спиннера), затем идёт сетевой запрос;
/// 2) сеть ок → кеш перезаписан, возвращаются свежие данные (isFromCache=false);
/// 3) сетевая ошибка (transport/5xx) при наличии кеша → возвращается кеш
///    с isFromCache=true БЕЗ ошибки — экраны не показывают алерт;
/// 4) ошибка без кеша, ошибки 4xx и отмена задачи — пробрасываются как раньше.
@MainActor
final class DataRepo {
    /// Прямой доступ к API для мутаций и некешируемых запросов
    /// (страницы активности кроме первой и т.п.).
    let api: APIClient
    private let cache: OfflineStore

    init(api: APIClient, cache: OfflineStore) {
        self.api = api
        self.cache = cache
    }

    // MARK: Кешируемые GET (ключ = эндпоинт + параметры)

    func me(onCached: ((Me) -> Void)? = nil) async throws -> CachedResult<Me> {
        try await cachedFirst(key: "me", onCached: onCached) { try await self.api.me() }
    }

    func rooms(
        archived: Bool,
        onCached: (([RoomSummary]) -> Void)? = nil
    ) async throws -> CachedResult<[RoomSummary]> {
        try await cachedFirst(key: "rooms-archived-\(archived)", onCached: onCached) {
            try await self.api.rooms(archived: archived)
        }
    }

    func room(
        id: String,
        onCached: ((RoomDetail) -> Void)? = nil
    ) async throws -> CachedResult<RoomDetail> {
        try await cachedFirst(key: "room-\(id)", onCached: onCached) {
            try await self.api.room(id: id)
        }
    }

    func friends(
        onCached: (([FriendBalance]) -> Void)? = nil
    ) async throws -> CachedResult<[FriendBalance]> {
        try await cachedFirst(key: "friends", onCached: onCached) { try await self.api.friends() }
    }

    /// Кешируется ТОЛЬКО первая страница ленты (offset == 0);
    /// подгрузка следующих страниц — напрямую через `api.activity`.
    func activityFirstPage(
        limit: Int,
        onCached: (([ActivityItem]) -> Void)? = nil
    ) async throws -> CachedResult<[ActivityItem]> {
        try await cachedFirst(key: "activity-first", onCached: onCached) {
            try await self.api.activity(limit: limit, offset: 0)
        }
    }

    func statistics(
        roomId: String,
        onCached: ((Statistics) -> Void)? = nil
    ) async throws -> CachedResult<Statistics> {
        try await cachedFirst(key: "statistics-\(roomId)", onCached: onCached) {
            try await self.api.statistics(roomId: roomId)
        }
    }

    func currencies(
        onCached: (([CurrencyInfo]) -> Void)? = nil
    ) async throws -> CachedResult<[CurrencyInfo]> {
        try await cachedFirst(key: "currencies", onCached: onCached) {
            try await self.api.currencies()
        }
    }

    // MARK: - Внутреннее

    private func cachedFirst<T: Codable>(
        key: String,
        onCached: ((T) -> Void)?,
        fetch: () async throws -> T
    ) async throws -> CachedResult<T> {
        // await — хоп на актор кеша: дисковый I/O и JSON-кодек вне main.
        let cached: T? = await cache.read(key: key)
        if let cached {
            onCached?(cached)
        }
        do {
            let fresh = try await fetch()
            await cache.write(fresh, key: key)
            return CachedResult(value: fresh, isFromCache: false)
        } catch {
            // Отмена .task (ушли с экрана) — не «нет сети», пробрасываем:
            // VM молча игнорируют её по общей конвенции.
            if error.isTaskCancellation { throw error }
            if let cached, error.allowsCacheFallback {
                return CachedResult(value: cached, isFromCache: true)
            }
            throw error
        }
    }
}

private extension Error {
    /// Ошибки, при которых уместно показать офлайн-кеш: сетевые (нет соединения,
    /// таймаут) и 5xx. Ошибки 4xx (нет доступа, комната удалена…) и битый адрес
    /// сервера кешем не маскируются — пользователь должен их увидеть.
    var allowsCacheFallback: Bool {
        guard let apiError = self as? APIError else { return false }
        switch apiError {
        case .transport:
            return true
        case .server(let status, _, _):
            return status >= 500
        case .invalidURL, .decoding:
            return false
        }
    }
}
