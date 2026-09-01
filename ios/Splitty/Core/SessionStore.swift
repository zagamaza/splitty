import Foundation
import Observation

/// Сессия пользователя: JWT-токен (Keychain), профиль,
/// базовый URL сервера (UserDefaults).
@Observable
final class SessionStore {
    /// Подтверждение последнего успешного действия («Погашение записано»).
    ///
    /// Ни погашение, ни выход из группы, ни смена пароля ничего не отвечали:
    /// человек не понимал, случилось ли действие, и повторял его. Одно место на
    /// всё приложение, а не пять разных плашек.
    private(set) var successToast: String?

    /// Показать подтверждение действия.
    func confirm(_ text: String) {
        successToast = text
    }

    /// Скрыть подтверждение (по таймеру или тапу).
    func dismissToast() {
        successToast = nil
    }

    /// Профиль не загрузился и кеша нет. Без этого признака экран навсегда
    /// оставался скелетоном и показывал способы входа как «Не привязан».
    private(set) var profileLoadFailed = false

    /// Дефолтный сервер (меняется в поле «Сервер» на экране входа). UI-тестам
    /// нужен локальный бэкенд — они передают его через launch environment
    /// `SPLITTY_BASE_URL`; переменная имеет приоритет над UserDefaults,
    /// чтобы прогоны были детерминированы.
    ///
    /// Бэкенд живёт на https://splitor.zagirnur.dev (Caddy + Let's Encrypt),
    /// поэтому обе конфигурации ходят на один и тот же TLS-адрес. Каждый запрос
    /// несёт `Authorization: Bearer` с 90-дневным JWT — по plaintext это
    /// перехват сессии в любой публичной сети, так что release схему http вообще
    /// не принимает (см. `serverURL`). Android держит тот же инвариант
    /// (`SessionStore.DEFAULT_BASE_URL` + network_security_config).
    #if DEBUG
    static let defaultBaseURL = "https://splitor.zagirnur.dev"
    #else
    static let defaultBaseURL = "https://splitor.zagirnur.dev"
    #endif

    private static let baseURLKey = "splitty.baseURL"
    private static let tokenKey = "splitty.apiToken"
    private static let userIdKey = "splitty.userId"
    private static let purgePendingKey = "splitty.purgePending"
    private static let welcomeSeenKey = "splitty.welcomeSeenAccounts"

    /// Профиль текущего пользователя (nil до первого refreshMe/login).
    var me: Me?

    /// Непрочитанные уведомления — источник бейджа на табе.
    ///
    /// Живёт в сессии, а не во вью-модели экрана: иначе счётчик появлялся бы
    /// ровно в тот момент, когда экран его и гасит. Обновляется при старте,
    /// возврате из фона и приходе push.
    var unreadNotifications = 0

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
    /// Поколение сессии: растёт на каждую смену токена (вход, выход, протухание).
    ///
    /// Нужно, чтобы отличить «401 на действующий токен» от «401, догнавший нас
    /// из прошлой сессии». Сравнивать сами токены ненадёжно: JWT без jti и с
    /// секундным iat теоретически повторим, а счётчик уникален всегда.
    ///
    /// `@ObservationIgnored` обязателен: счётчик растёт в `didSet` токена, а
    /// запись НАБЛЮДАЕМОГО свойства внутри `didSet` другого наблюдаемого рвёт
    /// сам `didSet` — сохранение токена в Keychain переставало доезжать, и
    /// падал `testUnauthorizedWhilePurgePendingKeepsToken`. Экранам этот
    /// счётчик и не нужен: он служебный, для сверки поколения.
    @ObservationIgnored private(set) var sessionGeneration = 0

