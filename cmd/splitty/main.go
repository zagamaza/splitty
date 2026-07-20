package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/dailyexpenses"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/rest"
	"github.com/almaznur91/splitty/internal/service"
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

	// бот опционален: без TG_TOKEN или при ошибке инициализации продолжаем только с REST API
	if cfg.TgToken == "" {
		log.Warn().Msg("TG_TOKEN is empty, telegram bot disabled, serving rest api only")
	} else if app, cl, err := initApp(ctx, cfg); err != nil {
		log.Warn().Err(err).Msg("Can not init telegram bot, serving rest api only")
	} else {
		closer.Bind(cl)
		// бот включён — REST-мутации шлют участникам те же telegram-уведомления,
		// что и экраны бота (без бота notifier остаётся nil, уведомления no-op)
		restServer.SetNotifier(bot.NewNotifier(app.TbAPI, restDeps.operationSrv, restDeps.buttonSrv, restDeps.userRepo))
		go app.DeIntegrationService.StartPostScheduler()
		go func() {
			if err := app.Do(ctx); err != nil {
				log.Error().Err(err).Msg("telegram listener failed")
			}
		}()
	}

	if err := restServer.Run(ctx); err != nil {
		log.Error().Err(err).Msg("rest api failed")
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
	roomService := service.NewRoomService(roomRepository)
	operationService := service.NewOperationService(roomRepository)
	buttonService := service.NewButtonService(repository.NewButtonRepository(db))

	restCfg := rest.Config{
		Listen:          cfg.Listen,
		JwtSecret:       jwtSecret,
		DevAuth:         cfg.ApiDevAuth,
		TgToken:         cfg.TgToken,
		ReviewLoginCode: cfg.ReviewLoginCode,
		ReviewUserId:    cfg.ReviewUserId,
	}
	server := rest.NewServer(restCfg, userRepository, roomRepository, loginCodeRepository, roomService, operationService)

	if err := loginCodeRepository.EnsureIndexes(ctx); err != nil {
		log.Warn().Err(err).Msg("cannot create login_code indexes")
	}

	// AI-парсинг расхода включается только при заданном ключе; иначе /parse → 503
	if cfg.GeminiApiKey != "" {
		aiUsageRepo := repository.NewAiUsageRepository(db)
		if err := aiUsageRepo.EnsureIndexes(ctx); err != nil {
			log.Warn().Err(err).Msg("cannot create ai_usage TTL index")
		}
		parser := ai.NewGemini(cfg.GeminiApiKey, cfg.GeminiModel)
		limiter := service.NewRateLimiter(aiUsageRepo, cfg.AiParseRatePerMin, cfg.AiParseDailyQuota)
		server.SetAI(parser, limiter, cfg.AiMaxBodyBytes)
		log.Info().Msg("AI expense parsing enabled (Gemini)")
	} else {
		log.Info().Msg("AI expense parsing disabled (GEMINI_API_KEY empty)")
	}

	return server, &restNotifierDeps{
		operationSrv: operationService,
		buttonSrv:    buttonService,
		userRepo:     userRepository,
	}, cleanup, nil
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
	tbAPI, err := tbapi.NewBotAPI(cfg.TgToken)
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
