// Package rest реализует REST API поверх сервисов splitty (контракт: docs/API.md).
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/oidc"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/safe"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Notifier отправляет участникам комнаты telegram-уведомления о REST-мутациях —
// те же, что шлют экраны бота (реализация — bot.Notifier). Интерфейс объявлен
// в пакете rest узким, чтобы не тащить пакет bot и telegram-зависимости в тесты.
// Методы вызываются в фоне после успешной записи и не должны возвращать ошибок —
// сбой уведомления не ломает запрос (реализация только логирует)
type Notifier interface {
	NotifyOperationCreated(ctx context.Context, room api.Room, op api.Operation, author api.User)
	NotifyOperationUpdated(ctx context.Context, room api.Room, oldOp api.Operation, newOp api.Operation, author api.User)
	NotifyOperationDeleted(ctx context.Context, room api.Room, op api.Operation, author api.User)
	NotifyRepaymentCreated(ctx context.Context, room api.Room, op api.Operation, author api.User)
	// NotifyInvited сообщает человеку, что его позвали в комнату. isReturn —
	// это повторное приглашение после выхода (текст другой: «приглашает
	// вернуться», а не «добавил вас»)
	NotifyInvited(ctx context.Context, room api.Room, invitee api.User, inviter api.User, isReturn bool)
}

// userIDAllocator выдаёт номера пользователей Splitty, у которых нет telegram
// id (вход через Google/Apple). Интерфейс объявлен здесь узким, чтобы в тестах
// подставлялся фейк без mongo. Реализация — repository.MongoSequenceRepository
type userIDAllocator interface {
	NextUserID(ctx context.Context) (int, error)
}

// userDataCleaner удаляет записи пользователя из побочной коллекции
// (chat_state, bug_report, push_outbox).
//
// Интерфейс узкий, а не репозиторный целиком: REST нужен ровно один метод, а
// полные интерфейсы распухали бы фейками в каждом тесте — и правило расширения
// интерфейсов (см. план) било бы по каждой будущей задаче. Реализации —
// repository.MongoChatStateRepository, MongoBugReportRepository,
// MongoPushOutboxRepository
type userDataCleaner interface {
	DeleteByUserId(ctx context.Context, userId int) error
}

// productEventStore — журнал продуктовых событий. Интерфейс узкий, как и
// соседние: тестам нужен фейк, а не живой mongo.
type productEventStore interface {
	Insert(ctx context.Context, events []repository.ProductEvent) (repository.InsertResult, error)
	DeleteByUserId(ctx context.Context, userId int) error
	Feed(ctx context.Context, days, limit int) ([]repository.FeedRow, error)
	Daily(ctx context.Context, days int, name string) ([]repository.DailyRow, error)
	Platforms(ctx context.Context, days int) ([]repository.PlatformRow, error)
}

// inviteStore хранит отношения «человек × комната»: кто кого позвал и в каком
// состоянии отношение сейчас. Интерфейс объявлен здесь узким — как
// userIDAllocator и Notifier: тестам нужен фейк, а не живой mongo.
//
// DeleteByUserId входит в тот же набор, поэтому одна реализация служит и
// хранилищем приглашений, и userDataCleaner при удалении аккаунта.
type inviteStore interface {
	Upsert(ctx context.Context, roomID primitive.ObjectID, inviteeID, inviterID int, status api.InviteStatus, now time.Time) error
	// UpsertIfUnchanged пишет, только если запись не менялась с момента чтения
	// (since — сама прочитанная запись, nil означает «записи не было»)
	UpsertIfUnchanged(ctx context.Context, roomID primitive.ObjectID, inviteeID, inviterID int, status api.InviteStatus, since *api.RoomInvite, now time.Time) (bool, error)
	Find(ctx context.Context, roomID primitive.ObjectID, inviteeID int) (*api.RoomInvite, error)
	ListForUser(ctx context.Context, userID int) ([]api.RoomInvite, error)
	SetStatusIfCurrent(ctx context.Context, roomID primitive.ObjectID, inviteeID int, from, to api.InviteStatus, now time.Time) (bool, error)
	DeleteByUserId(ctx context.Context, userId int) error
}

