package main

import "github.com/caarlos0/env/v6"

type config struct {
	Listen   string `env:"LISTEN" envDefault:"localhost:7171"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"debug"`
	LogFmt   string `env:"LOG_FMT" envDefault:"console"`

	DbAddr          string   `env:"DB_HOST" envDefault:"mongodb://localhost:27017/"`
	DbName          string   `env:"DB_NAME" envDefault:"splitty"`
	TgToken         string   `env:"TG_TOKEN" envDefault:"6282994135:AAG-jkpfGhM3U6_0IJ_6RA28nuaAi0sEsC0"`
	SuperUsers      []string `env:"SUPER_USER" envSeparator:":" envDefault:"mazanur:zagirnur"`
	TgDebug         bool     `env:"TG_DEBUG" envDefault:"false"`
	DefaultLanguage string   `env:"DEFAULT_LANGUAGE" envDefault:"en"`

	DailyExpensesUrl   string `env:"DAILY_EXPENSES_URL" envDefault:"http://pet.zagirnur.dev:19090/from-splitty"`
	DailyExpensesUsers []int  `env:"DAILY_EXPENSES_USERS" envSeparator:":" envDefault:"147181773:369575379:172261383:304898122:360624984:373160631"`
}

func initConfig() (*config, error) {
	cfg := &config{}

	if err := env.Parse(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
