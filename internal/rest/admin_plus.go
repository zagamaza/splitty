package rest

// Выдача Plus из панели: первые ПИШУЩИЕ маршруты админского API.
//
// Только POST и DELETE, никогда GET. Кука сессии панели живёт с SameSite: Lax —
// на кросс-сайтовый POST она не уходит, а на top-level GET-навигацию уходит.
// «Удобная ссылка» вида GET …/plus открывала бы выдачу Plus по чужой ссылке из
// мессенджера.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

const (
	// maxGrantHorizon — потолок срока. Бессрочный подарок никто никогда не
	// пересматривает, а опечатка «2226-01-01» — тот же бессрочный, только
	// незаметный.
	maxGrantHorizon = 2 * 365 * 24 * time.Hour
	// maxGrantReasonRunes — причина в РУНАХ, не байтах: кириллица занимает по
	// два, и лимит в байтах резал бы русский текст вдвое раньше.
	maxGrantReasonRunes = 200
)

type grantRequest struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Reason    string    `json:"reason"`
}

type revokeRequest struct {
	Reason string `json:"reason"`
}

// adminPlusDto — строка списка «кому выдан Plus». С именем, а не голым номером:
// панели иначе нечего показать.
type adminPlusDto struct {
	UserID      int       `json:"userId"`
	DisplayName string    `json:"displayName,omitempty"`
	Username    string    `json:"username,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Reason      string    `json:"reason,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// plusSourcePurchase и соседи — откуда у человека Plus. Панели нужно отличать
// «платит» от «подарено» и от списка в окружении: отзывать можно только грант.
const (
	plusSourcePurchase = "purchase"
	plusSourceGrant    = "grant"
	plusSourceComp     = "comp"
)

// adminUserPlusDto — блок Plus в карточке человека.
type adminUserPlusDto struct {
	Tier      api.Tier   `json:"tier"`
	Source    string     `json:"source,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

// handleAdminGrantPlus POST /admin/users/{userId}/plus — выдать или продлить.
func (s *Server) handleAdminGrantPlus(w http.ResponseWriter, r *http.Request) {
	if s.plusGrants == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "гранты не подключены")
		return
	}

	user := s.adminLivingUser(w, r)
	if user == nil {
		return
	}

	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "тело запроса не разобрано")
		return
	}

	now := time.Now().UTC()
	if req.ExpiresAt.IsZero() || !req.ExpiresAt.After(now) {
		writeError(w, http.StatusBadRequest, "validation", "срок должен быть в будущем")
		return
	}
	if req.ExpiresAt.After(now.Add(maxGrantHorizon)) {
		writeError(w, http.StatusBadRequest, "validation", "срок не может быть дальше двух лет")
		return
	}
	if utf8.RuneCountInString(req.Reason) > maxGrantReasonRunes {
		writeError(w, http.StatusBadRequest, "validation", "причина длиннее двухсот символов")
		return
	}

	if err := s.plusGrants.Grant(r.Context(), user.ID, req.ExpiresAt, req.Reason, now); err != nil {
		log.Error().Err(err).Int("userId", user.ID).Msg("админский api: выдача plus")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось выдать plus")
		return
	}

	// Иначе Plus появится не сразу, и подарок будет выглядеть поломкой:
	// человек уже знает, что ему выдали, а приложение ещё показывает лимит.
	s.invalidateTier(user.ID)

	// Отдельной коллекции аудита нет: строка гранта сама несёт кто, когда и
	// почему. Лог — второй след, тот же, что пишет adminAuth.
	log.Info().
		Str("action", "plus_grant").
		Int("userId", user.ID).
		Time("expiresAt", req.ExpiresAt).
		Str("reason", req.Reason).
		Str("ip", s.clientIP(r)).
		Msg("админский api: plus выдан")

	s.writeAdminPlusState(w, r, user.ID)
}

// handleAdminRevokePlus DELETE /admin/users/{userId}/plus — отозвать.
func (s *Server) handleAdminRevokePlus(w http.ResponseWriter, r *http.Request) {
	if s.plusGrants == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "гранты не подключены")
		return
	}

	user := s.adminLivingUser(w, r)
	if user == nil {
		return
	}

	// Тело необязательное: отзыв без объяснения — рабочий случай.
	var req revokeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if utf8.RuneCountInString(req.Reason) > maxGrantReasonRunes {
		writeError(w, http.StatusBadRequest, "validation", "причина длиннее двухсот символов")
		return
	}

	if err := s.plusGrants.Revoke(r.Context(), user.ID, req.Reason, time.Now().UTC()); err != nil {
		log.Error().Err(err).Int("userId", user.ID).Msg("админский api: отзыв plus")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось отозвать plus")
		return
	}

	s.invalidateTier(user.ID)

	log.Info().
		Str("action", "plus_revoke").
		Int("userId", user.ID).
		Str("reason", req.Reason).
		Str("ip", s.clientIP(r)).
		Msg("админский api: plus отозван")

	s.writeAdminPlusState(w, r, user.ID)
}

// handleAdminPlusList GET /admin/plus — кому Plus выдан прямо сейчас.
func (s *Server) handleAdminPlusList(w http.ResponseWriter, r *http.Request) {
	if s.plusGrants == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "гранты не подключены")
		return
	}

	ctx := r.Context()
	grants, err := s.plusGrants.ListLive(ctx, time.Now().UTC())
	if err != nil {
		log.Error().Err(err).Msg("админский api: список grant'ов plus")
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось прочитать список")
		return
	}

	items := make([]adminPlusDto, 0, len(grants))
	ids := make([]int, 0, len(grants))
	for i := range grants {
		items = append(items, adminPlusDto{
			UserID:    grants[i].UserId,
			ExpiresAt: grants[i].ExpiresAt,
			Reason:    grants[i].Reason,
			CreatedAt: grants[i].CreatedAt,
		})
		ids = append(ids, grants[i].UserId)
	}

	// Имена одним запросом: список на десятки строк, но ходить за каждым по
	// отдельности незачем. Не прочитались — отдаём номера, а не 500: список
	// без имён всё равно полезнее пустого экрана.
	if len(ids) > 0 {
		users, err := s.userRepo.FindByIds(ctx, ids)
		if err != nil {
			log.Error().Err(err).Msg("админский api: имена для списка plus")
		} else {
			byId := make(map[int]api.User, len(users))
			for i := range users {
				byId[users[i].ID] = users[i]
			}
			for i := range items {
				if u, ok := byId[items[i].UserID]; ok {
					items[i].DisplayName = u.DisplayName
					items[i].Username = u.Username
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, items)
}

// adminLivingUser — общий guard пишущих маршрутов: номер разобран, человек есть
// и не удалён.
//
// Ссылаться на handleAdminUser нельзя: он удалённого ВОЗВРАЩАЕТ (карточке это
// нужно), а FindById tombstone не фильтрует. Без явной проверки грант на
// удалённый аккаунт прошёл бы с 200 OK — молчаливый провал.
func (s *Server) adminLivingUser(w http.ResponseWriter, r *http.Request) *api.User {
	userId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return nil
	}
	user, err := s.userRepo.FindById(r.Context(), userId)
	if err != nil || user == nil || user.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "not_found", "нет такого человека")
		return nil
	}
	return user
}

// writeAdminPlusState отдаёт состояние Plus после выдачи или отзыва — панели не
// приходится делать второй запрос, чтобы показать результат.
func (s *Server) writeAdminPlusState(w http.ResponseWriter, r *http.Request, userId int) {
	writeJSON(w, http.StatusOK, s.adminPlusState(r, userId))
}

// adminPlusState — тариф человека и ОТКУДА он: платит, подарено или comp.
func (s *Server) adminPlusState(r *http.Request, userId int) adminUserPlusDto {
	ctx := r.Context()
	now := time.Now().UTC()

	state := adminUserPlusDto{Tier: api.TierFree}
	if s.entitlements != nil {
		state.Tier = s.entitlements.TierOrFree(ctx, userId)
	}
	if state.Tier != api.TierPlus {
		return state
	}

	// Порядок тот же, что у резолва тарифа: список в окружении, покупка, грант.
	// Разойдись он — панель показала бы источник, которого резолв не выбирал.
	if s.entitlements != nil && s.entitlements.IsComp(userId) {
		state.Source = plusSourceComp
		return state
	}

	if s.subscriptions != nil {
		if subs, err := s.subscriptions.ActiveByUser(ctx, userId); err == nil {
			for i := range subs {
				if !subs[i].Active(now, s.deliverySlack) {
					continue
				}
				if state.ExpiresAt == nil || subs[i].ExpiresAt.After(*state.ExpiresAt) {
					expires := subs[i].ExpiresAt
					state.ExpiresAt = &expires
					state.Source = plusSourcePurchase
				}
			}
		}
	}

	if s.plusGrants != nil {
		if g, err := s.plusGrants.LiveByUser(ctx, userId, now); err == nil && g.Live(now) {
			if state.ExpiresAt == nil || g.ExpiresAt.After(*state.ExpiresAt) {
				expires := g.ExpiresAt
				state.ExpiresAt = &expires
				state.Source = plusSourceGrant
				state.Reason = g.Reason
			}
		}
	}

	return state
}

// invalidateTier сбрасывает кеш тарифа, если резолв подключён.
func (s *Server) invalidateTier(userId int) {
	if s.entitlements != nil {
		s.entitlements.Invalidate(userId)
	}
}
