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
