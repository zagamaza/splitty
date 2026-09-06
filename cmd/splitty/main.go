package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/dailyexpenses"
	"github.com/almaznur91/splitty/internal/metrics"
	"github.com/almaznur91/splitty/internal/oidc"
	"github.com/almaznur91/splitty/internal/push"
	"github.com/almaznur91/splitty/internal/reminders"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/rest"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/almaznur91/splitty/internal/store"
	"github.com/gookit/i18n"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/text/language"

	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/events"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/xlab/closer"
)

var revision = "local"

func main() {
	defer closer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	closer.Bind(cancel)

	cfg, err := initConfig()
	if err != nil {
		// env.Parse возвращает частично заполненную структуру: молча продолжив,
		// мы поднимаем сервис с нулевыми лимитами (AI_MAX_BODY_BYTES=0 → каждый
		// /parse отвечает 413) и пустыми секретами. Падаем, как и на logger/REST.
		log.Fatal().Err(err).Msg("Can not init config")
	}

	initI18n(cfg)

	if err := initLogger(cfg); err != nil {
		log.Fatal().Err(err).Msg("Can not init logger")
	}

	rand.Seed(int64(time.Now().Nanosecond()))

	// REST API работает всегда, зависимости собираются вручную поверх отдельного mongo-подключения
	restServer, restDeps, restCleanup, err := initRestServer(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Can not init rest api")
	}
	closer.Bind(restCleanup)
	closer.Bind(restServer.Shutdown)

	// бот опционален: без TG_TOKEN или при ошибке инициализации продолжаем только с REST API.
	// telegram-отправитель — реальный (бот поднят) либо noop; push-канал Notifier'а
	// работает независимо от бота (FCM по REST-мутациям iOS/Android).
	var tgSender bot.TelegramSender = bot.NoopTelegramSender{}
	// botLive — поднялся ли telegram-бот на самом деле. Заглушка отправки
	// молча «доставляет» что угодно, и напоминание о долге считалось бы
	// отправленным, никуда не уйдя
	botLive := false
	if cfg.TgToken == "" {
		log.Warn().Msg("TG_TOKEN is empty, telegram bot disabled, serving rest api only")
	} else if app, cl, err := initApp(ctx, cfg); err != nil {
		log.Warn().Err(err).Msg("Can not init telegram bot, serving rest api only")
	} else {
		closer.Bind(cl)
		tgSender = app.TbAPI
		botLive = true
		// Молчащий цикл обновлений снаружи ничем не отличался от работающего
		restServer.SetBotHeartbeat(app.LastUpdate)
		go app.DeIntegrationService.StartPostScheduler()
		go func() {
			if err := app.Do(ctx); err != nil {
				log.Error().Err(err).Msg("telegram listener failed")
			}
		}()
	}
	// Сводные числа для админки и графиков. Слушатель отдельный и наружу не
	// опубликован; пересчёт по расписанию, а не на каждый scrape — часть чисел
	// требует прохода по комнатам
	if cfg.MetricsListen != "" {
		metricsServer := metrics.NewServer(
			repository.NewStatsRepository(restDeps.db), cfg.MetricsInterval)
		go metricsServer.Refresh(ctx)
		go metricsServer.Listen(ctx, cfg.MetricsListen)
	}

	// Админский API. Слушатель отдельный и наружу не опубликован: до него
	// дотягивается только соседний контейнер по сети docker. Без токена не
	// поднимается вовсе — открытым этот порт быть не может
	if cfg.AdminApiToken == "" {
		log.Info().Msg("админский api выключен (ADMIN_API_TOKEN пуст)")
	} else if cfg.AdminApiListen == "" {
		log.Info().Msg("админский api выключен (ADMIN_API_LISTEN пуст)")
	} else {
		go serveAdminAPI(ctx, cfg.AdminApiListen, restServer.AdminHandler())
	}

	// REST-мутации участникам: те же telegram-уведомления, что и экраны бота
	// (когда бот включён), + native-пуши FCM (по WantsPush).
	notifier := bot.NewNotifier(tgSender, restDeps.operationSrv, restDeps.buttonSrv, restDeps.userRepo, restDeps.pushSender)
	restServer.SetNotifier(notifier)

	// Напоминания о долге. Телеграм подключаем только при живом боте: пуш умеет
	// одно приложение, а девять должников из десяти сидят в боте — без этого
	// канала рассылка проходит мимо почти всех
	if botLive {
		restDeps.reminderJob.SetTelegram(notifier)
	} else {
		log.Warn().Msg("бот не поднят: напоминания о долге пойдут только пушами")
	}
	go restDeps.reminderJob.Start(ctx)

	if err := restServer.Run(ctx); err != nil {
		log.Error().Err(err).Msg("rest api failed")
	}
}

