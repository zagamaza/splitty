package rest

import (
	"net/http"
	"strings"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// deviceRequest — регистрация/снятие FCM-токена устройства.
type deviceRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"` // "android" | "ios"
}

// handleRegisterDevice POST /api/v1/me/devices — привязать FCM-токен текущего
// устройства к пользователю (для native-пушей). Идемпотентно: повторный вызов с
// тем же токеном обновляет платформу, дублей не создаёт.
func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req deviceRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле token обязательно")
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	if err := s.userRepo.AddPushToken(ctx, user.ID, api.PushToken{Token: req.Token, Platform: req.Platform}); err != nil {
		log.Error().Err(err).Int("user", user.ID).Msg("cannot add push token")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось зарегистрировать устройство")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUnregisterDevice DELETE /api/v1/me/devices — отвязать токен (logout).
// Отсутствие токена — не ошибка (idempotent).
func (s *Server) handleUnregisterDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req deviceRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "validation", "поле token обязательно")
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	if err := s.userRepo.RemovePushToken(ctx, user.ID, req.Token); err != nil {
		log.Error().Err(err).Int("user", user.ID).Msg("cannot remove push token")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось отвязать устройство")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
