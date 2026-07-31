import Foundation
import Observation

/// Сессия пользователя: JWT-токен (Keychain), профиль,
/// базовый URL сервера (UserDefaults).
@Observable
final class SessionStore {
    /// Дефолтный сервер (меняется в поле «Сервер» на экране входа). UI-тестам
    /// нужен локальный бэкенд — они передают его через launch environment
    /// `SPLITTY_BASE_URL`; переменная имеет приоритет над UserDefaults,
    /// чтобы прогоны были детерминированы.
    ///
    /// В release — только https: каждый запрос несёт `Authorization: Bearer`
    /// с 90-дневным JWT, по plaintext это перехват сессии в любой публичной
    /// сети. Android закрыл это же место (`SessionStore.DEFAULT_BASE_URL`
    /// + network_security_config), iOS повторяет инвариант.
    #if DEBUG
    static let defaultBaseURL = "http://138.124.18.189:18002"
    #else
    // ВРЕМЕННО (сборка «для друзей», internal TestFlight): TLS-домена на бэкенд
    // нет (api.splitty.app не резолвится), поэтому release тоже ходит на дев-
    // сервер по голому HTTP-IP. Cleartext разрешён через NSAllowsArbitraryLoads.
    // Android держит тот же временный инвариант. TODO: вернуть
    // https://api.splitty.app (и https-guard в serverURL), когда поднимется TLS.
    static let defaultBaseURL = "http://138.124.18.189:18002"
    #endif