// serveAdminAPI поднимает слушатель админского API. Блокирующий вызов — звать
// из горутины. Падение слушателя логируется и не роняет сервис: без админки
// приложение работает, без приложения админка бессмысленна
func serveAdminAPI(ctx context.Context, addr string, handler http.Handler) {
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Info().Msgf("админский api на %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("слушатель админского api упал")
	}
}

// restNotifierDeps сервисы поверх mongo-подключения REST, из которых main
// собирает bot.Notifier, когда telegram-бот включён
type restNotifierDeps struct {
	operationSrv *service.OperationService
	buttonSrv    *service.ButtonService
	// userRepo — канонические настройки уведомлений: встроенные в комнату
	// снимки пользователей их не содержат (см. Notifier.allowsTelegram)
	userRepo repository.UserRepository
	// pushSender — доставка native-пушей (FCM) по outbox-подходу; NoopSender,
	// когда FCM не сконфигурирован.
	pushSender push.Sender
	// db — подключение к mongo: нужно сводным метрикам, которые считают по
	// коллекциям напрямую, а не через доменные репозитории
	db *mongo.Database
	// reminderJob — рассылка напоминаний о долге. Собран, но не запущен:
	// запасной telegram-канал ему выдаёт main, когда бот действительно поднят
	reminderJob *reminders.Job
}