// fileStore — картинки, загруженные из приложения (коллекция files). Байты
// лежат в mongo; телеграм остаётся только для файлов, присланных в бота.
type fileStore interface {
	Save(ctx context.Context, f *api.StoredFile) (string, error)
	Get(ctx context.Context, id string) (*api.StoredFile, error)
	Delete(ctx context.Context, id string) error
	DeleteByRoom(ctx context.Context, roomId string) error
}

// Config конфигурация REST-сервера
type Config struct {
	Listen    string // адрес http-сервера, например "localhost:7171"
	JwtSecret string // секрет подписи JWT (HS256)
	DevAuth   bool   // включает POST /auth/dev
	TgToken   string // токен бота: нужен для проверки Telegram Login и проксирования файлов
	// ReviewLoginCode/ReviewUserId — многоразовый код входа для ревьюеров
	// App Store (Beta App Review): обычный вход требует Telegram, ревьюерам
	// он недоступен. Код секретный, логинит только в выделенный демо-аккаунт;
	// пустой код — механизм выключен
	ReviewLoginCode string
	ReviewUserId    int
	// TokenMinIssuedAt — отсечка по дате выпуска токена: всё, что выдано
	// раньше, отвергается с 401. Нулевое значение — отсечки нет.
	//
	// Нужна разово: сборки, ходившие по открытому HTTP, раздали токены, которые
	// живут 90 дней, и обновление приложения их не отзывает. ⚠️ Включение
	// разлогинивает ВСЕХ, кто вошёл до указанной даты, — это и есть смысл, но
	// дату выбирают осознанно
	TokenMinIssuedAt time.Time
	// FractionalInput разрешает ПИСАТЬ дробные суммы. Выключен по умолчанию:
	// старый сервер этого поля не отдаёт вовсе, и клиент обязан прочесть
	// отсутствие как запрет. Умение ЧИТАТЬ дроби от флага не зависит —
	// уже сохранённые копейки отдаются точно в любом положении флага.
	FractionalInput bool
	// GoogleVerifier проверяет ID-токены Google для POST /auth/google.
	// nil — вход через Google не сконфигурирован (GOOGLE_CLIENT_IDS пуст),
	// эндпоинт отвечает 503. Интерфейс, а не client id: в тестах подставляется
	// фейк и в сеть никто не ходит
	GoogleVerifier oidc.Verifier
	// AppleVerifier проверяет ID-токены Sign in with Apple для POST /auth/apple.
	// nil — вход через Apple не сконфигурирован (APPLE_CLIENT_IDS пуст),
	// эндпоинт отвечает 503
	AppleVerifier oidc.Verifier
	// AppleTokens меняет authorizationCode на refresh token при входе и
	// отзывает его при удалении аккаунта (Apple Guideline 5.1.1(v)). nil —
	// ключ .p8 не задан: вход работает как обычно, обмен и отзыв просто не
	// делаются, чтобы локальная разработка не требовала ключа Apple
	AppleTokens oidc.AppleTokens

	// PublicBaseUrl — публичный https-адрес, по которому раздаются associated
	// files и страница приглашения (в проде "https://splitor.zagirnur.dev").
	// Пусто — домен не настроен: /join и оба .well-known отдают 404, так что
	// диплинк безопасно выкатывается и без домена (см. deeplink.go)
	PublicBaseUrl string
	// IosAppId — <TeamID>.<bundle id>, например "K8922Y6R3M.com.zagir.splitty".
	// Пусто — apple-app-site-association отдаёт 404: пустой appID в файле хуже
	// отсутствия файла, iOS закеширует негодный ассоциированный домен
	IosAppId string
	// AndroidPackage — имя пакета приложения, например "com.zagir.splitty".
	// Используется и в assetlinks.json, и в ссылке на карточку Google Play
	AndroidPackage string
	// AndroidCertSha256 — SHA-256 отпечатки сертификатов подписи для
	// assetlinks.json. Их может быть несколько: Play App Signing и локальный
	// debug-ключ, иначе app links не верифицируются в отладочной сборке.
	// Пусто — assetlinks.json отдаёт 404
	AndroidCertSha256 []string
	// IosStoreUrl — карточка приложения в App Store для страницы приглашения.
	// Ссылка требует числовой id, который появляется только после первой
	// публикации, поэтому это отдельная настройка, а не производная от bundle
	// id. Пусто — кнопка App Store не рисуется
	IosStoreUrl string
	// AdminToken — общий токен админского API (AdminHandler). Пусто — админский
	// слушатель не поднимается вовсе: эндпоинты отдают чужие суммы и долги, и
	// открытыми они быть не могут ни при какой конфигурации
	AdminToken string
	// TrustedProxies — сколько обратных прокси стоит перед сервером. 0 (дефолт)
	// означает «прямые соединения»: X-Forwarded-For не читается вовсе, лимиты
	// считаются по RemoteAddr. Ненулевое значение включает разбор заголовка с
	// хвоста — подробности и обоснование в clientIP
	TrustedProxies int
	// TrustedProxyNets — адреса и подсети, с которых заголовку X-Forwarded-For
	// можно верить. Пусто — петля и приватные диапазоны (реверс-прокси стоит
	// рядом). Прямому запросу из интернета заголовок не читается никогда
	TrustedProxyNets []*net.IPNet
}

