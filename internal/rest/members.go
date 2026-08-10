package rest

import (
	"context"
	"net/http"
	"strconv"

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
//  1. зовущий — участник;
//  2. приглашаемый существует и не удалён;
//  3. уже участник → 200 + примирение записи;
//  4. уходил раньше → 202, приглашение ждёт согласия;
//  5. иначе → проверка связи и добавление.
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

// handleAcceptInvite POST /api/v1/invites/{roomId}/accept — принять приглашение.
//
// Порядок: СНАЧАЛА перевод статуса, потом добавление в комнату. Compare-and-set
// гарантирует, что из pending выйдет ровно один запрос: проигравший гонку с
// «отклонить» (или со вторым тапом) получит 409 и не станет добавлять человека
// второй раз.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomId, roomHex, invite, hErr := s.pendingInvite(ctx, r)
	if hErr != nil {
		hErr.write(w)
		return
	}

	room, err := s.roomRepo.FindById(ctx, roomId)
	if err != nil || room == nil {
		// Комнату успели удалить — принимать нечего.
		writeError(w, http.StatusNotFound, "not_found", "группа не найдена")
		return
	}

	ok, err := s.invites.SetStatusIfCurrent(ctx, roomHex, invite.InviteeID, api.InvitePending, api.InviteAdded, s.now())
	if err != nil {
		log.Error().Err(err).Msg("cannot accept invite")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось принять приглашение")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "not_pending", "приглашение уже обработано")
		return
	}

	// JoinToRoom идемпотентен, поэтому ретрай безопасен. Если он всё же упадёт,
	// останется запись added без членства — её чинит повторное приглашение:
	// оно пойдёт по шагу (5) handleAddMember, потому что человек не участник.
	if err = s.roomRepo.JoinToRoom(ctx, *invite.user, roomId); err != nil {
		log.Error().Err(err).Msg("cannot join room on accept")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось войти в группу")
		return
	}

	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
}

// handleDeclineInvite POST /api/v1/invites/{roomId}/decline — отказаться.
func (s *Server) handleDeclineInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, roomHex, invite, hErr := s.pendingInvite(ctx, r)
	if hErr != nil {
		hErr.write(w)
		return
	}

	ok, err := s.invites.SetStatusIfCurrent(ctx, roomHex, invite.InviteeID, api.InvitePending, api.InviteDeclined, s.now())
	if err != nil {
		log.Error().Err(err).Msg("cannot decline invite")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось отклонить приглашение")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "not_pending", "приглашение уже обработано")
		return
	}

	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteDeclined})
}

// pendingInviteCtx — приглашение вместе с профилем приглашённого.
type pendingInviteCtx struct {
	*api.RoomInvite
	user *api.User
}

// pendingInvite общая подготовка accept/decline: находит СВОЁ приглашение в
// статусе pending. Чужое отдаём как 404, а не 403 — существование чужих
// приглашений не наше дело раскрывать.
func (s *Server) pendingInvite(ctx context.Context, r *http.Request) (string, primitive.ObjectID, *pendingInviteCtx, *httpError) {
	if s.invites == nil {
		return "", primitive.NilObjectID, nil, &httpError{http.StatusServiceUnavailable, "unavailable", "приглашения недоступны"}
	}

	roomId := r.PathValue("roomId")
	roomHex, err := primitive.ObjectIDFromHex(roomId)
	if err != nil {
		return "", primitive.NilObjectID, nil, &httpError{http.StatusNotFound, "not_found", "приглашение не найдено"}
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		return "", primitive.NilObjectID, nil, hErr
	}

	invite, err := s.invites.Find(ctx, roomHex, user.ID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", primitive.NilObjectID, nil, &httpError{http.StatusNotFound, "not_found", "приглашение не найдено"}
		}
		log.Error().Err(err).Msg("cannot read invite")
		return "", primitive.NilObjectID, nil, &httpError{http.StatusInternalServerError, "internal", "не удалось получить приглашение"}
	}
	if invite.Status != api.InvitePending {
		return "", primitive.NilObjectID, nil, &httpError{http.StatusConflict, "not_pending", "приглашение уже обработано"}
	}

	return roomId, roomHex, &pendingInviteCtx{RoomInvite: invite, user: user}, nil
}