// initRestServer собирает REST-сервер: mongo-подключение + репозитории + сервисы
// (вручную, вне wire — wire-граф бота не трогаем)
func initRestServer(ctx context.Context, cfg *config) (*rest.Server, *restNotifierDeps, func(), error) {
	jwtSecret, err := resolveJwtSecret(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validateReviewLoginCode(cfg); err != nil {
		return nil, nil, nil, err
	}
	if cfg.ApiDevAuth {
		log.Warn().Msg("!!! API_DEV_AUTH=true: POST /api/v1/auth/dev выдаёт токен под ЛЮБЫМ userId без проверки — только для разработки, НИКОГДА не включайте в проде !!!")
	}

	db, cleanup, err := initMongoConnection(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	userRepository := repository.NewUserRepository(db)
	roomRepository := repository.NewRoomRepository(db)
	loginCodeRepository := repository.NewLoginCodeRepository(db)
	// аллокатор номеров для входа через Google/Apple. Счётчик один на базу:
	// MongoUserRepository держит свой экземпляр над той же коллекцией sequence,
	// а $inc атомарен, поэтому одинаковый номер два экземпляра не выдадут
	sequenceRepository := repository.NewSequenceRepository(db)
	roomService := service.NewRoomService(roomRepository)
	operationService := service.NewOperationService(roomRepository)
	buttonService := service.NewButtonService(repository.NewButtonRepository(db))

	// Отсечка по дате выпуска токена. Кривое значение — фатально: молча
	// проигнорировать её значит оставить действующими те самые токены, ради
	// которых настройку и задали
	var tokenCutoff time.Time
	if v := strings.TrimSpace(cfg.TokenMinIssuedAt); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("TOKEN_MIN_ISSUED_AT: %w", err)
		}
		tokenCutoff = parsed
		log.Warn().Msgf("токены, выпущенные до %s, отвергаются: все, кто вошёл раньше, разлогинены", parsed)
	}

	restCfg := rest.Config{
		Listen:          cfg.Listen,
		JwtSecret:       jwtSecret,
		DevAuth:         cfg.ApiDevAuth,
		FractionalInput: cfg.FractionalInput,
		TgToken:         cfg.TgToken,
		ReviewLoginCode: cfg.ReviewLoginCode,
		ReviewUserId:    int(cfg.ReviewUserId),
		GoogleVerifier:  initGoogleVerifier(cfg),
		AppleVerifier:   initAppleVerifier(cfg),
		AppleTokens:     initAppleTokens(cfg),
		// Диплинк-вход в группу. Пустой PUBLIC_BASE_URL — фича выключена (404
		// на всех трёх маршрутах), и это штатное состояние до покупки домена.
		// Отпечатки фильтруются от пустых элементов по той же причине, что и
		// client id: envDefault:"" со сплитом даёт [""], а не пустой срез, и
		// assetlinks.json уехал бы с пустой строкой в списке отпечатков —
		// Android признал бы такой файл негодным
		PublicBaseUrl:     cfg.PublicBaseUrl,
		IosAppId:          cfg.IosAppId,
		AndroidPackage:    cfg.AndroidPackage,
		AndroidCertSha256: nonEmptyValues(cfg.AndroidCertSha256),
		IosStoreUrl:       cfg.IosStoreUrl,
		TrustedProxies:    cfg.TrustedProxyCount,
		TokenMinIssuedAt:  tokenCutoff,
		AdminToken:        cfg.AdminApiToken,
		TrustedProxyNets:  rest.ParseTrustedProxyNets(cfg.TrustedProxies),
	}
	if cfg.PublicBaseUrl == "" {
		log.Info().Msg("deep links disabled (PUBLIC_BASE_URL empty): /join and .well-known return 404")
	} else {
		log.Info().Msgf("deep links enabled on %s", cfg.PublicBaseUrl)
	}
	server := rest.NewServer(restCfg, userRepository, roomRepository, loginCodeRepository, roomService, operationService, sequenceRepository)

	// Побочные коллекции с PII. Все три создаются БЕЗУСЛОВНО: до этого
	// chat_state и bug_report жили только в графе бота (wire_gen.go), а
	// push_outbox — под условием FirebaseCredentialsFile, и удалению аккаунта
	// было физически нечем вычистить текст расхода, текст жалобы и
	// отрендеренные пуши. Сам репозиторий безвреден и без FCM — условным
	// остаётся только воркер доставки (см. ниже)
	pushOutbox := repository.NewPushOutboxRepository(db)
	// Состояния бота нужны отвязке telegram (незавершённый сценарий прежнего
	// профиля иначе подхватился бы новым, см. rest.clearChatState) и удалению
	// аккаунта. Коллекции те же, что у графа бота
	server.SetChatStates(repository.NewChatStateRepository(db))
	server.SetBugReports(repository.NewBugReportRepository(db))
	server.SetPushOutbox(pushOutbox)

	// Продуктовые события. Подключается БЕЗУСЛОВНО, даже если приём выключен:
	// purgeUserData падает на неподключённом очистителе, и падает уже после
	// tombstone — пропущенная строка означала бы аккаунт, помеченный удалённым,
	// у которого DELETE /me навсегда отвечает purge_incomplete.
	productEvents := repository.NewProductEventsRepository(db)
	if err := productEvents.EnsureIndexes(ctx); err != nil {
		log.Error().Err(err).Msg("cannot ensure product_events indexes")
	}
	server.SetProductEvents(productEvents)
	server.SetAnalyticsEnabled(cfg.AnalyticsEnabled)
	if !cfg.AnalyticsEnabled {
		log.Info().Msg("приём продуктовых событий выключен (ANALYTICS_ENABLED=false)")
	}

	// Приглашения в комнаты: кто кого позвал и в каком состоянии отношение.
	// Нужны и REST-эндпоинтам, и удалению аккаунта (там своя PII).
	inviteRepository := repository.NewInviteRepository(db)
	server.SetInvites(inviteRepository)

	// Картинки, загруженные из приложения (пока только ава группы), лежат
	// отдельной коллекцией: в документе комнаты уже все её операции и потолок
	// mongo 16 МБ
	fileRepository := repository.NewFileRepository(db)
	server.SetFiles(fileRepository)

	// Память о напоминаниях про долг: нужна и джобу, и удалению аккаунта
	debtReminderRepository := repository.NewDebtReminderRepository(db)
	server.SetDebtReminders(debtReminderRepository)

	// Напоминания о невозвращённом долге. Очередь берём напрямую, а не через
	// push.Sender: тот глотает ошибку постановки, а джобу она нужна — иначе он
	// спишет человеку попытку, которой не было
	reminderCfg := reminders.DefaultConfig()
	reminderCfg.Mode = reminders.Mode(cfg.DebtReminders)
	reminderCfg.Hour = cfg.DebtRemindersHour
	// Демо-аккаунт ревьюеров App Store — витрина, а не человек: долги в нём
	// показательные, и напоминать о них некому
	if cfg.ReviewUserId != 0 {
		reminderCfg.SkipUsers = []int{int(cfg.ReviewUserId)}
	}
	// Запускается НЕ здесь, а в main: сперва нужно понять, поднялся ли бот —
	// от этого зависит, доступен ли телеграм как запасной канал доставки
	reminderJob := reminders.NewJob(reminderCfg, roomRepository, debtReminderRepository, userRepository, pushOutbox)

	// Поиск комнат для админской панели. Включён всегда — доступ к нему решает
	// токен (см. rest.Config.AdminToken), а не наличие зависимости
	server.SetAdminRooms(roomRepository)
	server.SetAdminUsers(userRepository)

	// Проверка здоровья ходит в базу: сервис с упавшей mongo отвечал «ok» и
	// снаружи выглядел рабочим
	server.SetDBPing(func(ctx context.Context) error { return db.Client().Ping(ctx, nil) })

	// Потолок документа mongo — 16 МБ, а операции лежат ВНУТРИ документа
	// комнаты. Никто не знал, насколько мы к нему близко: печатаем самые
	// крупные комнаты один раз при старте
	roomRepository.LogLargestRooms(ctx, 5)

	if err := loginCodeRepository.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create login_code indexes")
	}

	// Индекс очереди пушей — не фатально: без него доставка работает, но
	// воркер сканирует и сортирует очередь целиком каждые 5 секунд
	if err := pushOutbox.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create push_outbox indexes")
	}

	// Индекс комнат по дате создания — не фатально, но без него суточный джоб
	// напоминаний сканирует всю коллекцию комнат
	if err := roomRepository.EnsureRoomIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create room indexes")
	}

	// Индекс files — не фатально: без него удаление файлов комнаты идёт полным
	// сканом, но загрузка и отдача авы работают
	if err := fileRepository.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create files indexes")
	}

	// Индексы личностей — фатально, в отличие от login_code: без unique sparse по
	// telegram_id/google_sub/apple_sub гонка двух первых входов одного человека
	// создаёт два аккаунта с одной личностью, и дальше повторный вход попадает в
	// случайный из них. Стартовать в таком режиме нельзя
	if err := userRepository.EnsureIndexes(ctx); err != nil {
		cleanup()
		return nil, nil, nil, errors.Wrap(err, "cannot create user identity indexes")
	}

	// Индексы приглашений — тоже фатально: запись описывает ТЕКУЩЕЕ состояние
	// отношения «человек × комната», и без unique по паре конкурентные
	// приглашения одного человека создали бы дубли, а Find возвращал бы
	// произвольный из них — принять можно было бы одно, а показываться другое
	if err := inviteRepository.EnsureIndexes(ctx); err != nil {
		cleanup()
		return nil, nil, nil, errors.Wrap(err, "cannot create room_invite indexes")
	}

	// Порядок фиксирован: сначала индексы, потом бэкфилл. Индекс по telegram_id
	// — sparse, поэтому он спокойно строится на документах, где поля ещё нет;
	// обратный порядок ничего бы не дал. Ошибка фатальна: сервер без
	// проставленных telegram_id не может ни отправить telegram-уведомление, ни
	// найти существующего пользователя по telegram-личности
	//
	// Выполняется ВСЕГДА и ни с какими флагами не связан. Раньше бэкфилл
	// пропускался при API_DEV_AUTH=true (dev-аккаунты попадают ровно по его
	// фильтру), но привязка МИГРАЦИИ ДАННЫХ к флагу АВТОРИЗАЦИИ означала, что
	// маркер миграции не записывался вовсе: на инсталляции с историческими
	// telegram-пользователями бот успевал завести им вторые, пустые профили, а
	// первый же старт с выключенным флагом падал на duplicate key — и уходил в
	// crash-loop, из которого нет выхода без правки базы руками. Dev-аккаунты
	// теперь отсекаются по собственному признаку (dev_auth), а не по режиму
	// инсталляции — см. repository.BackfillTelegramID
	if _, err := repository.BackfillTelegramID(ctx, db); err != nil {
		cleanup()
		return nil, nil, nil, errors.Wrap(err, "cannot backfill telegram_id")
	}

	// Раздел «Уведомления» считает непрочитанным всё новее notifications_seen_at,
	// а до этой выкатки поля не было ни у кого: без бэкфилла в день релиза вкладка
	// загорелась бы «99+» у каждого действующего пользователя — по расходам,
	// которые он давно видел. См. repository.BackfillNotificationsSeenAt
	if _, err := repository.BackfillNotificationsSeenAt(ctx, db); err != nil {
		cleanup()
		return nil, nil, nil, errors.Wrap(err, "cannot backfill notifications_seen_at")
	}

	// Дубли push-токенов, накопленные прежней неатомарной регистрацией: одно
	// устройство получало столько пушей, сколько его токен лежал копий.
	// См. repository.DedupePushTokens
	if _, err := repository.DedupePushTokens(ctx, db); err != nil {
		cleanup()
		return nil, nil, nil, errors.Wrap(err, "cannot dedupe push tokens")
	}

	// Подписки Splitor Plus. Коллекция и резолв тарифа заводятся ВСЕГДА, даже
	// когда ключи сторов пусты: без них никто не станет платным, но тариф всё
	// равно надо у кого-то спрашивать — иначе бесплатный лимит негде взять.
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	if err := subscriptionRepo.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create subscriptions indexes")
	}
	compUserIds := intsFromInt64(cfg.PlusCompUserIds)
	entitlements := service.NewEntitlements(subscriptionRepo, service.EntitlementsConfig{
		FreeQuota:     cfg.AiFreeDailyQuota,
		PlusQuota:     cfg.AiPlusDailyQuota,
		LegacyQuota:   cfg.AiLegacyDailyQuota,
		DeliverySlack: cfg.PlusDeliverySlack,
		CompUserIds:   compUserIds,
		CacheTTL:      cfg.PlusTierCacheTTL,
	})
	server.SetEntitlements(entitlements)

	// Гранты: Plus по решению админа, третий источник тарифа рядом со списком в
	// окружении и покупкой. Заводится всегда — выдача идёт из панели, ключи
	// сторов к ней отношения не имеют.
	plusGrantRepo := repository.NewPlusGrantRepository(db)
	if err := plusGrantRepo.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create plus grants indexes")
	}
	entitlements.SetGrants(plusGrantRepo)
	server.SetPlusGrants(plusGrantRepo)
	// Тот же запас, что у резолва тарифа: экран подписки не имеет права судить
	// о «живой покупке» строже, чем Entitlements.
	server.SetDeliverySlack(cfg.PlusDeliverySlack)

	// Проверка чеков. Пустые ключи — покупки выключены: эндпоинты подписки
	// отдают 503, никто не становится платным, остальное работает как раньше.
	var appleReceipts, googleReceipts rest.ReceiptVerifier
	var googleAck rest.PurchaseAcknowledger
	if a, err := store.NewApple(store.AppleConfig{
		KeyContent:         []byte(cfg.AppleIapPrivateKey),
		KeyID:              cfg.AppleIapKeyId,
		Issuer:             cfg.AppleIapIssuerId,
		BundleID:           cfg.AppleIapBundleId,
		AllowedEnvironment: cfg.StoreAllowedEnvironment,
		ProductIds:         nonEmptyValues(cfg.PlusProductIds),
	}); err != nil {
		log.Info().Msg("покупки App Store выключены (APPLE_IAP_* пусты)")
	} else {
		appleReceipts = a
	}
	if saJson, err := os.ReadFile(cfg.GooglePlaySaJson); err == nil {
		if g, gErr := store.NewGoogle(ctx, store.GoogleConfig{
			ServiceAccountJSON: saJson,
			PackageName:        cfg.GooglePlayPackage,
			ProductIds:         nonEmptyValues(cfg.PlusProductIds),
			AllowedEnvironment: cfg.StoreAllowedEnvironment,
		}); gErr != nil {
			log.Warn().Err(gErr).Msg("не удалось поднять Play Developer API, покупки Google выключены")
		} else {
			googleReceipts, googleAck = g, g
		}
	} else {
		log.Info().Msg("покупки Google Play выключены (GOOGLE_PLAY_SA_JSON недоступен)")
	}
	server.SetSubscriptions(subscriptionRepo, appleReceipts, googleReceipts, googleAck)

	// Чеки песочницы — только тем, кто и так получает Plus без покупки:
	// владельцу проекта и демо-аккаунту ревьюера. Ревьюер покупает в песочнице,
	// и отказ по окружению показал бы ему сломанную кнопку «Оформить».
	if len(cfg.PlusCompUserIds) > 0 {
		var appleSandbox, googleSandbox rest.ReceiptVerifier
		if a, err := store.NewApple(store.AppleConfig{
			KeyContent: []byte(cfg.AppleIapPrivateKey),
			KeyID:      cfg.AppleIapKeyId,
			Issuer:     cfg.AppleIapIssuerId,
			BundleID:   cfg.AppleIapBundleId,
			ProductIds: nonEmptyValues(cfg.PlusProductIds),
		}); err == nil {
			appleSandbox = a
		}
		if saJson, err := os.ReadFile(cfg.GooglePlaySaJson); err == nil {
			if g, gErr := store.NewGoogle(ctx, store.GoogleConfig{
				ServiceAccountJSON: saJson,
				PackageName:        cfg.GooglePlayPackage,
				ProductIds:         nonEmptyValues(cfg.PlusProductIds),
			}); gErr == nil {
				googleSandbox = g
			}
		}
		server.SetSandboxReceipts(compUserIds, appleSandbox, googleSandbox)
	}

	// Уведомления магазинов и фоновая доработка. Состояние подписки они берут у
	// магазина, а не из уведомления, поэтому нужен тот же клиент.
	var appleStatus service.AppleStatusReader
	var googleStatus service.GoogleStatusReader
	if a, ok := appleReceipts.(service.AppleStatusReader); ok {
		appleStatus = a
	}
	if g, ok := googleReceipts.(service.GoogleStatusReader); ok {
		googleStatus = g
	}
	server.SetStoreWebhooks(appleStatus, googleStatus)

	if appleStatus != nil || googleStatus != nil {
		worker := service.NewSubscriptionWorker(subscriptionRepo, appleStatus, googleStatus, entitlements,
			service.SubscriptionWorkerConfig{})
		go worker.Run(ctx)
	}

	// AI-парсинг расхода включается только при заданном ключе; иначе /parse → 503
	if cfg.GeminiApiKey != "" {
		aiUsageRepo := repository.NewAiUsageRepository(db)
		if err := aiUsageRepo.EnsureIndexes(ctx); err != nil {
			log.Warn().Err(err).Msg("cannot create ai_usage TTL index")
		}
		parser := ai.NewGemini(cfg.GeminiApiKey, cfg.GeminiModel)
		limiter := service.NewRateLimiter(aiUsageRepo, cfg.AiParseRatePerMin)
		server.SetAI(parser, limiter, cfg.AiMaxBodyBytes)
		log.Info().Msg("AI expense parsing enabled (Gemini)")
	} else {
		log.Info().Msg("AI expense parsing disabled (GEMINI_API_KEY empty)")
	}

	// Push (FCM) по outbox-подходу: персистентная очередь push_outbox + фоновый
	// воркер с ретраями/бэк-оффом. Пустой credentials-файл — NoopSender (пуши
	// выключены), остальной сервер работает как раньше.
	var pushSender push.Sender = push.NoopSender{}
	if cfg.FirebaseCredentialsFile != "" {
		worker, wErr := push.NewWorker(ctx, cfg.FirebaseCredentialsFile, pushOutbox, userRepository, userRepository)
		if wErr != nil {
			log.Warn().Err(wErr).Msg("cannot init FCM worker, push disabled")
		} else {
			pushSender = push.NewSender(pushOutbox)
			go worker.Run(ctx)
			log.Info().Msg("FCM push enabled (outbox worker started)")
		}
	} else {
		log.Info().Msg("FCM push disabled (FIREBASE_CREDENTIALS_FILE empty)")
	}

	return server, &restNotifierDeps{
		operationSrv: operationService,
		buttonSrv:    buttonService,
		userRepo:     userRepository,
		pushSender:   pushSender,
		db:           db,
		reminderJob:  reminderJob,
	}, cleanup, nil
}

