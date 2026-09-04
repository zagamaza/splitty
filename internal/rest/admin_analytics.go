package rest

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// analyticsFeedLimit — потолок строк ленты. Единственный блок, отдающий
	// записи, а не агрегат: без потолка окно в 90 дней вернуло бы всё разом.
	analyticsFeedLimit = 200
	// analyticsBudget — потолок на весь обработчик. У админского слушателя
	// своего таймаута нет, только ReadHeaderTimeout.
	analyticsBudget = 15 * time.Second
)

// analyticsWindows — закрытый список окон.
//
// Свободное число не принимается: панели незачем просить произвольное окно, а
// нам есть зачем не пускать в базу произвольный запрос. То же правило, по
// которому наружу торчат только именованные выборки.
var analyticsWindows = map[int]bool{7: true, 30: true, 90: true}

// handleAdminAnalytics отдаёт именованный блок продуктовых событий.
//
// Воронки и удержания здесь нет намеренно: на 267 пользователях и четырёх живых
// группах в неделю проценты по когортам — это единицы наблюдений, шум, который
// выглядит как сигнал. Границы шагов при этом собираются с первого дня, потому
// что задним числом их не восстановить.
func (s *Server) handleAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	if s.productEvents == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "журнал событий не подключён")
		return
	}

	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || !analyticsWindows[days] {
		writeError(w, http.StatusBadRequest, "validation", "окно должно быть 7, 30 или 90 дней")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), analyticsBudget)
	defer cancel()

	block := r.PathValue("block")
	var (
		body any
		qErr error
	)
	switch block {
	case "feed":
		body, qErr = s.productEvents.Feed(ctx, days, analyticsFeedLimit)
	case "daily":
		// Фильтр по имени, а не отдельный блок «ошибки»: expense_parse_failed,
		// purchase_failed и room_join_failed — те же суточные счётчики.
		body, qErr = s.productEvents.Daily(ctx, days, r.URL.Query().Get("name"))
	case "platforms":
		body, qErr = s.productEvents.Platforms(ctx, days)
	default:
		writeError(w, http.StatusNotFound, "not_found", "нет такого блока")
		return
	}

	if qErr != nil {
		log.Error().Err(qErr).Str("block", block).Int("days", days).Msg("analytics query failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось посчитать блок")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "block": block, "rows": body})
}