    private static let baseURLKey = "splitty.baseURL"
    private static let tokenKey = "splitty.apiToken"
    private static let userIdKey = "splitty.userId"

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
                // Результат записи нельзя ронять: при отказе Keychain
                // (например errSecInteractionNotAllowed) приложение выглядит
                // залогиненным до перезапуска, а после — молча выкидывает на
                // экран входа. Поднимаем флаг, чтобы UI мог предупредить.
                tokenPersisted = KeychainStore.save(token, key: Self.tokenKey)
            } else {
                KeychainStore.delete(key: Self.tokenKey)
                tokenPersisted = true
            }
        }
    }

    /// false — токен живёт только в памяти: Keychain отказал в записи и сессия
    /// не переживёт перезапуск приложения.
    private(set) var tokenPersisted = true

    var isAuthenticated: Bool { token != nil }

    /// id владельца локальных данных (кеш + outbox), переживает перезапуск:
    /// профиль на холодном старте ещё не загружен, а имя namespace кеша нужно
    /// ДО первого чтения. Персистится в UserDefaults при входе/refreshMe.
    private(set) var ownerUserId: Int? {
        didSet {
            if let ownerUserId {
                UserDefaults.standard.set(ownerUserId, forKey: Self.userIdKey)
            } else {
                UserDefaults.standard.removeObject(forKey: Self.userIdKey)
            }
        }
    }

    /// Namespace файлового кеша: ключи GET сами по себе (`me`, `friends`…)
    /// пользователя не различают, и после смены аккаунта экран мгновенно
    /// показывал бы профиль и группы ПРЕДЫДУЩЕГО.
    private var cacheScope: String {
        ownerUserId.map { "u\($0)" } ?? "anon"
    }

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
    ///
    /// В release схема http отвергается: иначе сохранённый в UserDefaults
    /// (или подставленный через SPLITTY_BASE_URL) plaintext-адрес обошёл бы
    /// https-дефолт и снова слал бы Bearer-токен открытым текстом.
    var serverURL: URL? {
        let trimmed = baseURLString.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty,
              let url = URL(string: trimmed),
              let scheme = url.scheme?.lowercased(),
              url.host() != nil
        else {
            return nil
        }
        // ВРЕМЕННО release тоже допускает http: дев-бэкенд по голому IP (нет TLS-
        // домена, сборка для друзей). Вернуть на release-only `https`, когда
        // поднимется api.splitty.app (см. defaultBaseURL выше).
        guard scheme == "http" || scheme == "https" else { return nil }
        return url
    }

    /// Транспорт для всех создаваемых клиентов: в проде `.shared`, в тестах —
    /// сессия с подставленным `URLProtocol`. Тот же шов, что у `APIClient`
    /// (см. его `urlSession`): без него сценарии, живущие в SessionStore
    /// (удаление аккаунта, привязка), тестировать нечем — клиент он создаёт
    /// сам, и подменить его снаружи невозможно.
    private let urlSession: URLSession

    /// Всегда актуальный API-клиент (с текущими token/baseURL).
    /// Любой ответ 401 сбрасывает сессию (токен чистится из Keychain).
    var api: APIClient {
        let client = APIClient(baseURL: serverURL, token: token, urlSession: urlSession)
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
        DataRepo(api: api, cache: cache, scope: cacheScope)
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

    init(urlSession: URLSession = .shared) {
        self.urlSession = urlSession
        // Прямое присваивание в init не дергает didSet — override из окружения
        // не затирает сохранённый пользователем адрес в UserDefaults.
        baseURLString = ProcessInfo.processInfo.environment["SPLITTY_BASE_URL"]
            ?? UserDefaults.standard.string(forKey: Self.baseURLKey)
            ?? Self.defaultBaseURL
        token = KeychainStore.read(key: Self.tokenKey)
        ownerUserId = UserDefaults.standard.object(forKey: Self.userIdKey) as? Int
    }

    /// Вход для разработки: POST /auth/dev, сохраняет токен и профиль.
    @MainActor
    func loginDev(userId: Int, displayName: String, username: String?) async throws {
        let response = try await api.devLogin(userId: userId, displayName: displayName, username: username)
        token = response.token
        me = response.user
        adoptOwner(response.user.id)
    }

    /// Привязывает локальные данные ко вошедшему пользователю. Если вошёл
    /// ДРУГОЙ аккаунт — кеш прошлого владельца стирается, а его записи outbox
    /// выбрасываются: иначе syncOutbox отправил бы расходы пользователя A
    /// под токеном пользователя B.
    @MainActor
    private func adoptOwner(_ userId: Int) {
        let previous = ownerUserId
        ownerUserId = userId
        if let previous, previous != userId {
            Task { [cache] in await cache.removeAll() }
        }
        // Отложенное вступление по ссылке решается СВОИМ владельцем, а не сменой
        // владельца устройства: чужое намерение выбрасывается, ничьё (ссылку
        // открыл гость) достаётся вошедшему. Раньше тут стояла безусловная
        // чистка на любой смене `ownerUserId`, и она детерминированно убивала
        // приглашение самого гостя: `expireSession` оставляет `ownerUserId`
        // прошлого аккаунта, поэтому «A разлогинило по 401 → гость B открыл
        // ссылку → B вошёл» ничем не отличалось от прихода чужого человека.
        // А зовётся это ДО того, как SwiftUI доставит `.onChange(of:
        // session.isAuthenticated)`, так что `RootView` уже не находил ничего.
        PendingJoin.shared.reconcileOwner(userId)
        outbox.keepOwned(by: userId, inheritingOrphans: previous == nil || previous == userId)
    }

    /// Вход по одноразовому коду из Telegram-бота: POST /auth/code,
    /// сохраняет токен (Keychain) и профиль. 401 — неверный/просроченный код.
    @MainActor
    func loginWithCode(_ code: String) async throws {
        let response = try await api.loginWithCode(code)
        token = response.token
        me = response.user
        adoptOwner(response.user.id)
    }

    /// Вход через Google: POST /auth/google, сохраняет токен (Keychain)
    /// и профиль. `idToken` берётся из `GIDGoogleUser.idToken` (см.
    /// GoogleSignInService). 401 — токен не прошёл проверку на сервере.
    @MainActor
    func loginWithGoogle(idToken: String) async throws {
        let response = try await api.loginWithGoogle(idToken: idToken)
        token = response.token
        me = response.user
        adoptOwner(response.user.id)
    }

    /// Вход через Sign in with Apple: POST /auth/apple, сохраняет токен
    /// (Keychain) и профиль. Аргументы собирает LoginView из
    /// `ASAuthorizationAppleIDCredential` — сырой nonce из `AppleNonce`
    /// и одноразовый `authorizationCode` (см. APIClient.loginWithApple).
    /// 401 — Apple-токен не прошёл проверку на сервере.
    @MainActor
    func loginWithApple(
        idToken: String,
        displayName: String,
        nonce: String,
        authorizationCode: String
    ) async throws {
        let response = try await api.loginWithApple(
            idToken: idToken,
            displayName: displayName,
            nonce: nonce,
            authorizationCode: authorizationCode
        )
        token = response.token
        me = response.user
        adoptOwner(response.user.id)
    }

    // MARK: - Способы входа

    /// Номер поколения списка способов входа: растёт после каждой успешной
    /// привязки/отвязки. Нужен `refreshMe`, чтобы отличить «свежий /me» от
    /// ответа, ушедшего в полёт ДО привязки: такой ответ несёт устаревший
    /// `linkedProviders` и, придя вторым, стирал бы только что привязанный
    /// способ из UI (Android закрыл это же место в задаче 21).
    private(set) var identityRevision = 0

    /// Привязка Google к текущему аккаунту: POST /me/link/google.
    /// Профиль в сессии обновляется ответом сервера — `linkedProviders`
    /// приезжает оттуда, а не досочиняется на клиенте.
    /// 409 `identity_taken` — личность занята другим профилем Splitty.
    @MainActor
    func linkGoogle(idToken: String) async throws {
        me = try await api.linkGoogle(idToken: idToken).user
        identityRevision += 1
    }

    /// Привязка Apple ID: POST /me/link/apple. Сырой nonce — как при входе
    /// (в подписанном токене лежит его SHA256, сервер сверяет одно с другим).
    ///
    /// `authorizationCode` тянется сюда из системного листа привязки и уходит
    /// на сервер: без него у аккаунта появится `apple_sub` без refresh token,
    /// и отозвать доступ при удалении будет нечем (Guideline 5.1.1(v)).
    @MainActor
    func linkApple(idToken: String, nonce: String, authorizationCode: String?) async throws {
        me = try await api.linkApple(
            idToken: idToken,
            nonce: nonce,
            authorizationCode: authorizationCode
        ).user
        identityRevision += 1
    }

    /// Отвязка способа входа: DELETE /me/link/{provider}.
    /// Возвращает предупреждение сервера (отвязка Telegram), которое экран
    /// обязан показать — молча проглатывать его нельзя, там про потерю групп.
    @MainActor
    @discardableResult
    func unlink(_ provider: LoginProvider) async throws -> String? {
        let response = try await api.unlinkProvider(provider)
        me = response.user
        identityRevision += 1
        return response.warning
    }

    // MARK: - Удаление аккаунта

    /// Удаление аккаунта: DELETE /me и полный `logout` — но ТОЛЬКО при 204.
    ///
    /// Любая ошибка оставляет сессию, токен и офлайн-хранилища на месте, и это
    /// не осторожность, а единственный работающий вариант. Сервер различает два
    /// вида сбоя кодом ошибки (см. `delete_account.go`), и оба требуют одного:
    ///
    ///  - до tombstone (`internal`, 403, 401, запрос не дошёл вовсе) аккаунт
    ///    ЖИВ и не тронут. Стирать нечего: «Удалить аккаунт», нажатое в метро
    ///    или попавшее на транзиентный сбой mongo, уносило очередь
    ///    неотправленных офлайн-расходов НАВСЕГДА и выкидывало человека из
    ///    живой сессии;
    ///  - после tombstone (`purge_incomplete`) аккаунт удалён, но PII —
    ///    имя в снимках комнат, тексты в chat_state/bug_report/push_outbox —
    ///    осталась. Доделать чистку может только ПОВТОРНЫЙ DELETE /me: маршрут
    ///    висит на `authDeleted` ровно ради этого. Токен для повтора —
    ///    единственный: `SoftDeleteUser` уже вычистил telegram_id/google_sub/
    ///    apple_sub, так что войти заново нельзя (Google/Apple завели бы НОВЫЙ
    ///    аккаунт). Выбросив его, клиент навсегда закрывал путь, который сервер
    ///    специально держит открытым, — и оставлял данные человека в базе, то
    ///    есть проваливал ровно то требование 5.1.1(v)/GDPR, ради которого
    ///    удаление и написано.
    ///
    /// Android (`ProfileViewModel.deleteAccount`) держит тот же инвариант.
    ///
    /// Ошибка пробрасывается всегда — экран обязан сказать, что удаление не
    /// подтвердилось, и что делать дальше (текст — `deleteAccountErrorText`).
    @MainActor
    func deleteAccount() async throws {
        try await api.deleteAccount()
        logout()
    }

    /// Выход: сброс токена/профиля и очистка офлайн-хранилищ (read-кеш
    /// и outbox — чужие данные не должны пережить смену аккаунта).
    /// Подтверждение при непустом outbox — на экране «Профиль».
    @MainActor
    func logout() {
        expireSession()
        ownerUserId = nil
        // Отложенное вступление по ссылке — тоже чужое: без этой строки
        // следующий вошедший на устройстве человек молча оказался бы
        // в группе предыдущего.
        PendingJoin.shared.clear()
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
        // Поколение способов входа на момент старта запроса: экран «Профиль»
        // запускает refreshMe в `.task`, и пользователь успевает нажать
        // «Привязать» до того, как ответ придёт (см. `identityRevision`).
        let revision = identityRevision
        do {
            let result = try await repo.me { [weak self] cached in
                // Кеш мгновенно, но только если профиля ещё нет в памяти
                // и никто не успел привязать способ входа: кешированный
                // профиль заведомо старее ответа на привязку.
                if self?.me == nil, self?.identityRevision == revision {
                    self?.me = cached
                }
            }
            // Токен мог остаться от установки без сохранённого владельца —
            // подхватываем его, чтобы namespace кеша и outbox были привязаны.
            // Делаем это в любом случае: id пользователя привязка не меняет.
            adoptOwner(result.value.id)
            // Пока /me летел, привязка/отвязка уже обновила профиль ответом
            // сервера. Этот ответ старее — перезаписав им `me`, мы стёрли бы
            // только что привязанный способ входа из карточки.
            guard revision == identityRevision else { return }
            me = result.value
        } catch let error as APIError where error.isUnauthorized {
            // Именно expireSession, а НЕ logout: refreshMe зовётся на каждом
            // старте и на «Профиле», и полная очистка стирала бы outbox —
            // один протухший токен уносил все неотправленные офлайн-расходы.
            expireSession()
        } catch {
            // Сервер недоступен и кеша нет — оставляем текущее состояние.
        }
    }
}