// initGoogleVerifier собирает верификатор ID-токенов Google. Возвращает nil
// (вход через Google выключен, хендлер отвечает 503), когда не задан ни один
// client id.
//
// Фильтрация пустых элементов обязательна: env-парсер со сплитом по ":" на
// envDefault:"" отдаёт срез из одной ПУСТОЙ строки, а не пустой срез. Без неё
// len(clientIDs) == 1, верификатор считался бы сконфигурированным с невозможным
// audience "" и на каждый честный токен отвечал бы 401 вместо честного 503 —
// вместо «фича не настроена» клиент видел бы «вас не пускают»
func initGoogleVerifier(cfg *config) oidc.Verifier {
	clientIDs := nonEmptyValues(cfg.GoogleClientIds)
	if len(clientIDs) == 0 {
		log.Info().Msg("Google sign-in disabled (GOOGLE_CLIENT_IDS empty)")
		return nil
	}
	log.Info().Msgf("Google sign-in enabled for %d client id(s)", len(clientIDs))
	return oidc.NewGoogle(clientIDs)
}

// initAppleVerifier собирает верификатор ID-токенов Sign in with Apple.
// Возвращает nil (вход через Apple выключен, хендлер отвечает 503), когда не
// задан ни один client id. Фильтрация пустых элементов обязательна по той же
// причине, что и у Google (см. initGoogleVerifier)
func initAppleVerifier(cfg *config) oidc.Verifier {
	clientIDs := nonEmptyValues(cfg.AppleClientIds)
	if len(clientIDs) == 0 {
		log.Info().Msg("Apple sign-in disabled (APPLE_CLIENT_IDS empty)")
		return nil
	}
	log.Info().Msgf("Apple sign-in enabled for %d client id(s)", len(clientIDs))
	return oidc.NewApple(clientIDs)
}