// Server REST API сервер со всеми зависимостями.
// Статистика и долги считаются в хендлерах по нормализованной комнате
// (см. normalizedRoom) расчётом develop — отдельный StatisticService не нужен:
// он перечитывает комнату из базы и не видит нормализацию легаси-операций
type Server struct {
	cfg           Config
	userRepo      repository.UserRepository
	roomRepo      repository.RoomRepository
	loginCodeRepo repository.LoginCodeRepository
	roomSrv       *service.RoomService
	operationSrv  *service.OperationService
	// userIDs выдаёт номер новому пользователю без telegram id — нужен входу
	// через Google/Apple
	userIDs userIDAllocator
	// notifier опционален (см. SetNotifier): nil — уведомления выключены (no-op)
	notifier Notifier
	// chatStates опционален (см. SetChatStates): нужен отвязке telegram и
	// удалению аккаунта, nil — чистка состояний бота пропускается
	chatStates userDataCleaner
	// bugReports и pushOutbox опциональны (см. SetBugReports/SetPushOutbox):
	// нужны только удалению аккаунта, где вычищается PII побочных коллекций.
	// nil — соответствующая коллекция не чистится
	bugReports userDataCleaner
	pushOutbox userDataCleaner
	// invites опционален (см. SetInvites): хранилище отношений «человек ×
	// комната» для приглашений и раздела уведомлений
	invites inviteStore
	// debtReminders — память о напоминаниях про долг. Обязателен для удаления
	// аккаунта: без него id человека и история напоминаний пережили бы tombstone
	debtReminders userDataCleaner
	// adminRooms опционален (см. SetAdminRooms): поиск комнат для админской
	// панели. nil — поиск отвечает 503, остальной сервер работает как раньше
	adminRooms adminRoomStore
	// adminUsers опционален (см. SetAdminUsers): поиск людей для админской панели
	adminUsers adminUserStore
	// productEvents — журнал продуктовых событий. Опционален для ПРИЁМА
	// (nil — маршрут отвечает 503), но НЕ для удаления аккаунта: там он
	// подключается безусловно, см. purgeUserData
	productEvents productEventStore
	// analyticsEnabled — принимать ли события. Управляет записью, а не
	// чисткой: события удалённого аккаунта вычищаются всегда
	analyticsEnabled bool
	// files опционален (см. SetFiles): хранилище картинок в mongo. nil —
	// загрузка авы отвечает 503, а отдача файлов работает по-старому, через
	// телеграм
	files fileStore

	// dbPing проверяет доступность базы для /health; nil — проверять нечем
	// (тесты, запуск без mongo)
	dbPing func(ctx context.Context) error
	// botHeartbeat отдаёт время последнего успешно принятого обновления
	// telegram; нулевое время — бот не запущен в этом процессе
	botHeartbeat func() time.Time

	// accounts кеширует «жив ли аккаунт» для auth-middleware: без него проверка
	// tombstone стоила бы запроса в mongo на КАЖДЫЙ авторизованный запрос
	accounts *accountCache

	// AI-парсинг расхода опционален (см. SetAI): nil aiParser — фича выключена
	// (эндпоинт /parse отдаёт 503), остальной сервер работает как раньше
	aiParser    ai.Parser
	rateLimiter *service.RateLimiter
	aiMaxBody   int64
	// entitlements резолвит тариф пользователя И суточный лимит для него —
	// единственный источник правды по тарифам (см. SetEntitlements)
	entitlements *service.Entitlements
	// Подписки: хранилище и проверка чеков магазинов (см. SetSubscriptions).
	// nil-верификатор — ключи магазина не заданы, эндпоинт отдаёт 503
	subscriptions  subscriptionStore
	appleReceipts  ReceiptVerifier
	googleReceipts ReceiptVerifier
	googleAck      PurchaseAcknowledger
	// plusGrants — Plus, выданный из панели: третий источник тарифа. Экрану
	// подписки нужен его срок, удалению аккаунта — чистка (см. SetPlusGrants)
	plusGrants plusGrantStore
	// deliverySlack — тот же запас, что у Entitlements. Нужен, чтобы решить,
	// живая ли покупка: Active(now, 0) спрятал бы ссылку «управлять» у
	// платящего на два часа окна продления, хотя тариф всё ещё Plus
	deliverySlack time.Duration
	// Чеки песочницы — только для перечисленных людей (см. SetSandboxReceipts)
	sandboxUsers          map[int]struct{}
	appleSandboxReceipts  ReceiptVerifier
	googleSandboxReceipts ReceiptVerifier
	// Перезапрос состояния подписки для уведомлений магазинов
	// (см. SetStoreWebhooks и правило «не верить payload»)
	appleStatus  storeStatusReader
	googleStatus googleStatusReader

	// authThrottle гасит перебор одноразовых кодов входа (см. throttle.go)
	authThrottle *throttle

	httpServer *http.Server
	httpClient *http.Client
	tgApiURL   string           // базовый url telegram api, переопределяется в тестах
	now        func() time.Time // источник времени для статистики, переопределяется в тестах
	avatars    avatarCache      // суточный кеш аватаров из telegram (см. avatar.go)
}

