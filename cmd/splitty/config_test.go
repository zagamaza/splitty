package main

import "testing"

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
