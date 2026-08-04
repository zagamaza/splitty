package rest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const tgTestToken = "1687852330:AAEnFzgF-testtoken"

func tgAuthServer(t *testing.T, publicBase string) *Server {
	t.Helper()
	return newTestServer(Config{PublicBaseUrl: publicBase, TgToken: tgTestToken},
		newFakeUserRepo(), newFakeRoomRepo())
}

func getPage(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, s, http.MethodGet, path, "", "")
}

// Без домена весь диплинк-блок выключен: Telegram сюда всё равно не приведёт,
// а открытый /tg-auth редиректил бы на виджет с пустым origin
func TestTelegramWebAuthDisabledWithoutDomain(t *testing.T) {
	s := tgAuthServer(t, "")
	for _, p := range []string{"/tg-auth", "/tg-callback"} {
		if rec := getPage(t, s, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", p, rec.Code)
		}
	}
}

// /tg-auth уводит на виджет с origin и return_to НАШЕГО домена: Telegram
// сверяет origin с доменом бота, и собери мы ссылку на клиенте — origin был бы
// его baseURL (http-IP), то есть виджет бы не открылся
func TestTelegramAuthStartRedirect(t *testing.T) {
	s := tgAuthServer(t, "https://splito.zagirnur.dev")
	rec := getPage(t, s, "/tg-auth")

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("разобрать Location: %v", err)
	}
	if loc.Host != "oauth.telegram.org" {
		t.Errorf("host = %q, want oauth.telegram.org", loc.Host)
	}
	q := loc.Query()
	if got := q.Get("bot_id"); got != "1687852330" {
		t.Errorf("bot_id = %q — должен быть id из TG_TOKEN", got)
	}
	if got := q.Get("origin"); got != "https://splito.zagirnur.dev" {
		t.Errorf("origin = %q", got)
	}
	if got := q.Get("return_to"); got != "https://splito.zagirnur.dev/tg-callback?native=1" {
		t.Errorf("return_to = %q", got)
	}
	// секретная часть токена наружу не уходит ни при каких обстоятельствах
	if strings.Contains(rec.Header().Get("Location"), "AAEnFzgF") {
		t.Fatal("секрет TG_TOKEN попал в редирект")
	}
}

// Страница возврата уводит в приложение по кастомной схеме и не индексируется
func TestTelegramCallbackPage(t *testing.T) {
	s := tgAuthServer(t, "https://splito.zagirnur.dev")
	rec := getPage(t, s, "/tg-callback?native=1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"splitty"`) {
		t.Error("в странице нет схемы splitty — возврат в приложение не сработает")
	}
	if !strings.Contains(body, "tgAuthResult") {
		t.Error("страница не читает tgAuthResult")
	}
	// fragment на сервер не приходит, поэтому забирать значение обязан скрипт
	if !strings.Contains(body, "location.hash") {
		t.Error("страница не читает fragment — Telegram отдаёт результат и туда")
	}
	if rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Error("страница входа не должна индексироваться")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("одноразовый payload нельзя кешировать")
	}
}

func TestTelegramBotID(t *testing.T) {
	tests := []struct{ token, want string }{
		{"1687852330:AAEnFzgF", "1687852330"},
		{"", ""},
		{"без-двоеточия", ""},
		{":только-секрет", ""},
	}
	for _, tt := range tests {
		if got := telegramBotID(tt.token); got != tt.want {
			t.Errorf("telegramBotID(%q) = %q, want %q", tt.token, got, tt.want)
		}
	}
}