// NewServer собирает сервер со всеми зависимостями
func NewServer(cfg Config, ur repository.UserRepository, rr repository.RoomRepository,
	lr repository.LoginCodeRepository, rs *service.RoomService, os *service.OperationService,
	ua userIDAllocator) *Server {
	// клиент для telegram: без общего Timeout, иначе он режет скачивание больших
	// файлов (таймер тикает и во время чтения тела). Ограничиваем только фазу
	// соединения/заголовков, а тело привязано к контексту входящего запроса
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second

	s := &Server{
		cfg:           cfg,
		userRepo:      ur,
		roomRepo:      rr,
		loginCodeRepo: lr,
		roomSrv:       rs,
		operationSrv:  os,
		userIDs:       ua,
		accounts:      newAccountCache(),
		authThrottle:  newThrottle(),
		httpClient:    &http.Client{Transport: transport},
		tgApiURL:      "https://api.telegram.org",
		now:           time.Now,
	}
	// httpServer создаётся здесь, а не в Run: Shutdown может быть вызван из
	// closer/сигнальной горутины параллельно с Run — инициализация поля до
	// запуска горутин исключает data race на s.httpServer
	s.httpServer = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// защита от slowloris: ReadTimeout ограничивает чтение всего запроса,
		// IdleTimeout закрывает вечные keep-alive соединения.
		// WriteTimeout намеренно не задан — через сервер стримятся большие файлы
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
	return s
}

// SetNotifier включает telegram-уведомления о мутациях (когда бот сконфигурирован).
// Вызывать до Run. Nil-безопасно: без notifier'а сервер работает как раньше
func (s *Server) SetNotifier(n Notifier) {
	s.notifier = n
}

// SetChatStates подключает коллекцию состояний бота: отвязка telegram и
// удаление аккаунта чистят незавершённые сценарии пользователя (chat_state
// хранит текст расхода в CallbackData.ExternalData — это PII). Вызывать до Run.
// Отдельный setter, а не параметр NewServer — не ломает вызовы конструктора в
// тестах. Nil-безопасно: без него состояния просто не чистятся
func (s *Server) SetChatStates(c userDataCleaner) {
	s.chatStates = c
}

// SetBugReports подключает коллекцию репортов о багах. Нужна удалению аккаунта:
// bug_report хранит username, display_name и свободный текст жалобы.
// Вызывать до Run, nil-безопасно
func (s *Server) SetBugReports(c userDataCleaner) {
	s.bugReports = c
}