// initAppleTokens собирает клиента token-эндпоинтов Apple: обмен
// authorizationCode на refresh token при входе (POST /auth/token) и отзыв этого
// токена при удалении аккаунта (POST /auth/revoke, Guideline 5.1.1(v)).
//
// Возвращает nil, когда ключ .p8 не задан или собрать клиента не удалось —
// вход через Apple продолжает работать, просто без refresh token. Требовать
// ключ Apple для локальной разработки нельзя, а падать на старте из-за него —
// значит класть весь сервис ради одной необязательной интеграции.
//
// client id берётся первый из списка: client_secret подписывается ровно под
// один sub, и это bundle id приложения (Services ID веб-потока, если он
// когда-нибудь появится, потребует отдельного секрета)
func initAppleTokens(cfg *config) oidc.AppleTokens {
	if strings.TrimSpace(cfg.ApplePrivateKey) == "" {
		log.Info().Msg("Apple token exchange disabled (APPLE_PRIVATE_KEY empty): apple tokens cannot be revoked on account deletion")
		return nil
	}
	clientIDs := nonEmptyValues(cfg.AppleClientIds)
	if len(clientIDs) == 0 {
		log.Warn().Msg("Apple token exchange disabled: APPLE_PRIVATE_KEY set without APPLE_CLIENT_IDS")
		return nil
	}
	client, err := oidc.NewAppleTokenClient(cfg.AppleTeamId, cfg.AppleKeyId, clientIDs[0], cfg.ApplePrivateKey)
	if err != nil {
		log.Warn().Err(err).Msg("cannot init apple token client: apple token exchange disabled")
		return nil
	}
	log.Info().Msg("Apple token exchange enabled")
	return client
}

