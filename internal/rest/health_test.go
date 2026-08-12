package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Проверка здоровья.
//
// Раньше /health отвечал «ok» всегда, даже с упавшей базой: снаружи сервис
// выглядел рабочим, и ни перезапуска, ни сигнала не происходило — про поломку
// узнавали от людей, у которых «ничего не открывается».

func TestHealthOkWhenDatabaseAnswers(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	s.SetDBPing(func(context.Context) error { return nil })

	rec := doRequest(t, s, http.MethodGet, "/health", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHealthUnavailableWhenDatabaseDown(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	s.SetDBPing(func(context.Context) error { return errors.New("mongo недоступна") })

	rec := doRequest(t, s, http.MethodGet, "/health", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — сервис с упавшей базой отчитался живым; body: %s",
			rec.Code, rec.Body.String())
	}
}

// Без заданной проверки (тесты, запуск без mongo) поведение прежнее: 200.
func TestHealthOkWithoutPing(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))

	rec := doRequest(t, s, http.MethodGet, "/health", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// Молчащий цикл обновлений виден в ответе, но REST при этом жив — код 200.
func TestHealthReportsSilentBot(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	s.SetDBPing(func(context.Context) error { return nil })
	s.SetBotHeartbeat(func() time.Time { return time.Now().Add(-time.Hour) })

	rec := doRequest(t, s, http.MethodGet, "/health", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — REST работает и без бота", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не разобрался: %v, тело: %s", err, rec.Body.String())
	}
	if body["bot"] != "silent" {
		t.Fatalf("бот молчит час, а в ответе %v", body["bot"])
	}
}

func TestHealthReportsLiveBot(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	s.SetBotHeartbeat(func() time.Time { return time.Now() })

	rec := doRequest(t, s, http.MethodGet, "/health", "", "")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	if body["bot"] != "ok" {
		t.Fatalf("живой бот показан как %v", body["bot"])
	}
}