// SetPushOutbox подключает очередь пушей. Нужна удалению аккаунта: в очереди
// лежат отрендеренные title/body с именами и описаниями расходов, и без чистки
// человеку доставился бы пуш со старым именем уже после анонимизации комнат.
// Вызывать до Run, nil-безопасно
func (s *Server) SetPushOutbox(c userDataCleaner) {
	s.pushOutbox = c
}

// SetProductEvents подключает журнал продуктовых событий.
//
// ⚠️ Вызывать БЕЗУСЛОВНО, даже когда приём событий выключен. purgeUserData
// возвращает ошибку на неподключённом очистителе, и делает это уже ПОСЛЕ
// tombstone: пропущенный вызов означал бы аккаунт, который помечен удалённым, а
// DELETE /me навсегда отвечает purge_incomplete. Выключатель приёма
// (SetAnalyticsEnabled) управляет записью, а не чисткой.
func (s *Server) SetProductEvents(store productEventStore) {
	s.productEvents = store
}

// SetAnalyticsEnabled включает приём событий. Выключено — маршрут отвечает
// «принято» и ничего не пишет: отказ заставил бы клиента копить очередь и
// ретраить её вечно.
func (s *Server) SetAnalyticsEnabled(enabled bool) {
	s.analyticsEnabled = enabled
}

// SetInvites подключает хранилище приглашений. Вызывать до Run, nil-безопасно:
// без него эндпоинты приглашений отвечают 503, остальной сервер работает.
func (s *Server) SetInvites(store inviteStore) {
	s.invites = store
}

// SetDebtReminders подключает память о напоминаниях про долг. Нужна удалению
// аккаунта (там своя PII) и джобу напоминаний.
func (s *Server) SetDebtReminders(c userDataCleaner) {
	s.debtReminders = c
}

// SetFiles подключает хранилище картинок. Вызывать до Run, nil-безопасно:
// без него загрузка авы отвечает 503, а старые телеграмные вложения
// продолжают отдаваться.
func (s *Server) SetFiles(store fileStore) {
	s.files = store
}

// SetEntitlements подключает резолв тарифа — единственный источник правды о
// том, платный ли человек и какой у него суточный лимит. Отдельный setter, а не
// параметр NewServer: иначе пришлось бы править каждый вызов конструктора.
//
// nil означает «тарифов нет»: лимит безлимитный, то есть сервер ведёт себя как
// до их введения. Ноль здесь означал бы «ноль распознаваний» и тихо сломал бы
// фичу там, где её просто не настраивали.
func (s *Server) SetEntitlements(e *service.Entitlements) { s.entitlements = e }

// SetDeliverySlack проводит в сервер тот же запас на задержку доставки, что
// получил Entitlements. Без него экран подписки судил бы о «живой покупке»
// строже, чем резолв тарифа, и у платящего в окне продления пропадала бы ссылка
// «Управлять подпиской» при живом Plus. Вызывать до Run.
func (s *Server) SetDeliverySlack(d time.Duration) { s.deliverySlack = d }

// SetAI включает AI-парсинг расхода (эндпоинт /parse). Вызывать до Run.
// nil parser оставляет фичу выключенной (503). Отдельный setter, а не параметр
// NewServer — не ломает существующие вызовы конструктора в тестах.
func (s *Server) SetAI(parser ai.Parser, limiter *service.RateLimiter, maxBody int64) {
	s.aiParser = parser
	s.rateLimiter = limiter
	s.aiMaxBody = maxBody
}

// notifyTimeout ограничивает фоновую отправку уведомлений одной мутации
const notifyTimeout = 30 * time.Second

// notifyAsync выполняет notify в отдельной горутине, чтобы поход в telegram не
// задерживал ответ клиенту. Контекст отвязан от запроса (context.WithoutCancel —
// значения auth сохраняются, но завершение запроса не отменяет отправку)
// и ограничен notifyTimeout. При nil notifier (бот выключен) — no-op
func (s *Server) notifyAsync(ctx context.Context, notify func(ctx context.Context, n Notifier)) {
	if s.notifier == nil {
		return
	}
	nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	go func() {
		defer cancel()
		defer safe.Recover("фоновая отправка уведомления")
		notify(nctx, s.notifier)
	}()
}

