package main

import (
	"context"
	"fmt"
	"github.com/almaznur91/splitty/internal/reporter"
	"github.com/almaznur91/splitty/internal/repository"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/jessevdk/go-flags"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/almaznur91/splitty/internal/bot"
	"github.com/almaznur91/splitty/internal/events"
	"github.com/go-pkgz/lgr"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
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
	ctx := context.TODO()

	fmt.Printf("radio-t bot, %s\n", revision)
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
	database, _, err := initMongoConnection(ctx, cfg)
	mongoMineRepository := repository.New(database)
	mineService := service.New(mongoMineRepository)

	if err := initTelegramConfig(ctx, cfg, mineService); err != nil {
		log.Fatal().Err(err).Msg("[ERROR] telegram listener failed")

	}
}

func initTelegramConfig(ctx context.Context, cfg *config, sc service.Service) error {
	httpClient := &http.Client{Timeout: 5 * time.Second}

	tbAPI, err := tbapi.NewBotAPI(cfg.TgToken)
	if err != nil {
		log.Fatal().Err(err).Msg("[ERROR] can't make telegram bot")
	}
	tbAPI.Debug = cfg.LogLevel == "debug"
	log.Info().Msg("super users: " + strings.Join(cfg.SuperUsers, ","))

	multiBot := bot.MultiBot{
		bot.NewBroadcastStatus(
			ctx,
			bot.BroadcastParams{
				URL:          "https://stream.radio-t.com",
				PingInterval: 10 * time.Second,
				DelayToOff:   time.Minute,
				Client:       http.Client{Timeout: 5 * time.Second}}),
		bot.NewNews(httpClient, "https://news.radio-t.com/api", opts.NewsArticles),
		bot.NewAnecdote(httpClient),
		bot.NewStackOverflow(),
		bot.NewStart(sc),
		bot.NewDuck(opts.MashapeToken, httpClient),
		bot.NewPodcasts(httpClient, "https://radio-t.com/site-api", 5),
		bot.NewPrepPost(httpClient, "https://radio-t.com/site-api", 5*time.Minute),
		bot.NewWTF(time.Hour*24, 7*time.Hour*24, opts.SuperUsers),
	}

	if sb, err := bot.NewSys(opts.SysData); err == nil {
		multiBot = append(multiBot, sb)
	} else {
		log.Printf("[ERROR] failed to load sysbot, %v", err)
	}

	tgListener := events.TelegramListener{
		TbAPI:        tbAPI,
		Bots:         multiBot,
		Debug:        opts.Dbg,
		MsgLogger:    reporter.NewLogger(opts.LogsPath),
		IdleDuration: opts.IdleDuration,
		Service:      sc,
		SuperUsers:   cfg.SuperUsers,
	}

	if err := tgListener.Do(ctx); err != nil {
		log.Fatal().Err(err).Msg("[ERROR] telegram listener failed")
	}
	return nil
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
		return fmt.Errorf("unknown output format %s", c.LogFmt)
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