// nonEmptyValues отбрасывает пустые элементы env-списка (см. initGoogleVerifier)
func nonEmptyValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// intsFromInt64 приводит номера пользователей к int.
//
// В конфиге они int64 не от хорошей жизни: caarlos0/env разбирает поле типа int
// через ParseInt с bitSize 32 независимо от разрядности платформы, и номер из
// аллокатора (1000000000004) роняет старт. Внутри же номер пользователя —
// обычный int (api.User.ID). Нули отбрасываются: пустая переменная со сплитом
// даёт [""], а не пустой срез, и превратилась бы в comp-доступ для id 0.
func intsFromInt64(values []int64) []int {
	out := make([]int, 0, len(values))
	for _, v := range values {
		if v != 0 {
			out = append(out, int(v))
		}
	}
	return out
}

// resolveJwtSecret применяет политику JWT-секрета: пустой API_JWT_SECRET допустим
// только при API_DEV_AUTH=true — тогда генерируется случайный эфемерный секрет
// (все выданные токены протухают при рестарте); иначе — фатальная ошибка старта.
// Дефолтного секрета в коде нет намеренно: он позволял бы подделывать токены.
func resolveJwtSecret(cfg *config) (string, error) {
	if cfg.ApiJwtSecret != "" {
		return cfg.ApiJwtSecret, nil
	}
	if !cfg.ApiDevAuth {
		return "", errors.New("API_JWT_SECRET не задан: задайте непустой секрет подписи JWT " +
			"(например `openssl rand -hex 32`); пустой секрет допустим только при API_DEV_AUTH=true (режим разработки)")
	}
	buf := make([]byte, 32)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", errors.Wrap(err, "cannot generate ephemeral jwt secret")
	}
	log.Warn().Msg("!!! API_JWT_SECRET не задан: сгенерирован СЛУЧАЙНЫЙ ЭФЕМЕРНЫЙ секрет — все выданные токены перестанут работать после рестарта; НИКОГДА не используйте это в проде !!!")
	return hex.EncodeToString(buf), nil
}

