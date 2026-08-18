// Package metrics отдаёт сводные числа Splitty в формате Prometheus.
//
// Отдельный слушатель, а не маршрут основного API: метрики не должны торчать
// наружу вместе с публичным доменом. Наружу порт не публикуется — до него
// дотягивается только Alloy по сети docker.
//
// Формат пишем руками: это десяток строк для гейджей без меток, а тянуть
// клиентскую библиотеку со своим деревом зависимостей в проект, где её нет,
// ради этого незачем.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/almaznur91/splitty/internal/repository"
	"github.com/rs/zerolog/log"
)

// Collector — источник снимка (реализация — repository.MongoStatsRepository).
type Collector interface {
	Collect(ctx context.Context, now time.Time) (repository.Stats, error)
}

type Server struct {
	collector Collector
	interval  time.Duration

	mu      sync.RWMutex
	stats   repository.Stats
	updated time.Time
}

func NewServer(collector Collector, interval time.Duration) *Server {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Server{collector: collector, interval: interval}
}

// Refresh пересчитывает снимок по расписанию. Блокирующий вызов — звать из горутины.
func (s *Server) Refresh(ctx context.Context) {
	s.refreshOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshOnce(ctx)
		}
	}
}

func (s *Server) refreshOnce(ctx context.Context) {
	stats, err := s.collector.Collect(ctx, time.Now().UTC())
	if err != nil {
		log.Warn().Err(err).Msg("не удалось посчитать метрики")
		// Снимок не затираем: устаревшее число честнее, чем ноль, который
		// на графике неотличим от «всё пропало».
		return
	}
	s.mu.Lock()
	s.stats = stats
	s.updated = time.Now().UTC()
	s.mu.Unlock()
}

// ServeHTTP отдаёт текстовую экспозицию Prometheus.
func (s *Server) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	stats := s.stats
	updated := s.updated
	s.mu.RUnlock()

	var b strings.Builder
	gauge := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
	}

	gauge("splitty_rooms_total", "Всего групп", stats.Rooms)
	gauge("splitty_rooms_active_7d", "Групп с движением за неделю", stats.RoomsActive7d)
	gauge("splitty_users_total", "Всего пользователей", stats.Users)
	gauge("splitty_push_outbox_depth", "Пушей в очереди доставки", stats.PushOutbox)
	gauge("splitty_debt_reminders_total", "Людей с историей напоминаний о долге", stats.DebtReminders)
	// Возраст снимка виден снаружи: без него нельзя отличить «ничего не
	// меняется» от «пересчёт умер полчаса назад».
	gauge("splitty_stats_age_seconds", "Сколько секунд назад пересчитаны числа",
		int64(time.Since(updated).Seconds()))

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// Listen поднимает слушатель метрик. Блокирующий вызов — звать из горутины.
func (s *Server) Listen(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", s)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Info().Msgf("метрики на %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("слушатель метрик упал")
	}
}