// handleLeaveRoom DELETE /api/v1/rooms/{roomId}/members/me — выйти самому.
func (s *Server) handleLeaveRoom(w http.ResponseWriter, r *http.Request) {
	s.removeMember(w, r, userIdFromCtx(r.Context()), true)
}

// handleRemoveMember DELETE /api/v1/rooms/{roomId}/members/{userId} — убрать
// участника. Доступно любому участнику: это лекарство от «позвал не того»,
// и в комнате все равны (расходы тоже правит кто угодно).
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	targetId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil || targetId == 0 {
		writeError(w, http.StatusNotFound, "not_found", "участник не найден")
		return
	}
	// Себя убираем только через /me: два пути к одному действию с разными
	// текстами ошибок путали бы и клиент, и человека.
	if targetId == userIdFromCtx(r.Context()) {
		writeError(w, http.StatusBadRequest, "validation", "чтобы выйти самому, используйте /members/me")
		return
	}
	s.removeMember(w, r, targetId, false)
}

// removeMember общая часть выхода и удаления участника.
//
// Главное правило: пока на человеке висят расходы, убрать его нельзя — иначе
// уход стирал бы долг, и кредитор молча терял бы деньги. Это НЕ тупик: правка и
// удаление расхода открыты любому участнику, поэтому себя можно убрать из
// операции (а если он плательщик — сменить плательщика или удалить расход) и
// после этого выйти. Текст ошибки обязан это объяснять.
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request, targetId int, isSelf bool) {
	ctx := r.Context()
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userIdFromCtx(ctx))
	if hErr != nil {
		hErr.write(w)
		return
	}
	if !isRoomMember(room, targetId) {
		writeError(w, http.StatusNotFound, "not_found", "участник не найден")
		return
	}

	// Проверка на НОРМАЛИЗОВАННОЙ комнате: у легаси-операций recipients_with_sum
	// в базе нет, доли синтезируются в памяти (normalizedOperation). Фильтром
	// mongo это не выразить, поэтому решение принимается здесь.
	if hasOperations(room, targetId) {
		msg := "На вас записаны расходы. Уберите себя из них — или, если вы плательщик, смените плательщика либо удалите расход. После этого сможете выйти"
		if !isSelf {
			msg = "На участнике записаны расходы. Уберите его из них — или, если он плательщик, смените плательщика либо удалите расход. После этого сможете его убрать"
		}
		writeError(w, http.StatusConflict, "has_operations", msg)
		return
	}

	// Последнего участника не убираем: комната осталась бы без владельцев, а
	// удаления комнаты в REST нет — только персональный архив.
	if len(roomMembers(room)) <= 1 {
		writeError(w, http.StatusConflict, "last_member",
			"Вы последний участник. Заархивируйте группу, если она больше не нужна")
		return
	}

	left, err := s.roomRepo.LeaveRoom(ctx, targetId, roomId)
	if err != nil {
		log.Error().Err(err).Msg("cannot leave room")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выйти из группы")
		return
	}
	if !left {
		// Гонку с параллельным выходом трактуем как успех: состояние уже такое,
		// каким его хотел видеть вызывающий.
		writeJSON(w, http.StatusNoContent, nil)
		return
	}

	if s.invites != nil {
		roomHex, hexErr := primitive.ObjectIDFromHex(roomId)
		if hexErr == nil {
			// Запись left закрывает тихий возврат: следующее приглашение пойдёт
			// через pending и будет ждать согласия человека.
			if err = s.invites.Upsert(ctx, roomHex, targetId, userIdFromCtx(ctx), api.InviteLeft, s.now()); err != nil {
				log.Error().Err(err).Msg("cannot mark invite as left")
			}
		}
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// hasOperations участвует ли пользователь хотя бы в одной АКТИВНОЙ операции —
// как донор или как получатель с ненулевой долей.
//
// Работает по activeOperations, а не по сырым данным: легаси-операции эпохи
// бота хранят recipients без recipients_with_sum, и доли для них синтезируются
// при нормализации. Без этого старые долги были бы не видны, и человек с
// реальной задолженностью спокойно вышел бы.
func hasOperations(room *api.Room, userId int) bool {
	for _, op := range activeOperations(room) {
		if op.Donor != nil && op.Donor.ID == userId {
			return true
		}
		for _, r := range op.RecipientsWithSum {
			if r.User.ID == userId {
				return true
			}
		}
	}
	return false
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
