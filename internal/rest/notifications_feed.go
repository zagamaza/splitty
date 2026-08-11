package rest

import (
	"net/http"
	"slices"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// maxUnreadCount последнее ТОЧНОЕ значение счётчика непрочитанного: число сверх
// сотни человеку ничего не даёт, а обход всей ленты ради него — лишняя работа.
// Переполнение отдаётся как maxUnreadCount+1 — «больше 99»; клиент рисует «99+».
// Клампить ровно 99 нельзя: клиент не отличил бы потолок от честной сотни минус
// один и рисовал бы точное «99», которого не было.
const maxUnreadCount = 99

// unreadOverflow значение-маркер «непрочитанного больше, чем maxUnreadCount».
const unreadOverflow = maxUnreadCount + 1

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
	// UnreadCount — pending + непрочитанные added + события новее отметки, о
	// которых человеку уходило уведомление (см. notifiesUser);
	// maxUnreadCount+1 означает «больше 99»
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

	// Время берём ДО первого чтения: возьми мы его после, событие, созданное
	// пока читалась лента, не попало бы в ответ, но оказалось бы старше
	// seenThrough — и клиентская отметка погасила бы его непоказанным.
	seenThrough := s.now().UTC()

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	all, hErr := s.allActivityItems(ctx, user.ID)
	if hErr != nil {
		hErr.write(w)
		return
	}

	out := notificationsDto{
		Invites:     []inviteCardDto{},
		Items:       activityPage(all, limit, offset),
		SeenThrough: seenThrough,
	}

	// Непрочитанные события ленты — по ВСЕЙ ленте, а не по отданной странице:
	// бейдж читают запросом с limit=1, и счёт по странице упирал бы его в
	// единицу. Считаем по CreateAt активных операций: времени изменения у
	// операции нет (api.Operation хранит только CreateAt), поэтому правки и
	// удаления расходов в счётчик не попадают. Это принятое ограничение, а не
	// недосмотр — «новое» здесь значит «новые расходы».
	for i := range all {
		item := &all[i]
		if !notifiesUser(item, user.ID) {
			continue
		}
		if user.NotificationsSeenAt != nil && !item.Operation.CreatedAt.After(*user.NotificationsSeenAt) {
			continue
		}
		out.UnreadCount++
		// Потолок: у старого пользователя без отметки непрочитано вообще всё, и
		// бейдж «347» не сообщает ничего сверх «много». Клиент рисует «99+».
		if out.UnreadCount > maxUnreadCount {
			break
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
			// Комнату или пригласившего могли удалить — карточка не должна ронять
			// весь ответ. Пустое имя клиент подменяет своим плейсхолдером, но сбой
			// чтения обязан быть виден в логах, а не растворяться в «».
			if room, err := s.roomRepo.FindById(ctx, inv.RoomID.Hex()); err != nil {
				log.Warn().Err(err).Str("room", inv.RoomID.Hex()).Msg("cannot read room for invite card")
			} else if room != nil {
				card.RoomName = room.Name
			}
			if inviter, err := s.userRepo.FindById(ctx, inv.InviterID); err != nil {
				log.Warn().Err(err).Int("user", inv.InviterID).Msg("cannot read inviter for invite card")
			} else if inviter != nil {
				card.InviterName = toUserDto(inviter).DisplayName
			}
			out.Invites = append(out.Invites, card)
			out.UnreadCount++
		}
	}
	if out.UnreadCount > maxUnreadCount {
		out.UnreadCount = unreadOverflow
	}

	writeJSON(w, http.StatusOK, out)
}

// notifiesUser считается ли событие ленты непрочитанным ЛИЧНО для человека.
//
// Раздел — входящие, поэтому счётчик обязан совпадать с тем, о чём человеку
// сообщали. Точный ответ хранится в самой операции: notification_sent — список
// тех, кому по ней ушло уведомление, его пишут оба пути (notifier REST и экраны
// бота). Никаких догадок про автора: назначенный плательщик там есть, автор —
// нет, получатель с нулевой долей — нет.
//
// Пустой список — не «никому», а «неизвестно»: у легаси-операций эпохи
// master-2021 поля нет вовсе, а у свежих оно появляется чуть позже самой
// операции (уведомления уходят фоном). Для таких работает прежнее правило по
// долям — свой расход бейдж не поднимает, чужой без твоей доли тоже.
func notifiesUser(item *activityItemDto, userId int) bool {
	if len(item.notified) > 0 {
		return slices.Contains(item.notified, userId)
	}
	op := &item.Operation
	if op.Donor.ID == userId {
		return false
	}
	for _, r := range op.Recipients {
		if r.User.ID == userId && r.Sum != 0 {
			return true
		}
	}
	return false
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
	w.WriteHeader(http.StatusNoContent)
}