// reviewLoginCodeMinLen минимальная длина REVIEW_LOGIN_CODE. Код постоянный и
// сам по себе даёт 90-дневный токен демо-аккаунта, поэтому «APPLE2026» и прочие
// угадываемые строки недопустимы: требуем длину и разнообразие символов
const reviewLoginCodeMinLen = 16

// validateReviewLoginCode проверяет конфигурацию многоразового кода ревьюеров:
// код без REVIEW_USER_ID бесполезен, а короткий/однообразный — перебираем
func validateReviewLoginCode(cfg *config) error {
	code := strings.TrimSpace(cfg.ReviewLoginCode)
	if code == "" {
		return nil
	}
	if cfg.ReviewUserId == 0 {
		return errors.New("REVIEW_LOGIN_CODE задан без REVIEW_USER_ID: код входа ревьюеров некуда логинить")
	}
	distinct := map[rune]bool{}
	for _, r := range code {
		distinct[r] = true
	}
	if len(code) < reviewLoginCodeMinLen || len(distinct) < 8 {
		return fmt.Errorf("REVIEW_LOGIN_CODE слишком слабый: нужно минимум %d символов и минимум 8 различных "+
			"(код постоянный и выдаёт 90-дневный токен; сгенерируйте, например, `openssl rand -hex 16`)", reviewLoginCodeMinLen)
	}
	return nil
}

