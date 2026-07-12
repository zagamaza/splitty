package rest

import (
	"net/http"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// channelPrefsDto каналы доставки одной категории — ЭФФЕКТИВНЫЕ значения
// (с учётом легаси-фолбэков), чтобы клиенту не нужно было их дублировать.
type channelPrefsDto struct {
	Telegram bool `json:"telegram"`
	Push     bool `json:"push"`
}

// notifySettingsDto настройки уведомлений по категориям событий.
type notifySettingsDto struct {
	// Operations — добавление/изменение расходов в тусах пользователя.
	Operations channelPrefsDto `json:"operations"`
	// Debts — возвраты долгов (и будущие напоминания).
	Debts channelPrefsDto `json:"debts"`
}

// patchNotifyRequest частичное обновление: незаданные поля не меняются.
type patchNotifyRequest struct {
	Operations *struct {
		Telegram *bool `json:"telegram"`
		Push     *bool `json:"push"`
	} `json:"operations"`
	Debts *struct {
		Telegram *bool `json:"telegram"`
		Push     *bool `json:"push"`
	} `json:"debts"`
}

func toNotifyDto(u *api.User) notifySettingsDto {
	return notifySettingsDto{
		Operations: channelPrefsDto{
			Telegram: u.AllowsTelegram(api.NotifyOperations),
			Push:     u.WantsPush(api.NotifyOperations),
		},
		Debts: channelPrefsDto{
			Telegram: u.AllowsTelegram(api.NotifyDebts),
			Push:     u.WantsPush(api.NotifyDebts),
		},
	}
}

// handleGetNotifications GET /api/v1/me/notifications — эффективные настройки
// уведомлений (категория × канал) с учётом легаси-правил бота.
func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request) {
	user, hErr := s.currentUser(r.Context())
	if hErr != nil {
		hErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, toNotifyDto(user))
}

// handlePatchNotifications PATCH /api/v1/me/notifications — частичное
// обновление настроек; ответ — новые эффективные значения.
func (s *Server) handlePatchNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req patchNotifyRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	// Стартуем с текущих ЭФФЕКТИВНЫХ значений: первое же изменение из
	// приложения фиксирует всю матрицу явно, дальше легаси-фолбэки не нужны.
	settings := api.NotifySettings{
		Operations: api.ChannelPrefs{
			Telegram: boolPtr(user.AllowsTelegram(api.NotifyOperations)),
			Push:     boolPtr(user.WantsPush(api.NotifyOperations)),
		},
		Debts: api.ChannelPrefs{
			Telegram: boolPtr(user.AllowsTelegram(api.NotifyDebts)),
			Push:     boolPtr(user.WantsPush(api.NotifyDebts)),
		},
	}
	if req.Operations != nil {
		if req.Operations.Telegram != nil {
			settings.Operations.Telegram = req.Operations.Telegram
		}
		if req.Operations.Push != nil {
			settings.Operations.Push = req.Operations.Push
		}
	}
	if req.Debts != nil {
		if req.Debts.Telegram != nil {
			settings.Debts.Telegram = req.Debts.Telegram
		}
		if req.Debts.Push != nil {
			settings.Debts.Push = req.Debts.Push
		}
	}

	if err := s.userRepo.SetNotifySettings(ctx, user.ID, settings); err != nil {
		log.Error().Err(err).Msg("cannot update notify settings")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить настройки уведомлений")
		return
	}
	user.Notify = &settings
	writeJSON(w, http.StatusOK, toNotifyDto(user))
}

func boolPtr(b bool) *bool { return &b }
