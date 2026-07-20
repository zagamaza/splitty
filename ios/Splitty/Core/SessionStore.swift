import Foundation
import Observation

/// Сессия пользователя: JWT-токен (Keychain), профиль,
/// базовый URL сервера (UserDefaults).
@Observable
final class SessionStore {
    /// Дефолтный сервер — прод, в том числе на симуляторе (меняется в поле
    /// «Сервер» на экране входа). UI-тестам нужен локальный бэкенд — они
    /// передают его через launch environment `SPLITTY_BASE_URL`; переменная
    /// имеет приоритет над UserDefaults, чтобы прогоны были детерминированы.
    static let defaultBaseURL = "http://138.124.18.189:18002"

    private static let baseURLKey = "splitty.baseURL"
    private static let tokenKey = "splitty.apiToken"

    /// Профиль текущего пользователя (nil до первого refreshMe/login).
    var me: Me?

    /// Монитор сети (NWPathMonitor): офлайн-баннер и офлайн-ветки экранов.
    let network = NetworkMonitor()

    /// Файловый read-кеш последних успешных GET (Application Support/SplittyCache).
    let cache = OfflineStore()

    /// Outbox локальных операций (офлайн-создание расходов, outbox.json).
    let outbox = OutboxStore()

    /// Кеш аватаров из Telegram (in-memory, чистится при logout).
    @MainActor let avatars = AvatarStore()

    /// true — есть сеть (см. `NetworkMonitor`).
    var isOnline: Bool { network.isOnline }

    /// Базовый URL бэкенда, персистится в UserDefaults.
    var baseURLString: String {
        didSet {
            UserDefaults.standard.set(baseURLString, forKey: Self.baseURLKey)
        }
    }

    /// JWT-токен, персистится в Keychain.
    private(set) var token: String? {
        didSet {
            if let token {
                KeychainStore.save(token, key: Self.tokenKey)
            } else {
                KeychainStore.delete(key: Self.tokenKey)
            }
        }
    }

    var isAuthenticated: Bool { token != nil }

    /// Версия данных на сервере. Увеличивается после каждой мутации
    /// (сохранение/удаление операции, платёж, создание/архив комнаты) —
    /// экраны-списки перезагружаются по `.onChange(of: session.dataVersion)`.
    private(set) var dataVersion = 0

    /// Отметить мутацию данных: все подписанные экраны перезагрузятся.
    func noteDataChanged() {
        dataVersion += 1
    }

    /// URL сервера из настроек. nil — строка невалидна (пустая, с опечаткой,
    /// без http/https или хоста): дефолтом её НЕ подменяем — пользователь
    /// получит ошибку «Некорректный адрес сервера», а не запросы не туда.
    var serverURL: URL? {
        let trimmed = baseURLString.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty,
              let url = URL(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              url.host() != nil
        else {
            return nil
        }
        return url
    }

    /// Всегда актуальный API-клиент (с текущими token/baseURL).
    /// Любой ответ 401 сбрасывает сессию (токен чистится из Keychain).
    var api: APIClient {
        let client = APIClient(baseURL: serverURL, token: token)
        // Протухший токен — НЕ полный logout: очередь неотправленных офлайн-расходов
        // должна пережить переавторизацию, иначе фоновый GET с просроченным JWT
        // молча стирает то, что пользователь ввёл без сети.
        client.onUnauthorized = { [weak self] in
            Task { @MainActor in
                self?.expireSession()
            }
        }
        return client
    }

    /// Слой чтения с офлайн-кешем поверх текущего `api` («кеш сразу → сеть →
    /// перезапись»; сетевая ошибка при наличии кеша → кеш без алерта).
    @MainActor
    var repo: DataRepo {
        DataRepo(api: api, cache: cache)
    }

    /// Синхронизация outbox (FIFO, сериализовано — см. `OutboxStore.sync`).
    /// Триггеры: сеть снова появилась, приложение стало активным,
    /// pull-to-refresh, сохранение формы расхода. Если отправлена хотя бы
    /// одна операция — единая инвалидация (экраны перечитают данные).
    @MainActor
    func syncOutbox() async {
        guard isAuthenticated else { return }
        if await outbox.sync(api: api) {
            noteDataChanged()
        }
    }

    init() {
        // Прямое присваивание в init не дергает didSet — override из окружения
        // не затирает сохранённый пользователем адрес в UserDefaults.
        baseURLString = ProcessInfo.processInfo.environment["SPLITTY_BASE_URL"]
            ?? UserDefaults.standard.string(forKey: Self.baseURLKey)
            ?? Self.defaultBaseURL
        token = KeychainStore.read(key: Self.tokenKey)
    }

    /// Вход для разработки: POST /auth/dev, сохраняет токен и профиль.
    func loginDev(userId: Int, displayName: String, username: String?) async throws {
        let response = try await api.devLogin(userId: userId, displayName: displayName, username: username)
        token = response.token
        me = response.user
    }

    /// Вход по одноразовому коду из Telegram-бота: POST /auth/code,
    /// сохраняет токен (Keychain) и профиль. 401 — неверный/просроченный код.
    func loginWithCode(_ code: String) async throws {
        let response = try await api.loginWithCode(code)
        token = response.token
        me = response.user
    }

    /// Выход: сброс токена/профиля и очистка офлайн-хранилищ (read-кеш
    /// и outbox — чужие данные не должны пережить смену аккаунта).
    /// Подтверждение при непустом outbox — на экране «Профиль».
    @MainActor
    func logout() {
        expireSession()
        // Кеш — актор: чистим асинхронно, UI разлогина не ждёт диска.
        Task { [cache] in await cache.removeAll() }
        outbox.clear()
    }

    /// Истёкшая сессия (401 от сервера): сбрасывает токен и профиль, но
    /// СОХРАНЯЕТ outbox и read-кеш — пользователь переавторизуется тем же
    /// аккаунтом, и неотправленные расходы уйдут после входа.
    /// Полную очистку делает только явный `logout()`.
    @MainActor
    func expireSession() {
        token = nil
        me = nil
        avatars.removeAll()
    }

    /// Обновляет профиль (через кеш: офлайн-старт получает последний
    /// сохранённый профиль). При 401 сбрасывает сессию, сетевые ошибки
    /// молча игнорирует (профиль остаётся прежним/кешированным).
    @MainActor
    func refreshMe() async {
        guard isAuthenticated else { return }
        do {
            let result = try await repo.me { [weak self] cached in
                // Кеш мгновенно, но только если профиля ещё нет в памяти.
                if self?.me == nil {
                    self?.me = cached
                }
            }
            me = result.value
        } catch let error as APIError where error.isUnauthorized {
            logout()
        } catch {
            // Сервер недоступен и кеша нет — оставляем текущее состояние.
        }
    }
}
