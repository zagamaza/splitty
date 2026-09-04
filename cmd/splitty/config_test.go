package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// Ловушка env-парсера, из-за которой вход через Google выглядел бы настроенным
// там, где он выключен: envDefault:"" со сплитом по разделителю даёт срез из
// ОДНОЙ ПУСТОЙ строки, а не пустой срез. Без фильтрации верификатор собрался бы
// с audience "" — совпасть с ним не может ни один настоящий токен, и клиент
// вместо честного 503 «не сконфигурировано» получал бы 401 «вас не пускают»
func TestGoogleClientIdsFiltering(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		wantIDs  []string
		wantOnly bool // ожидается ли собранный верификатор
	}{
		{name: "не задано", env: "", wantIDs: nil, wantOnly: false},
		{name: "только разделители", env: "::", wantIDs: nil, wantOnly: false},
		{name: "один id", env: "app.apps.googleusercontent.com", wantIDs: []string{"app.apps.googleusercontent.com"}, wantOnly: true},
		{name: "пустой элемент между id", env: "ios.id::android.id", wantIDs: []string{"ios.id", "android.id"}, wantOnly: true},
		{name: "пробелы вокруг id", env: " ios.id : android.id ", wantIDs: []string{"ios.id", "android.id"}, wantOnly: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLIENT_IDS", tt.env)
			cfg, err := initConfig()
			if err != nil {
				t.Fatalf("initConfig: %v", err)
			}

			got := nonEmptyValues(cfg.GoogleClientIds)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("client ids = %q, want %q", got, tt.wantIDs)
			}
			for i := range got {
				if got[i] != tt.wantIDs[i] {
					t.Fatalf("client ids = %q, want %q", got, tt.wantIDs)
				}
			}

			verifier := initGoogleVerifier(cfg)
			if tt.wantOnly && verifier == nil {
				t.Error("верификатор не собран, вход через Google отвечал бы 503 при заданных client id")
			}
			if !tt.wantOnly && verifier != nil {
				t.Error("верификатор собран без client id: эндпоинт отвечал бы 401 вместо честного 503")
			}
		})
	}
}

// Та же ловушка env-парсера, что и у Google: без фильтрации вход через Apple
// собрался бы с audience "" и отвечал 401 вместо честного 503
func TestAppleClientIdsFiltering(t *testing.T) {
	tests := []struct {
		name        string
		env         string
		wantEnabled bool
	}{
		{name: "не задано", env: "", wantEnabled: false},
		{name: "только разделители", env: "::", wantEnabled: false},
		{name: "bundle id", env: "dev.zagirnur.splitty", wantEnabled: true},
		{name: "bundle id и services id", env: "dev.zagirnur.splitty::dev.zagirnur.splitty.web", wantEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APPLE_CLIENT_IDS", tt.env)
			cfg, err := initConfig()
			if err != nil {
				t.Fatalf("initConfig: %v", err)
			}
			verifier := initAppleVerifier(cfg)
			if tt.wantEnabled && verifier == nil {
				t.Error("верификатор не собран, вход через Apple отвечал бы 503 при заданных client id")
			}
			if !tt.wantEnabled && verifier != nil {
				t.Error("верификатор собран без client id: эндпоинт отвечал бы 401 вместо честного 503")
			}
		})
	}
}