// Handler возвращает корневой http.Handler со всеми маршрутами и middleware
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /api/v1/auth/telegram", s.handleAuthTelegram)
	mux.HandleFunc("POST /api/v1/auth/code", s.handleAuthCode)
	mux.HandleFunc("POST /api/v1/auth/google", s.handleAuthGoogle)
	mux.HandleFunc("POST /api/v1/auth/apple", s.handleAuthApple)
	mux.HandleFunc("POST /api/v1/auth/dev", s.handleAuthDev)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleAuthPassword)

	// Диплинк-вход в группу. ПУБЛИЧНЫЕ маршруты, намеренно без s.auth:
	// .well-known читают iOS и Android без всякой авторизации, а /join
	// открывает браузер человека, у которого аккаунта ещё может не быть.
	// Пустой PublicBaseUrl выключает все три (404) — см. deeplink.go
	mux.HandleFunc("GET /.well-known/apple-app-site-association", s.handleAppleAppSiteAssociation)
	mux.HandleFunc("GET /.well-known/assetlinks.json", s.handleAssetLinks)
	mux.HandleFunc("GET /join/{roomId}", s.handleJoinPage)
	// Политика и удаление аккаунта: оба магазина требуют ссылку, открываемую
	// БЕЗ входа в приложение. Публичность здесь не послабление, а требование —
	// страницу читает ревьюер, у которого аккаунта нет
	mux.HandleFunc("GET /privacy", s.handlePrivacyPolicy)
	mux.HandleFunc("GET /account-deletion", s.handleAccountDeletion)
	// /terms — вторая обязательная ссылка с экрана оплаты (Guideline 3.1.2)
	mux.HandleFunc("GET /terms", s.handleSubscriptionTerms)
	// Уведомления магазинов о подписках. Без s.auth: их аутентифицирует не наш
	// JWT, а сам факт, что состояние мы перезапрашиваем у магазина по своему
	// ключу — содержимому уведомления мы не верим (см. store_webhooks.go)
	mux.HandleFunc("POST /api/v1/webhooks/apple", s.handleAppleStoreWebhook)
	mux.HandleFunc("POST /api/v1/webhooks/google", s.handleGoogleStoreWebhook)
	// Вход через Telegram Login Widget для нативных клиентов: /tg-auth уводит
	// на oauth.telegram.org с нашим origin, /tg-callback возвращает результат
	// в приложение через splitty:// (см. tg_callback.go). Оба публичные:
	// человек здесь ещё не авторизован — он только пытается войти
	mux.HandleFunc("GET /tg-auth", s.handleTelegramAuthStart)
	mux.HandleFunc("GET /tg-callback", s.handleTelegramCallback)

	// Продуктовые события. Под обычной авторизацией: анонимного маршрута
	// записи наружу не появляется — это был бы самый дешёвый способ насыпать
	// нам в базу. Верх воронки закрывают числа App Store, а не этот поток.
	mux.Handle("POST /api/v1/events", s.auth(s.handlePostEvents))
	// Без авторизации намеренно: всё, что происходит до входа, иначе не
	// измерить. Набор имён там закрыт четырьмя (analytics.Anonymous).
	mux.HandleFunc("POST /api/v1/events/anonymous", s.handlePostAnonymousEvents)

	mux.Handle("GET /api/v1/me", s.auth(s.handleGetMe))
	mux.Handle("PATCH /api/v1/me", s.auth(s.handlePatchMe))
	// Единственный маршрут под authDeleted: удалённому аккаунту нужно позволить
	// ПОВТОРИТЬ удаление, иначе запрос, упавший после tombstone, некому довести
	// до конца — обычный auth отверг бы его 401 (см. handleDeleteMe)
	mux.Handle("DELETE /api/v1/me", s.authDeleted(s.handleDeleteMe))
	mux.Handle("GET /api/v1/me/ai-quota", s.auth(s.handleGetAiQuota))
	mux.Handle("GET /api/v1/me/subscription", s.auth(s.handleGetSubscription))
	mux.Handle("POST /api/v1/me/subscription/apple", s.auth(s.handlePostAppleSubscription))
	mux.Handle("POST /api/v1/me/subscription/google", s.auth(s.handlePostGoogleSubscription))
	mux.Handle("GET /api/v1/me/notifications", s.auth(s.handleGetNotifications))
	mux.Handle("PATCH /api/v1/me/notifications", s.auth(s.handlePatchNotifications))
	mux.Handle("POST /api/v1/me/devices", s.auth(s.handleRegisterDevice))
	// «Выйти на всех устройствах»: единственный способ отозвать украденный
	// токен, не разлогинивая заодно всех остальных сменой общего секрета
	mux.Handle("POST /api/v1/me/revoke-tokens", s.auth(s.handleRevokeTokens))
	mux.Handle("DELETE /api/v1/me/devices", s.auth(s.handleUnregisterDevice))
	mux.Handle("POST /api/v1/me/password", s.auth(s.handleSetPassword))
	mux.Handle("POST /api/v1/me/link/{provider}", s.auth(s.handleLinkIdentity))
	mux.Handle("DELETE /api/v1/me/link/{provider}", s.auth(s.handleUnlinkIdentity))

	mux.Handle("GET /api/v1/rooms", s.auth(s.handleListRooms))
	mux.Handle("POST /api/v1/rooms", s.auth(s.handleCreateRoom))
	mux.Handle("GET /api/v1/rooms/{roomId}", s.auth(s.handleGetRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/join", s.auth(s.handleJoinRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/members", s.auth(s.handleAddMember))
	mux.Handle("DELETE /api/v1/rooms/{roomId}/members/me", s.auth(s.handleLeaveRoom))
	mux.Handle("DELETE /api/v1/rooms/{roomId}/members/{userId}", s.auth(s.handleRemoveMember))
	mux.Handle("POST /api/v1/invites/{roomId}/accept", s.auth(s.handleAcceptInvite))
	mux.Handle("POST /api/v1/invites/{roomId}/decline", s.auth(s.handleDeclineInvite))
	mux.Handle("GET /api/v1/notifications", s.auth(s.handleNotifications))
	mux.Handle("POST /api/v1/me/notifications-seen", s.auth(s.handleMarkNotificationsSeen))
	mux.Handle("POST /api/v1/rooms/{roomId}/notifications-seen", s.auth(s.handleMarkRoomSeen))
	mux.Handle("PUT /api/v1/rooms/{roomId}/avatar", s.auth(s.handleSetRoomAvatar))
	mux.Handle("DELETE /api/v1/rooms/{roomId}/avatar", s.auth(s.handleDeleteRoomAvatar))
	mux.Handle("POST /api/v1/rooms/{roomId}/archive", s.auth(s.handleArchiveRoom))
	mux.Handle("POST /api/v1/rooms/{roomId}/unarchive", s.auth(s.handleUnarchiveRoom))
	mux.Handle("PUT /api/v1/rooms/{roomId}/currency", s.auth(s.handleUpdateCurrency))
	mux.Handle("PUT /api/v1/rooms/{roomId}/fractional", s.auth(s.handleUpdateFractional))
	mux.Handle("GET /api/v1/currencies", s.auth(s.handleCurrencies))

	mux.Handle("GET /api/v1/rooms/{roomId}/operations", s.auth(s.handleListOperations))
	mux.Handle("POST /api/v1/rooms/{roomId}/operations/parse", s.auth(s.handleParseOperation))
	mux.Handle("POST /api/v1/rooms/{roomId}/operations", s.auth(s.handleCreateOperation))
	mux.Handle("PUT /api/v1/rooms/{roomId}/operations/{operationId}", s.auth(s.handleUpdateOperation))
	mux.Handle("DELETE /api/v1/rooms/{roomId}/operations/{operationId}", s.auth(s.handleDeleteOperation))

	mux.Handle("GET /api/v1/rooms/{roomId}/debts", s.auth(s.handleListDebts))
	mux.Handle("POST /api/v1/rooms/{roomId}/repayments", s.auth(s.handleCreateRepayment))
	mux.Handle("GET /api/v1/rooms/{roomId}/statistics", s.auth(s.handleStatistics))

	mux.Handle("GET /api/v1/friends", s.auth(s.handleFriends))
	mux.Handle("GET /api/v1/activity", s.auth(s.handleActivity))

	mux.Handle("GET /api/v1/files/{fileId}", s.auth(s.handleGetFile))
	mux.Handle("GET /api/v1/users/{userId}/avatar", s.auth(s.handleGetUserAvatar))
	mux.Handle("POST /api/v1/users/{userId}/aliases", s.auth(s.handleAddAlias))

	// все незарегистрированные пути — 404 в едином json-формате
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
	})

	return recoverMiddleware(logMiddleware(maxBodyMiddleware(mux)))
}

