package rest

import (
	"context"
	"net/http"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// addMemberRequest тело POST /rooms/{roomId}/members.
type addMemberRequest struct {
	UserId int `json:"userId"`
}

// addMemberResponse отдаётся и при добавлении, и при отправке приглашения:
// клиенту нужно знать, стал человек участником сразу или ждёт решения.
type addMemberResponse struct {
	Status api.InviteStatus `json:"status"`
}

// handleAddMember POST /api/v1/rooms/{roomId}/members — позвать человека в комнату.
//
// Порядок проверок ФИКСИРОВАН, от него зависит корректность повторных
// приглашений (см. комментарии по шагам). Кратко:
//
//	1) зовущий — участник;
//	2) приглашаемый существует и не удалён;
//	3) уже участник → 200 + примирение записи;
//	4) уходил раньше → 202, приглашение ждёт согласия;
//	5) иначе → проверка связи и добавление.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.invites == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "приглашения недоступны")
		return
	}

	roomId := r.PathValue("roomId")
	inviterId := userIdFromCtx(ctx)

	// (1) Зовущий обязан быть участником: иначе посторонний открывал бы людям
	// доступ к чужой комнате.
	room, hErr := s.roomForMember(ctx, roomId, inviterId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	var req addMemberRequest
	if dErr := decodeJSON(r, &req); dErr != nil {
		dErr.write(w)
		return
	}
	if req.UserId == 0 {
		writeError(w, http.StatusBadRequest, "validation", "поле userId обязательно")
		return
	}
	if req.UserId == inviterId {
		writeError(w, http.StatusBadRequest, "validation", "нельзя пригласить самого себя")
		return
	}

	// (2) Приглашаемый обязан существовать и не быть удалённым. Удалённые
	// остаются во встроенных снимках комнат (анонимизированными), поэтому
	// попадают и в /friends — без этой проверки их «приглашали» бы в никуда.
	invitee, err := s.userRepo.FindById(ctx, req.UserId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
			return
		}
		log.Error().Err(err).Msg("cannot find invitee")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить пользователя")
		return
	}
	if invitee.IsDeleted() {
		writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
		return
	}

	roomHex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "комната не найдена")
		return
	}

	existing, err := s.invites.Find(ctx, roomHex, invitee.ID)
	if err != nil && err != mongo.ErrNoDocuments {
		log.Error().Err(err).Msg("cannot read room invite")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить приглашение")
		return
	}

	// (3) Уже участник — идемпотентный 200, но с ПРИМИРЕНИЕМ записи. Человек мог
	// войти сам по ссылке /join/{roomId}, которая про приглашения ничего не
	// знает: без примирения он остался бы участником со статусом pending
	// навсегда, и следующее приглашение повело бы себя как для вышедшего.
	if isRoomMember(room, invitee.ID) {
		if existing == nil || existing.Status != api.InviteAdded {
			if err = s.invites.Upsert(ctx, roomHex, invitee.ID, inviterId, api.InviteAdded, s.now()); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
				return
			}
		}
		writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
		return
	}

	// (4) Уходил раньше или отказывался — тихо вернуть его нельзя, иначе
	// получался бы цикл «убрал себя из расхода → вышел → добавили снова».
	// Создаём приглашение, которое ждёт его решения.
	if existing != nil && (existing.Status == api.InviteLeft || existing.Status == api.InviteDeclined) {
		if err = s.invites.Upsert(ctx, roomHex, invitee.ID, inviterId, api.InvitePending, s.now()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
			return
		}
		s.notifyInvited(ctx, *room, *invitee, inviterId, true)
		writeJSON(w, http.StatusAccepted, addMemberResponse{Status: api.InvitePending})
		return
	}

	// (5) Связь: друг ИЛИ прошлое отношение по этой комнате.
	//
	// /friends строится из ТЕКУЩИХ участников неархивных комнат, поэтому вышедший
	// перестаёт быть другом, если общая комната была единственной. Проверка
	// «только друзья» в чистом виде сделала бы повторное приглашение
	// недостижимым в самом типовом случае — отсюда вторая ветка.
	related := existing != nil
	if !related {
		related, hErr = s.shareRoomWith(ctx, inviterId, invitee.ID)
		if hErr != nil {
			hErr.write(w)
			return
		}
	}
	if !related {
		writeError(w, http.StatusForbidden, "not_a_friend",
			"Пригласить можно того, с кем у вас уже была общая группа. Остальным отправьте ссылку")
		return
	}

	// Порядок записей: сначала членство, потом запись отношения. Если второй шаг
	// упадёт, человек окажется в группе без карточки — повторный вызов эндпоинта
	// пойдёт по шагу (3) и создаст недостающую запись.
	if err = s.roomRepo.JoinToRoom(ctx, *invitee, roomId); err != nil {
		log.Error().Err(err).Msg("cannot add member to room")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось добавить участника")
		return
	}
	if err = s.invites.Upsert(ctx, roomHex, invitee.ID, inviterId, api.InviteAdded, s.now()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
		return
	}

	s.notifyInvited(ctx, *room, *invitee, inviterId, false)
	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
}

// notifyInvited шлёт уведомление приглашённому в фоне (как остальные
// уведомления REST — сбой доставки не должен ронять запрос).
func (s *Server) notifyInvited(ctx context.Context, room api.Room, invitee api.User, inviterId int, isReturn bool) {
	inviter, err := s.userRepo.FindById(ctx, inviterId)
	if err != nil {
		log.Warn().Err(err).Int("user", inviterId).Msg("cannot read inviter for notification")
		return
	}
	s.notifyAsync(ctx, func(nctx context.Context, n Notifier) {
		n.NotifyInvited(nctx, room, invitee, *inviter, isReturn)
	})
}

// shareRoomWith есть ли у двоих общая неархивная комната — то же отношение,
// на котором строится /friends.
func (s *Server) shareRoomWith(ctx context.Context, userId, otherId int) (bool, *httpError) {
	rooms, err := s.roomRepo.FindRoomsByUserId(ctx, userId)
	if err != nil {
		log.Error().Err(err).Msg("cannot find rooms")
		return false, &httpError{http.StatusInternalServerError, "internal", "не удалось получить комнаты"}
	}
	if rooms == nil {
		return false, nil
	}
	for i := range *rooms {
		if isRoomMember(&(*rooms)[i], otherId) {
			return true, nil
		}
	}
	return false, nil
}
