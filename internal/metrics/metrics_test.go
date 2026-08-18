package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/repository"
)

type fakeCollector struct {
	stats repository.Stats
	err   error
	calls int
}

func (f *fakeCollector) Collect(context.Context, time.Time) (repository.Stats, error) {
	f.calls++
	return f.stats, f.err
}

func TestExposition(t *testing.T) {
	c := &fakeCollector{stats: repository.Stats{
		Rooms: 128, RoomsActive7d: 12, Users: 340, PushOutbox: 3, DebtReminders: 7,
	}}
	s := NewServer(c, time.Minute)
	s.refreshOnce(context.Background())

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	body := rec.Body.String()
	for _, want := range []string{
		"splitty_rooms_total 128",
		"splitty_rooms_active_7d 12",
		"splitty_users_total 340",
		"splitty_push_outbox_depth 3",
		"splitty_debt_reminders_total 7",
		"# TYPE splitty_rooms_total gauge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в экспозиции нет %q:\n%s", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// Сбой пересчёта не должен обнулять снимок: ноль на графике неотличим от
// «всё пропало», а устаревшее число честнее.
func TestFailedRefreshKeepsPreviousSnapshot(t *testing.T) {
	c := &fakeCollector{stats: repository.Stats{Rooms: 100}}
	s := NewServer(c, time.Minute)
	s.refreshOnce(context.Background())

	c.err = errors.New("база недоступна")
	c.stats = repository.Stats{}
	s.refreshOnce(context.Background())

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "splitty_rooms_total 100") {
		t.Errorf("снимок затёрт после сбоя:\n%s", rec.Body.String())
	}
}

// Возраст снимка обязан быть виден: иначе не отличить «ничего не меняется» от
// «пересчёт умер полчаса назад».
func TestSnapshotAgeExposed(t *testing.T) {
	s := NewServer(&fakeCollector{}, time.Minute)
	s.refreshOnce(context.Background())

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "splitty_stats_age_seconds") {
		t.Error("возраст снимка не отдаётся")
	}
}