    private(set) var token: String? {
        didSet {
            sessionGeneration &+= 1
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

    /// true — `DELETE /me` упал ПОСЛЕ tombstone (`purge_incomplete`): аккаунт
    /// удалён, но его PII осталась в базе, и доделать чистку может только
    /// повторный `DELETE /me` ЭТИМ ЖЕ токеном (маршрут висит на `authDeleted`,
    /// войти заново нельзя — личности вычищены).
    ///
    /// Флаг персистится РЯДОМ С ТОКЕНОМ и живёт до подтверждённого 204 ровно
    /// потому, что удержать токен на время повтора одной локальной переменной
    /// экрана невозможно. Аккаунт уже tombstone, и КАЖДЫЙ следующий запрос к
    /// любому другому маршруту (все висят на `s.auth`) отвечает 401 —
    /// переключение вкладки, `refreshMe` на «Профиле», открытие группы. Без
    /// флага первый же такой 401 звал `expireSession`, тот стирал токен из
    /// Keychain, и единственный ключ к маршруту, который сервер держит
    /// открытым ради повтора, уничтожался самим клиентом — вместе с шансом
    /// когда-либо доделать удаление (5.1.1(v)/GDPR). Поэтому пока флаг стоит:
    /// `expireSession` токен НЕ трогает, а корень (`RootView`) сам повторяет
    /// `DELETE /me` до 204.
    private(set) var isPurgePending: Bool {
        didSet {
            if isPurgePending {
                UserDefaults.standard.set(true, forKey: Self.purgePendingKey)
            } else {
                UserDefaults.standard.removeObject(forKey: Self.purgePendingKey)
            }
        }
    }

    /// Повтор чистки уже в полёте — второй запускать нельзя: корень зовёт
    /// `finishPendingPurge` и по появлению флага, и по `.task`, а экран
    /// «Профиль» в это же время может нажать кнопку.
    private var isPurgeRetryInFlight = false

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

    /// Перечитать счётчик непрочитанного (старт, вход, возврат из фона, push).
    /// Тихо: сбой не должен ничем мигать пользователю.
    func refreshUnreadCount() async {
        guard isAuthenticated else { return }
        if let feed = try? await api.notificationFeed(limit: 1, offset: 0) {
            unreadNotifications = feed.unreadCount
        }
    }

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
        // Debug допускает http — локальный бэкенд на 127.0.0.1 и прогоны
        // UI-тестов с `SPLITTY_BASE_URL`. Release принимает только https:
        // бэкенд на splitor.zagirnur.dev под TLS, и никакой plaintext-адрес
        // (из UserDefaults или окружения) не должен утащить Bearer-токен
        // в открытую сеть.
        #if DEBUG
        guard scheme == "http" || scheme == "https" else { return nil }
        #else
        guard scheme == "https" else { return nil }
        #endif
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
        let generation = sessionGeneration
        let client = APIClient(baseURL: serverURL, token: token, urlSession: urlSession)
        // Протухший токен — НЕ полный logout: очередь неотправленных офлайн-расходов
        // должна пережить переавторизацию, иначе фоновый GET с просроченным JWT
        // молча стирает то, что пользователь ввёл без сети.
        client.onUnauthorized = { [weak self] in
            Task { @MainActor in
                // 401 засчитываем, только если он пришёл на ДЕЙСТВУЮЩУЮ сессию.
                // Ответ клиента, созданного до входа (или до переавторизации),
                // относится к прошлому поколению и гасил бы свежую сессию —
                // человек входил и тут же оказывался на экране логина.
                guard let self, self.sessionGeneration == generation else { return }
                self.expireSession()
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
        // Незавершённая чистка переживает перезапуск: без этого «человек убил
        // приложение вместо повтора» навсегда оставлял его PII в базе.
        isPurgePending = UserDefaults.standard.bool(forKey: Self.purgePendingKey)
    }

    /// Общий хвост всех способов входа: токен, профиль в памяти, владелец
    /// локальных данных и профиль В КЕШЕ. Кеш обязателен: `refreshMe` (только
    /// он писал ключ `me`) на старте после входа уже не зовётся, и человек,
    /// потерявший сеть до следующего запуска, получал «Профиль не загружен»
    /// на экране группы — кешированные комнаты есть, а `me` нет.
    @MainActor
    private func adoptSession(_ response: AuthResponse) {
        token = response.token
        me = response.user
        adoptOwner(response.user.id)
        // Строго ПОСЛЕ adoptOwner: ключ кеша префиксован владельцем.
        let repo = repo
        Task { await repo.cacheMe(response.user) }
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

    /// Вход через Google: POST /auth/google, сохраняет токен (Keychain)
    /// и профиль. `idToken` берётся из `GIDGoogleUser.idToken` (см.
    /// GoogleSignInService). 401 — токен не прошёл проверку на сервере.
    /// Вход через Telegram Login Widget (веб-поток, без ухода в Telegram):
    /// POST /auth/telegram, дальше как у остальных способов.
    @MainActor
    func loginWithTelegram(_ payload: TelegramWebAuth.Payload) async throws {
        let response = try await api.loginWithTelegram(payload)
        adoptSession(response)
    }

    @MainActor
    func loginWithGoogle(idToken: String) async throws {
        let response = try await api.loginWithGoogle(idToken: idToken)
        adoptSession(response)
    }

    /// Вход через Sign in with Apple: POST /auth/apple, сохраняет токен
    /// (Keychain) и профиль. Аргументы собирает LoginView из
    /// `AppleSignInService.Credential` — сырой nonce из `AppleNonce`
    /// и одноразовый `authorizationCode` (см. APIClient.loginWithApple).
    /// 401 — Apple-токен не прошёл проверку на сервере.
    @MainActor
    func loginWithApple(
        idToken: String,
        displayName: String,
        nonce: String,
        authorizationCode: String?
    ) async throws {
        let response = try await api.loginWithApple(
            idToken: idToken,
            displayName: displayName,
            nonce: nonce,
            authorizationCode: authorizationCode
        )
        adoptSession(response)
    }

    /// Регистрация по email и паролю: POST /auth/register.
    /// 409 `email_taken` — адрес уже занят.
    @MainActor
    func register(email: String, password: String, displayName: String) async throws {
        let response = try await api.register(email: email, password: password, displayName: displayName)
        adoptSession(response)
    }

    /// Вход по email и паролю: POST /auth/login.
    /// 401 — неверная пара; какая именно половина не сошлась, сервер не говорит.
    @MainActor
    func loginWithPassword(email: String, password: String) async throws {
        let response = try await api.loginWithPassword(email: email, password: password)
        adoptSession(response)
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

    /// Пароль: POST /me/password. `current` нужен, только если пароль уже был.
    /// 403 `invalid_password` — текущий пароль не сошёлся.
    @MainActor
    func setPassword(current: String?, new: String) async throws {
        me = try await api.setPassword(current: current, new: new).user
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
        do {
            try await api.deleteAccount()
        } catch {
            // Сбой ПОСЛЕ tombstone: поднимаем флаг ДО того, как ошибка уйдёт
            // наверх. С этого момента ни один 401 (а их теперь будет отдавать
            // каждый маршрут — аккаунт удалён) не имеет права стереть токен:
            // им и только им доделывается чистка. См. `isPurgePending`.
            if (error as? APIError)?.isPurgeIncomplete == true {
                isPurgePending = true
            }
            throw error
        }
        // 204: чистка доведена до конца — флаг снимаем ДО `logout`, иначе он
        // же и не дал бы токену исчезнуть.
        logout()
    }

    /// Доводит до конца чистку, начатую упавшим после tombstone `DELETE /me`.
    ///
    /// Зовёт корень (`RootView`) — и на старте, и по появлению флага. Одного
    /// повтора «по кнопке» на экране «Профиль» мало: человек, увидевший
    /// «повторите», волен вместо этого уйти на другую вкладку или убить
    /// приложение, а его данные так и остались бы в базе — без фонового
    /// реконсилятора на сервере доделать чистку больше некому.
    ///
    /// 401 здесь — единственный терминальный исход: `authDeleted` пускает
    /// удалённых, и раз он отказал, токен мёртв по-настоящему (или аккаунт уже
    /// вычищен целиком). Повторять нечем — снимаем флаг и выходим по-честному,
    /// иначе сессия-зомби осталась бы на устройстве навсегда. Всё остальное
    /// (снова `purge_incomplete`, 5xx, нет сети) — временно: флаг остаётся,
    /// повтор случится на следующем запуске.
    @MainActor
    func finishPendingPurge() async {
        guard isPurgePending, isAuthenticated, !isPurgeRetryInFlight else { return }
        isPurgeRetryInFlight = true
        defer { isPurgeRetryInFlight = false }
        let generation = sessionGeneration
        do {
            try await api.deleteAccount()
            logout()
        } catch let error as APIError where error.isUnauthorized {
            // Только для своей сессии: запоздалый 401 из прошлого поколения
            // унёс бы вместе с logout ещё и outbox нового аккаунта.
            guard sessionGeneration == generation else { return }
            logout()
        } catch {
            // Повторим позже — токен и флаг на месте.
        }
    }

    /// Выход: сброс токена/профиля и очистка офлайн-хранилищ (read-кеш
    /// и outbox — чужие данные не должны пережить смену аккаунта).
    /// Подтверждение при непустом outbox — на экране «Профиль».
    @MainActor
    func logout() {
        // Снять ДО `expireSession`: пока флаг стоит, тот намеренно бережёт
        // токен, и выход не состоялся бы вовсе. Явный выход (и подтверждённый
        // 204) — единственные места, где чистку больше повторять не нужно.
        isPurgePending = false
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
        // Незавершённая чистка после tombstone — единственное исключение.
        // Аккаунт удалён, поэтому 401 отвечает КАЖДЫЙ маршрут на `s.auth`
        // (вкладка «Группы», `refreshMe`, отвязка push-токена), и обычный
        // сброс уничтожил бы токен, которым только и можно доделать удаление:
        // войти заново нельзя, личности вычищены. Токен держим до 204 —
        // повторяет `finishPendingPurge`. См. `isPurgePending`.
        guard !isPurgePending else { return }
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
        profileLoadFailed = false
        // Поколение способов входа на момент старта запроса: экран «Профиль»
        // запускает refreshMe в `.task`, и пользователь успевает нажать
        // «Привязать» до того, как ответ придёт (см. `identityRevision`).
        let revision = identityRevision
        // Поколение на момент старта запроса: ответ мог задержаться, а человек
        // за это время успел перевойти (см. sessionGeneration).
        let generation = sessionGeneration
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
            //
            // И только для СВОЕЙ сессии: refreshMe зовётся на каждом старте, его
            // ответ легко переживает вход, и без этой проверки запоздалый 401
            // гасил бы только что созданную сессию в обход onUnauthorized.
            guard sessionGeneration == generation else { return }
            expireSession()
        } catch {
            // Сервер недоступен и кеша нет. Раньше экран оставался скелетоном
            // навсегда и показывал ВСЕ способы входа как «Не привязан» — то
            // есть врал про безопасность аккаунта. Отмечаем сбой, чтобы экран
            // сказал правду и предложил повтор
            if me == nil { profileLoadFailed = true }
        }
    }
    // MARK: Приветствие

    /// Видел ли этот аккаунт разовое приветствие.
    ///
    /// Ключ по НОМЕРУ аккаунта, а не на устройство: вход другим человеком на том
    /// же телефоне обязан показать приветствие снова — иначе новый пользователь
    /// молча теряет единственное объяснение продукта.
    func hasSeenWelcome(userId: Int) -> Bool {
        seenWelcomeIds().contains(String(userId))
    }

    /// Отметить приветствие показанным. Вызывается и по «Пропустить»: пропуск —
    /// это ответ «не показывай больше», а не «покажи в следующий раз».
    func markWelcomeSeen(userId: Int) {
        var ids = seenWelcomeIds()
        ids.insert(String(userId))
        UserDefaults.standard.set(Array(ids), forKey: Self.welcomeSeenKey)
    }

    private func seenWelcomeIds() -> Set<String> {
        Set(UserDefaults.standard.stringArray(forKey: Self.welcomeSeenKey) ?? [])
    }

}