type tgLogger struct {
	zerolog.Logger
}

func (i *tgLogger) Println(v ...interface{}) { i.Print(v...) }

func initTelegramApi(cfg *config, bcfg *bot.Config) (*tbapi.BotAPI, error) {
	_ = tbapi.SetLogger(&tgLogger{log.Output(zerolog.ConsoleWriter{Out: os.Stdout})})
	// Свой клиент вместо http.DefaultClient: у того нет таймаутов вовсе, и
	// зависший запрос к telegram держал бы цикл обновлений бесконечно.
	// Общий таймаут заведомо больше окна длинного опроса (60 секунд) — иначе
	// он рвал бы КАЖДЫЙ опрос, а не только зависший
	httpClient := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	tbAPI, err := tbapi.NewBotAPIWithClient(cfg.TgToken, httpClient)
	if err != nil {
		log.Error().Err(err).Msg("can't make telegram bot")
		return nil, err
	}
	log.Info().Msg("super users: " + strings.Join(cfg.SuperUsers, ","))

	bcfg.BotName = tbAPI.Self.UserName
	tbAPI.Debug = cfg.TgDebug
	log.Info().Msgf("BotName: %s", bcfg.BotName)
	return tbAPI, nil
}

func initTelegramConfig(tbAPI *tbapi.BotAPI, bots []bot.Interface, bs events.ButtonService, us events.UserService,
	cs events.ChatStateService, de events.DeIntegrationService) (*events.TelegramListener, error) {
	multiBot := bot.MultiBot(bots)

	tgListener := &events.TelegramListener{
		TbAPI:            tbAPI,
		Bots:             multiBot,
		ChatStateService: cs,
		ButtonService:    bs,
		UserService:      us,

		DeIntegrationService: de,
	}

	return tgListener, nil
}

func initLogger(c *config) error {
	log.Debug().Msg("initialize logger")
	logLvl, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel))
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(logLvl)
	switch c.LogFmt {
	case "console":
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	case "json":
	default:
		return fmt.Errorf("unknown output format %service", c.LogFmt)
	}
	return nil
}

func initMongoConnection(ctx context.Context, cfg *config) (*mongo.Database, func(), error) {
	client, err := mongo.NewClient(options.Client().ApplyURI(cfg.DbAddr))
	if err != nil {
		return nil, nil, err
	}

	// Create connect
	err = client.Connect(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Check the connection
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	return client.Database(cfg.DbName), func() {
		// только Error: закрывашка выполняется в цепочке closer'ов, и Fatal
		// здесь означал бы os.Exit(1) прямо из неё — оставшиеся закрывашки
		// (в первую очередь restServer.Shutdown) не отработали бы, обрубив
		// висящие http-запросы из-за разовой ошибки отключения от mongo
		if err := client.Disconnect(ctx); err != nil {
			log.Error().Err(err).Msg("error while disconnect from mongo")
		}
	}, nil
}

func initBotConfig(c *config) *bot.Config {
	cfg := &bot.Config{
		SuperUsers: c.SuperUsers,
	}
	return cfg
}

func initDeConfig(c *config) *dailyexpenses.Config {
	cfg := &dailyexpenses.Config{
		Url:   c.DailyExpensesUrl,
		Users: c.DailyExpensesUsers,
	}
	return cfg
}

func initI18n(c *config) {
	languages := map[string]string{
		language.English.String(): "English",
		language.Russian.String(): "Русский",
	}
	i18n.Init("conf/lang", c.DefaultLanguage, languages)
}
