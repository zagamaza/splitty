package main

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/reporter"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/jessevdk/go-flags"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/events"
	"github.com/go-pkgz/lgr"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/xlab/closer"
)

var opts struct {
	RtjcPort             int              `short:"p" long:"port" env:"RTJC_PORT" default:"18001" description:"rtjc port room"`
	LogsPath             string           `short:"l" long:"logs" env:"TELEGRAM_LOGS" default:"logs" description:"path to logs"`
	SuperUsers           events.SuperUser `long:"super" description:"super-users"`
	MashapeToken         string           `long:"mashape" env:"MASHAPE_TOKEN" description:"mashape token"`
	SysData              string           `long:"sys-data" env:"SYS_DATA" default:"data" description:"location of sys data"`
	NewsArticles         int              `long:"max-articles" env:"MAX_ARTICLES" default:"5" description:"max number of news articles"`
	IdleDuration         time.Duration    `long:"idle" env:"IDLE" default:"30s" description:"idle duration"`
	ExportNum            int              `long:"export-num" description:"show number for export"`
	ExportPath           string           `long:"export-path" default:"logs" description:"path to export directory"`
	ExportDay            int              `long:"export-day" description:"day in yyyymmdd"`
	TemplateFile         string           `long:"export-template" default:"logs.html" description:"path to template file"`
	ExportBroadcastUsers events.SuperUser `long:"broadcast" description:"broadcast-users"`

	Dbg bool `long:"dbg" env:"DEBUG" description:"debug mode"`
}

var revision = "local"

func main() {
	defer closer.Close()
	ctx := context.Background()

	fmt.Printf("radio-t bot, %service\n", revision)
	if _, err := flags.Parse(&opts); err != nil {
		log.Err(err).Msg("[ERROR] failed to parse flags")
		os.Exit(1)
	}

	cfg, err := initConfig()
	if err != nil {
		log.Err(err).Msg("Can not init config")
	}

	if err := initLogger(cfg); err != nil {
		log.Fatal().Err(err).Msg("Can not init logger")
	}
	setupLog(opts.Dbg)

	rand.Seed(int64(time.Now().Nanosecond()))

	app, cl, err := initApp(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("Can not init application")
		return
	}
	closer.Bind(cl)

	if err := app.Do(ctx); err != nil {
		log.Error().Err(err).Msg("telegram listener failed")
		return
	}
}

func initTelegramConfig(ctx context.Context, cfg *config, sc *service.UserService, rs *service.RoomService) (*events.TelegramListener, error) {
	tbAPI, err := tbapi.NewBotAPI(cfg.TgToken)
	if err != nil {
		log.Error().Err(err).Msg("[ERROR] can't make telegram bot")
		return nil, err
	}
	tbAPI.Debug = cfg.LogLevel == "debug"
	log.Info().Msg("super users: " + strings.Join(cfg.SuperUsers, ","))

	multiBot := bot.MultiBot{
		bot.NewStart(sc, rs),
		bot.NewRoom(sc, rs),
	}

	tgListener := &events.TelegramListener{
		TbAPI:        tbAPI,
		Bots:         multiBot,
		Debug:        opts.Dbg,
		MsgLogger:    reporter.NewLogger(opts.LogsPath),
		IdleDuration: opts.IdleDuration,
		Service:      *sc,
		SuperUsers:   cfg.SuperUsers,
	}

	return tgListener, nil
}

func setupLog(dbg bool) {
	logOpts := []lgr.Option{lgr.Msec, lgr.LevelBraces}
	if dbg {
		logOpts = []lgr.Option{lgr.Debug, lgr.CallerFile, lgr.CallerFunc, lgr.Msec, lgr.LevelBraces}
	}
	lgr.SetupStdLogger(logOpts...)
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
		if err := client.Disconnect(ctx); err != nil {
			log.Fatal().Err(err).Msg("error while connect to mongo")
		}
	}, nil
}