// Отпечаток сертификата САМ состоит из байтов через двоеточие, поэтому
// привычный для проекта envSeparator ":" разорвал бы его на 32 куска и
// assetlinks.json уехал бы негодным. Разделитель здесь — запятая
func TestAndroidCertSha256Separator(t *testing.T) {
	const play = "E6:8C:8C:AF:20:18:20:2B:E3:93:BF:BE:AE:B9:DA:E6:AB:E7:BD:AE:AA:39:D2:20:9D:24:E4:75:B4:ED:E7:D0"
	const debug = "8B:F8:FC:55:7B:4B:14:79:7C:93:7C:9A:2D:A6:7F:2D:4E:49:D1:E6:00:11:22:33:44:55:66:77:88:99:AA:BB"

	tests := []struct {
		name string
		env  string
		want []string
	}{
		{name: "не задано", env: "", want: nil},
		{name: "один отпечаток", env: play, want: []string{play}},
		{name: "release и debug", env: play + "," + debug, want: []string{play, debug}},
		{name: "пробелы вокруг", env: " " + play + " , " + debug + " ", want: []string{play, debug}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANDROID_CERT_SHA256", tt.env)
			cfg, err := initConfig()
			if err != nil {
				t.Fatalf("initConfig: %v", err)
			}
			got := nonEmptyValues(cfg.AndroidCertSha256)
			if len(got) != len(tt.want) {
				t.Fatalf("fingerprints = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("fingerprints = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// Локальная разработка не обязана иметь ключ .p8: пустой APPLE_PRIVATE_KEY
// выключает обмен, а вход через Apple продолжает работать
func TestAppleTokensDisabledWithoutPrivateKey(t *testing.T) {
	t.Setenv("APPLE_CLIENT_IDS", "dev.zagirnur.splitty")
	t.Setenv("APPLE_TEAM_ID", "TEAM123456")
	t.Setenv("APPLE_KEY_ID", "KEY7890AB")
	t.Setenv("APPLE_PRIVATE_KEY", "")

	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if tokens := initAppleTokens(cfg); tokens != nil {
		t.Error("обмен токенов собран без ключа .p8")
	}
	if verifier := initAppleVerifier(cfg); verifier == nil {
		t.Error("отсутствие ключа .p8 не должно выключать сам вход через Apple")
	}
}

// Негодный ключ не роняет старт: обмен просто выключается (нельзя класть весь
// сервис из-за необязательной интеграции)
func TestAppleTokensSurviveBrokenKey(t *testing.T) {
	t.Setenv("APPLE_CLIENT_IDS", "dev.zagirnur.splitty")
	t.Setenv("APPLE_TEAM_ID", "TEAM123456")
	t.Setenv("APPLE_KEY_ID", "KEY7890AB")
	t.Setenv("APPLE_PRIVATE_KEY", "не PEM вовсе")

	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if tokens := initAppleTokens(cfg); tokens != nil {
		t.Error("обмен собран на негодном ключе")
	}
}

// Полный набор (client id + team/key id + валидный .p8) собирает обмен токенов.
// Ключ генерируется локально: настоящий ключ Apple в тестах не нужен и в
// репозитории его быть не может
func TestAppleTokensEnabledWithValidKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate ec key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("cannot marshal pkcs8: %v", err)
	}

	t.Setenv("APPLE_CLIENT_IDS", "dev.zagirnur.splitty")
	t.Setenv("APPLE_TEAM_ID", "TEAM123456")
	t.Setenv("APPLE_KEY_ID", "KEY7890AB")
	t.Setenv("APPLE_PRIVATE_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))

	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if tokens := initAppleTokens(cfg); tokens == nil {
		t.Error("обмен токенов не собран при полном наборе настроек")
	}
}

// Номер демо-аккаунта не помещался в 32 бита, и прод падал в краш-луп с
// «value out of range»: caarlos0/env v6 разбирает поле типа int через ParseInt
// с bitSize 32 независимо от разрядности платформы. Аллокатор выдаёт номера
// вида 1000000000004 — заметно больше MaxInt32, так что поле обязано быть int64.
func TestReviewUserIdFitsAllocatorNumbers(t *testing.T) {
	const allocated = 1000000000004 // реальный номер демо-аккаунта ревью

	t.Setenv("REVIEW_USER_ID", "1000000000004")
	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig с большим REVIEW_USER_ID: %v", err)
	}
	if cfg.ReviewUserId != allocated {
		t.Fatalf("ReviewUserId = %d, ожидалось %d", cfg.ReviewUserId, allocated)
	}
}

// Конфигурация тарифов. Все три проверки — про деньги: ошибка в любую сторону
// либо раздаёт платное бесплатно, либо ломает распознавание всем сразу.

func TestQuotaConfigDefaults(t *testing.T) {
	t.Setenv("API_JWT_SECRET", "x")
	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if cfg.AiFreeDailyQuota != 5 {
		t.Errorf("AiFreeDailyQuota = %d, хотели 5", cfg.AiFreeDailyQuota)
	}
	if cfg.AiPlusDailyQuota != -1 {
		t.Errorf("AiPlusDailyQuota = %d, хотели -1 (безлимит)", cfg.AiPlusDailyQuota)
	}
	if cfg.AiLegacyDailyQuota != 50 {
		t.Errorf("AiLegacyDailyQuota = %d, хотели 50", cfg.AiLegacyDailyQuota)
	}
}

// Прежнее имя переменной побеждает новый дефолт: окружение, задеплоенное до
// введения тарифов, не должно молча провалиться с 50 на 5.
func TestLegacyQuotaEnvNameStillHonored(t *testing.T) {
	t.Setenv("API_JWT_SECRET", "x")
	t.Setenv("AI_PARSE_DAILY_QUOTA", "42")

	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if cfg.AiFreeDailyQuota != 42 {
		t.Errorf("AiFreeDailyQuota = %d, хотели 42 из старого имени переменной", cfg.AiFreeDailyQuota)
	}
}

// Ноль отвергается на старте. Если бы ноль означал безлимит, любая пустая или
// битая переменная тихо раздавала бы платное всем — и платил бы за это проект.
func TestZeroQuotaRefusesToStart(t *testing.T) {
	vars := []string{"AI_FREE_DAILY_QUOTA", "AI_PLUS_DAILY_QUOTA", "AI_LEGACY_DAILY_QUOTA"}
	for _, name := range vars {
		t.Run(name+"=0", func(t *testing.T) {
			t.Setenv("API_JWT_SECRET", "x")
			t.Setenv(name, "0")
			if _, err := initConfig(); err == nil {
				t.Errorf("%s=0 принят — ошибка конфигурации осталась бы незамеченной", name)
			}
		})
		t.Run(name+"=-2", func(t *testing.T) {
			t.Setenv("API_JWT_SECRET", "x")
			t.Setenv(name, "-2")
			if _, err := initConfig(); err == nil {
				t.Errorf("%s=-2 принят", name)
			}
		})
	}
}

func TestRatePerMinMustBePositive(t *testing.T) {
	t.Setenv("API_JWT_SECRET", "x")
	t.Setenv("AI_PARSE_RATE_PER_MIN", "0")
	if _, err := initConfig(); err == nil {
		t.Error("AI_PARSE_RATE_PER_MIN=0 принят — распознавание перестало бы работать у всех")
	}
}

// Приём событий по умолчанию ВКЛЮЧЁН, и это проверяется отдельно.
//
// При дефолте «выключено» забытая строка в compose даёт ровно ту картинку,
// которую пустая база рисует законно — «данных пока нет», — и отличить одно от
// другого нечем: ни ошибки, ни записи в логе. Такой отказ живёт месяцами.
func TestAnalyticsEnabledByDefault(t *testing.T) {
	t.Setenv("API_JWT_SECRET", "секрет-для-теста")

	cfg, err := initConfig()
	if err != nil {
		t.Fatalf("конфиг не собрался: %v", err)
	}
	if !cfg.AnalyticsEnabled {
		t.Error("незаданный ANALYTICS_ENABLED выключил приём событий — молчаливая потеря данных")
	}

	t.Setenv("ANALYTICS_ENABLED", "false")
	cfg, err = initConfig()
	if err != nil {
		t.Fatalf("конфиг не собрался: %v", err)
	}
	if cfg.AnalyticsEnabled {
		t.Error("ANALYTICS_ENABLED=false не выключил приём")
	}
}
