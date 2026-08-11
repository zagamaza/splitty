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

// errInviteNotPending единственный ответ на «приглашение уже обработано». К нему
// приходят три пути: чтение статуса в pendingInvite и проигранный compare-and-set
// в accept/decline — контракт один, и три копии литерала расходились бы.
var errInviteNotPending = &httpError{http.StatusConflict, "not_pending", "приглашение уже обработано"}

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
				log.Error().Err(err).Str("room", roomId).Int("invitee", invitee.ID).Msg("cannot reconcile invite")
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
			log.Error().Err(err).Str("room", roomId).Int("invitee", invitee.ID).Msg("cannot save invite")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
			return
		}
		s.notifyInvited(ctx, *room, *invitee, inviterId, true)
		writeJSON(w, http.StatusAccepted, addMemberResponse{Status: api.InvitePending})
		return
	}

	// Приглашение уже ждёт решения — повтор идемпотентен: ни новой записи, ни
	// второго push. Без этой ветки второй вызов (тот же участник тапнул ещё раз
	// или позвал другой) проваливался бы в шаг (5), где existing != nil делает
	// связь истинной, и человек оказался бы в комнате БЕЗ согласия — прямое
	// нарушение решения 5 плана.
	if existing != nil && existing.Status == api.InvitePending {
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
		var shareErr error
		related, shareErr = s.shareRoom(ctx, inviterId, invitee.ID)
		if shareErr != nil {
			log.Error().Err(shareErr).Msg("cannot find rooms")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось получить комнаты")
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
		log.Error().Err(err).Str("room", roomId).Int("invitee", invitee.ID).Msg("cannot save invite")
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
	invite, user, hErr := s.pendingInvite(ctx, r)
	if hErr != nil {
		hErr.write(w)
		return
	}
	roomId := invite.RoomID.Hex()

	room, err := s.roomRepo.FindById(ctx, roomId)
	if err != nil || room == nil {
		// Комнату успели удалить — принимать нечего.
		writeError(w, http.StatusNotFound, "not_found", "группа не найдена")
		return
	}

	ok, err := s.invites.SetStatusIfCurrent(ctx, invite.RoomID, invite.InviteeID, api.InvitePending, api.InviteAdded, s.now())
	if err != nil {
		log.Error().Err(err).Msg("cannot accept invite")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось принять приглашение")
		return
	}
	if !ok {
		errInviteNotPending.write(w)
		return
	}

	// JoinToRoom идемпотентен, поэтому ретрай безопасен. Если он всё же упадёт,
	// останется запись added без членства — её чинит повторное приглашение:
	// оно пойдёт по шагу (5) handleAddMember, потому что человек не участник.
	if err = s.roomRepo.JoinToRoom(ctx, *user, roomId); err != nil {
		log.Error().Err(err).Msg("cannot join room on accept")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось войти в группу")
		return
	}

	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
}

// handleDeclineInvite POST /api/v1/invites/{roomId}/decline — отказаться.
func (s *Server) handleDeclineInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	invite, _, hErr := s.pendingInvite(ctx, r)
	if hErr != nil {
		hErr.write(w)
		return
	}

	ok, err := s.invites.SetStatusIfCurrent(ctx, invite.RoomID, invite.InviteeID, api.InvitePending, api.InviteDeclined, s.now())
	if err != nil {
		log.Error().Err(err).Msg("cannot decline invite")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось отклонить приглашение")
		return
	}
	if !ok {
		errInviteNotPending.write(w)
		return
	}

	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteDeclined})
}

// pendingInvite общая подготовка accept/decline: находит СВОЁ приглашение в
// статусе pending и профиль приглашённого. Чужое отдаём как 404, а не 403 —
// существование чужих приглашений не наше дело раскрывать.
func (s *Server) pendingInvite(ctx context.Context, r *http.Request) (*api.RoomInvite, *api.User, *httpError) {
	if s.invites == nil {
		return nil, nil, &httpError{http.StatusServiceUnavailable, "unavailable", "приглашения недоступны"}
	}

	roomHex, err := primitive.ObjectIDFromHex(r.PathValue("roomId"))
	if err != nil {
		return nil, nil, &httpError{http.StatusNotFound, "not_found", "приглашение не найдено"}
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		return nil, nil, hErr
	}

	invite, err := s.invites.Find(ctx, roomHex, user.ID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil, &httpError{http.StatusNotFound, "not_found", "приглашение не найдено"}
		}
		log.Error().Err(err).Msg("cannot read invite")
		return nil, nil, &httpError{http.StatusInternalServerError, "internal", "не удалось получить приглашение"}
	}
	if invite.Status != api.InvitePending {
		return nil, nil, errInviteNotPending
	}

	return invite, user, nil
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
	// Тексты ниже — запасные: оба клиента перехватывают отказ по КОДУ и рисуют
	// свою локализованную строку (message сервера всегда по-русски). Держать их
	// дословно синхронными с клиентскими не нужно, синхронизировать надо код.
	if api.HasOperations(room, targetId) {
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

	// Запись left идёт ПЕРЕД выходом и её сбой отменяет операцию. Порядок
	// наоборот оставлял бы человека вне комнаты без следа отношения, и
	// следующее приглашение вернуло бы его молча, мимо «после выхода — только с
	// явного согласия». Лишний left при неудавшемся выходе безвреден: человек
	// остался участником, и шаг (3) handleAddMember примирит запись обратно
	// в added.
	//
	// Поэтому ни отсутствующее хранилище, ни неразбираемый id комнаты запись не
	// «пропускают»: молчаливый пропуск и есть тот самый тихий возврат, ради
	// закрытия которого запись заведена.
	if s.invites == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "приглашения недоступны")
		return
	}
	roomHex, hexErr := primitive.ObjectIDFromHex(roomId)
	if hexErr != nil {
		log.Error().Err(hexErr).Str("room", roomId).Msg("bad room id on leave")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выйти из группы")
		return
	}
	if err := s.invites.Upsert(ctx, roomHex, targetId, userIdFromCtx(ctx), api.InviteLeft, s.now()); err != nil {
		log.Error().Err(err).Msg("cannot mark invite as left")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выйти из группы")
		return
	}

	// Результат (matched) не смотрим: гонку с параллельным выходом трактуем как
	// успех — состояние уже такое, каким его хотел видеть вызывающий.
	if _, err := s.roomRepo.LeaveRoom(ctx, targetId, roomId); err != nil {
		log.Error().Err(err).Msg("cannot leave room")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выйти из группы")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// reconcileInviteOnJoin приводит запись отношения к added, когда человек вошёл
// в комнату сам — по коду или по ссылке /join.
//
// Без этого приглашённый, который вместо кнопки «Принять» прошёл по ссылке,
// оставался бы pending: раздел уведомлений вечно показывал бы ему карточку с
// выбором для комнаты, где он уже состоит, а тап по «Отклонить» записал бы
// declined участнику — ровно то противоречивое состояние, ради которого заведён
// compare-and-set. Сбой только логируем: членство уже записано, а следующее
// приглашение доведёт запись шагом (3) handleAddMember.
func (s *Server) reconcileInviteOnJoin(ctx context.Context, roomID primitive.ObjectID, userId int) {
	if s.invites == nil {
		return
	}
	existing, err := s.invites.Find(ctx, roomID, userId)
	if err != nil || existing == nil || existing.Status == api.InviteAdded {
		return
	}
	if err = s.invites.Upsert(ctx, roomID, userId, existing.InviterID, api.InviteAdded, s.now()); err != nil {
		log.Error().Err(err).Msg("cannot reconcile invite on join")
	}
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