// maxRequestBodyBytes лимит тела запроса — 1 МБ: защита от OOM на гигантском json
// (в т.ч. на неаутентифицированном /auth/telegram). Превышение decodeJSON отдаёт как 413
const maxRequestBodyBytes = 1 << 20

// bigBodyPaths — маршруты, которым мало общего мегабайта: загрузка медиа. Свой,
// больший лимит они ставят сами в хендлере, а re-wrap ЗДЕСЬ внешний ридер не
// снял бы — потому и исключение, а не переопределение.
var bigBodyPaths = []string{"/operations/parse", "/avatar"}

// maxBodyMiddleware ограничивает размер тела всех запросов через http.MaxBytesReader.
func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := false
		for _, suffix := range bigBodyPaths {
			if strings.HasSuffix(r.URL.Path, suffix) {
				big = true
				break
			}
		}
		if !big {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// Run запускает http-сервер, блокирующий вызов. Останавливается через Shutdown или отмену ctx
func (s *Server) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	log.Info().Msgf("rest api listening on %s", s.cfg.Listen)
	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown корректно останавливает http-сервер
func (s *Server) Shutdown() {
	if s.httpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("rest api shutdown failed")
	}
}

// SetDBPing задаёт проверку доступности базы для /health.
func (s *Server) SetDBPing(ping func(ctx context.Context) error) { s.dbPing = ping }

