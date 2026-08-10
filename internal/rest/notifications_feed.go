package rest

import (
	"net/http"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// inviteCardDto закреплённая карточка приглашения в разделе уведомлений.
type inviteCardDto struct {
	RoomId      string           `json:"roomId"`
	RoomName    string           `json:"roomName"`
	InviterName string           `json:"inviterName"`
	Status      api.InviteStatus `json:"status"`
	CreatedAt   time.Time        `json:"createdAt"`
}

// notificationsDto ответ раздела «Уведомления».
type notificationsDto struct {
	// Invites — закреплённые карточки: pending (ждут решения) и непрочитанные
	// added («вас добавили»)
	Invites []inviteCardDto `json:"invites"`
	// Items — та же лента событий, что отдаёт /activity
	Items []activityItemDto `json:"items"`
	// UnreadCount — pending + непрочитанные added + события новее отметки
	UnreadCount int `json:"unreadCount"`
	// SeenThrough — время формирования ОТВЕТА. Клиент возвращает ровно это
	// значение в POST /me/notifications-seen: если поставить там серверное
	// «сейчас», событие, пришедшее между ответом и отметкой, оказалось бы
	// прочитанным, так и не показавшись человеку
	SeenThrough time.Time `json:"seenThrough"`
}

// handleNotifications GET /api/v1/notifications — раздел «Уведомления».
//
// Приглашения хранятся (их единицы, у них есть состояние и кнопки), а лента
// событий выводится на лету — как в /activity. Хранить строку на каждый расход
// каждому получателю было бы write amplification без пользы.
func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit, hErr := queryInt(r, "limit", defaultActivityLimit)
	if hErr != nil {
		hErr.write(w)
		return
	}
	offset, hErr := queryInt(r, "offset", 0)
	if hErr != nil {
		hErr.write(w)
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	items, hErr := s.activityItems(ctx, user.ID, limit, offset)
	if hErr != nil {
		hErr.write(w)
		return
	}

	seenThrough := s.now()
	out := notificationsDto{
		Invites:     []inviteCardDto{},
		Items:       items,
		SeenThrough: seenThrough,
	}

	// Непрочитанные события ленты. Считаем по CreateAt активных операций:
	// времени изменения у операции нет (api.Operation хранит только CreateAt),
	// поэтому правки и удаления расходов в счётчик не попадают. Это принятое
	// ограничение, а не недосмотр — «новое» здесь значит «новые расходы».
	for _, it := range items {
		if user.NotificationsSeenAt == nil || it.Operation.CreatedAt.After(*user.NotificationsSeenAt) {
			out.UnreadCount++
		}
	}

	if s.invites != nil {
		invites, err := s.invites.ListForUser(ctx, user.ID)
		if err != nil {
			log.Error().Err(err).Msg("cannot list invites")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось получить приглашения")
			return
		}
		for _, inv := range invites {
			// pending показываем всегда: они требуют решения и не гаснут от
			// того, что человек заглянул в раздел. added — только пока не
			// просмотрены.
			unreadAdded := inv.Status == api.InviteAdded &&
				(user.NotificationsSeenAt == nil || inv.CreatedAt.After(*user.NotificationsSeenAt))
			if inv.Status != api.InvitePending && !unreadAdded {
				continue
			}

			card := inviteCardDto{
				RoomId:    inv.RoomID.Hex(),
				Status:    inv.Status,
				CreatedAt: inv.CreatedAt,
			}
			// Комнату могли удалить — карточка не должна ронять весь ответ.
			if room, err := s.roomRepo.FindById(ctx, inv.RoomID.Hex()); err == nil && room != nil {
				card.RoomName = room.Name
			}
			if inviter, err := s.userRepo.FindById(ctx, inv.InviterID); err == nil && inviter != nil {
				card.InviterName = toUserDto(inviter).DisplayName
			}
			out.Invites = append(out.Invites, card)
			out.UnreadCount++
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// markSeenRequest тело POST /me/notifications-seen.
type markSeenRequest struct {
	SeenThrough time.Time `json:"seenThrough"`
}

// handleMarkNotificationsSeen POST /api/v1/me/notifications-seen.
//
// Время приходит от клиента — но не его локальное, а seenThrough из ответа
// GET /notifications, то есть серверное время формирования того ответа.
// Так помечается прочитанным ровно то, что человек мог увидеть.
func (s *Server) handleMarkNotificationsSeen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req markSeenRequest
	if dErr := decodeJSON(r, &req); dErr != nil {
		dErr.write(w)
		return
	}
	if req.SeenThrough.IsZero() {
		writeError(w, http.StatusBadRequest, "validation", "поле seenThrough обязательно")
		return
	}
	// Из будущего — почти наверняка кривые часы клиента или подделка: приняв
	// такое, мы погасили бы и всё, что придёт дальше.
	if req.SeenThrough.After(s.now().Add(time.Minute)) {
		writeError(w, http.StatusBadRequest, "validation", "seenThrough из будущего")
		return
	}

	if err := s.userRepo.SetNotificationsSeenAt(ctx, userIdFromCtx(ctx), req.SeenThrough.UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить отметку")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