// SetBotHeartbeat задаёт источник времени последнего обновления telegram.
func (s *Server) SetBotHeartbeat(beat func() time.Time) { s.botHeartbeat = beat }

// healthTimeout — сколько ждём базу в проверке здоровья. Короткий: /health
// дёргают часто, и висеть он не имеет права
const healthTimeout = 2 * time.Second

// botSilenceLimit — после какого молчания бота считаем, что цикл обновлений
// встал. Длинный опрос телеграма — 60 секунд, поэтому запас втрое
const botSilenceLimit = 3 * time.Minute

// handleHealth отвечает 503, когда база недоступна.
//
// Раньше проверка отвечала «ok» всегда: сервис с упавшей базой выглядел живым,
// и снаружи это было неотличимо от рабочего состояния — ни перезапуска, ни
// сигнала. Состояние бота показывается отдельным полем, но на код ответа не
// влияет: REST работает и без него.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"status": "ok"}

	if s.dbPing != nil {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()
		if err := s.dbPing(ctx); err != nil {
			log.Error().Err(err).Msg("health: база недоступна")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable",
				"db":     "down",
			})
			return
		}
		body["db"] = "ok"
	}

	if s.botHeartbeat != nil {
		last := s.botHeartbeat()
		body["botLastUpdate"] = last
		body["bot"] = "ok"
		if last.IsZero() || s.now().Sub(last) > botSilenceLimit {
			body["bot"] = "silent"
		}
	}

	writeJSON(w, http.StatusOK, body)
}

// recoverMiddleware превращает панику в 500 с json-телом
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Msgf("panic! %s %s: %v, stack: %s", r.Method, r.URL.Path, rec, string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logMiddleware пишет request-лог через zerolog
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Dur("duration", time.Since(start)).
			Msg("rest request")
	})
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Error().Err(err).Msg("cannot write response")
	}
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

// writeErrorWithQuota — ошибка лимита вместе с состоянием квоты.
//
// Отдельный writer, а не третье поле в errorResponse: quota имеет смысл ровно
// на двух кодах отказа, и таскать пустой объект в каждой ошибке сервера
// (включая 500 и 404) незачем.
func writeErrorWithQuota(w http.ResponseWriter, status int, code, message string, quota quotaDto) {
	writeJSON(w, status, quotaErrorResponse{
		Error: errorBody{Code: code, Message: message},
		Quota: quota,
	})
}
