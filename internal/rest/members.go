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
//  4. приглашение уже висит → 202, повтор идемпотентен;
//  5. запись есть, но участником он не является → 202, ждём согласия;
//  6. записи нет вовсе → проверка связи и добавление.
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

	// Снимок комнаты для решения о членстве перечитываем ЗАНОВО и ПОСЛЕ чтения
	// записи отношения. Снимок шага (1) успел устареть на чтение тела запроса
	// (его темп задаёт клиент) и два запроса в базу, а по нему решается «он уже
	// участник» — то есть решается судьба записи.
	//
	// Этот порядок вместе с условием по created_at (см. reconcileInvite) не даёт
	// примирению лечь по устаревшим данным: чужая запись между чтением записи и
	// нашей записью отменяет нашу, а выход, начавшийся после свежего чтения
	// комнаты, сам вернёт запись в left — он пишет left ДО удаления из комнаты
	// и переспрашивает состояние после (см. removeMember).
	room, hErr = s.roomForMember(ctx, roomId, inviterId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	// (3) Уже участник — идемпотентный 200, но с ПРИМИРЕНИЕМ записи. Человек мог
	// войти сам по ссылке /join/{roomId}, которая про приглашения ничего не
	// знает: без примирения он остался бы участником со статусом pending
	// навсегда, и следующее приглашение повело бы себя как для вышедшего.
	if isRoomMember(room, invitee.ID) {
		if existing == nil || existing.Status != api.InviteAdded {
			if err = s.reconcileInvite(ctx, roomHex, invitee.ID, inviterId, existing); err != nil {
				log.Error().Err(err).Str("room", roomId).Int("invitee", invitee.ID).Msg("cannot reconcile invite")
				writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
				return
			}
		}
		writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
		return
	}

	// (4) Приглашение уже ждёт решения — повтор идемпотентен: ни новой записи, ни
	// второго push. Без этой ветки второй вызов (тот же участник тапнул ещё раз
	// или позвал другой) уводил бы отношение по кругу в новый pending, поднимая
	// человеку ещё одно уведомление о том, о чём ему уже сообщили.
	if existing != nil && existing.Status == api.InvitePending {
		writeJSON(w, http.StatusAccepted, addMemberResponse{Status: api.InvitePending})
		return
	}

	// (5) Запись отношения ЕСТЬ, а участником человек не является. Тихо вернуть
	// его нельзя, иначе получался бы цикл «убрал себя из расхода → вышел →
	// добавили снова». Создаём приглашение, которое ждёт его решения.
	//
	// Ветка стоит на ЧЛЕНСТВЕ, а не на хранимом статусе (было: left или
	// declined), и это принципиально. added у не-участника — не битые данные, а
	// штатный исход гонки «добавили × вышел»: членство и запись отношения лежат в
	// РАЗНЫХ документах, атомарно их не связать (mongo развёрнут одним узлом,
	// транзакций нет), поэтому Upsert(added) после JoinToRoom всегда может лечь
	// уже после чужого выхода. Ветка по статусу пропускала бы такого человека в
	// шаг (6) и возвращала бы в комнату без спроса — та самая дыра, которую
	// круги 5-6 пытались закрыть условными фильтрами. Здесь она закрыта тем, что
	// про «участник или нет» спрашивают комнату, а не запись.
	if existing != nil {
		if err = s.invites.Upsert(ctx, roomHex, invitee.ID, inviterId, api.InvitePending, s.now()); err != nil {
			log.Error().Err(err).Str("room", roomId).Int("invitee", invitee.ID).Msg("cannot save invite")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить приглашение")
			return
		}
		s.notifyInvited(ctx, *room, *invitee, inviterId, true)
		writeJSON(w, http.StatusAccepted, addMemberResponse{Status: api.InvitePending})
		return
	}

	// (6) Записи нет вовсе — в этой комнате человека не звали и он из неё не
	// выходил. Тогда связь: друг ИЛИ общая неархивная комната.
	related, shareErr := s.shareRoom(ctx, inviterId, invitee.ID)
	if shareErr != nil {
		log.Error().Err(shareErr).Msg("cannot find rooms")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить комнаты")
		return
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
	// Запись added безусловна и может лечь уже ПОСЛЕ того, как человек успел
	// выйти. Раньше это было опасно (следующее приглашение видело added и молча
	// возвращало его в комнату) — теперь нет: и повторное приглашение, и карточки
	// раздела уведомлений судят о членстве по комнате, а не по этой строке.
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

	// JoinToRoom идемпотентен, поэтому ретрай безопасен. А вот его сбой обязан
	// вернуть статус обратно в pending: с записью added и без членства человек
	// упирался бы в not_pending на каждое следующее «Принять» и выбраться сам не
	// мог — приглашение чинил бы только кто-то другой, позвав его заново.
	if err = s.roomRepo.JoinToRoom(ctx, *user, roomId); err != nil {
		log.Error().Err(err).Msg("cannot join room on accept")
		s.rollbackAcceptedInvite(ctx, invite)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось войти в группу")
		return
	}

	writeJSON(w, http.StatusOK, addMemberResponse{Status: api.InviteAdded})
}

// rollbackAcceptedInvite возвращает принятое приглашение в pending после сбоя
// входа в комнату — но только если человека в комнате ДЕЙСТВИТЕЛЬНО нет.
//
// Ошибка JoinToRoom не означает, что запись не легла: ответ mongo мог потеряться
// уже после записи, а параллельно человека мог добавить другой запрос (второй
// тап, приглашение из бота, вход по ссылке). Откат «вслепую» тогда давал бы
// участника с приглашением pending: раздел уведомлений показывал бы ему выбор
// для комнаты, где он состоит, а тап по «Отклонить» записал бы declined
// участнику — то самое противоречие, ради которого заведён compare-and-set.
//
// Сам откат тоже compare-and-set: если статус за это время увели (второй тап,
// «Отклонить»), трогать его не наше дело. Ошибку чтения комнаты трактуем как
// «не знаем» и оставляем added: у участника он верен, а не-участника починит
// повторное приглашение (шаг 4 handleAddMember).
func (s *Server) rollbackAcceptedInvite(ctx context.Context, invite *api.RoomInvite) {
	roomId := invite.RoomID.Hex()
	fresh, err := s.roomRepo.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Str("room", roomId).Msg("cannot re-read room before invite rollback")
		return
	}
	if fresh != nil && isRoomMember(fresh, invite.InviteeID) {
		return
	}
	if _, err := s.invites.SetStatusIfCurrent(ctx, invite.RoomID, invite.InviteeID,
		api.InviteAdded, api.InvitePending, s.now()); err != nil {
		log.Error().Err(err).Str("room", roomId).Int("invitee", invite.InviteeID).
			Msg("cannot roll invite back to pending")
	}
}

// handleDeclineInvite POST /api/v1/invites/{roomId}/decline — отказаться.
func (s *Server) handleDeclineInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	invite, _, hErr := s.pendingInvite(ctx, r)
	if hErr != nil {
		hErr.write(w)
		return
	}

	// Участнику отказываться не от чего: членство у него УЖЕ есть, и «Отклонить»
	// его из комнаты не выведет — для этого отдельная кнопка «Выйти». Записав
	// declined, мы получили бы участника, помеченного отказавшимся, а состояние
	// «участник + pending» возникает штатно: откат неудавшегося accept и
	// примирение после входа по ссылке решают по снимку комнаты, который стареет.
	// Проверка ставит точку на записи, а не на чтении, — тогда declined у
	// участника не появляется вообще.
	roomId := invite.RoomID.Hex()
	room, err := s.roomRepo.FindById(ctx, roomId)
	if err != nil {
		log.Error().Err(err).Str("room", roomId).Msg("cannot read room on decline")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось отклонить приглашение")
		return
	}
	if room != nil && isRoomMember(room, invite.InviteeID) {
		if rErr := s.reconcileInvite(ctx, invite.RoomID, invite.InviteeID, invite.InviterID, invite); rErr != nil {
			log.Error().Err(rErr).Str("room", roomId).Msg("cannot reconcile invite on decline")
		}
		errInviteNotPending.write(w)
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
	// в базе нет, доли синтезируются в памяти (normalizedOperation). Полным
	// фильтром mongo это не выразить, поэтому решение принимается здесь — а от
	// расхода, заведённого уже ПОСЛЕ этой проверки, страхует узкий фильтр в
	// LeaveRoom (см. ветку !left ниже).
	if api.HasOperations(room, targetId) {
		writeError(w, http.StatusConflict, "has_operations", hasOperationsMessage(isSelf))
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

	left, err := s.roomRepo.LeaveRoom(ctx, targetId, roomId)
	if err != nil {
		log.Error().Err(err).Msg("cannot leave room")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось выйти из группы")
		return
	}
	if !left {
		// matched==0 значит либо «человека в комнате уже нет» (гонка двух
		// выходов — это успех, состояние такое, каким его хотел видеть
		// вызывающий), либо «фильтр LeaveRoom увидел активный расход»: его
		// завели на человека между проверкой выше и записью. Различаем свежим
		// чтением комнаты — иначе второй случай отдавался бы как успешный выход,
		// а человек оставался бы участником с долгом.
		fresh, fErr := s.roomRepo.FindById(ctx, roomId)
		if fErr == nil && fresh != nil && isRoomMember(fresh, targetId) {
			// Запись left при этом остаётся — тот же безвредный «лишний left»,
			// что и при сбое выхода: участник на месте, и шаг (3) вернёт её в added.
			writeError(w, http.StatusConflict, "has_operations", hasOperationsMessage(isSelf))
			return
		}
	}

	// Человека в комнате точно нет — самое время исправить запись, если её увёл
	// в added конкурентный шаг (3) handleAddMember по УСТАРЕВШЕМУ снимку комнаты
	// (на момент его чтения человек ещё был участником). Иначе получалось бы
	// «не в комнате, но приглашение added», и следующее приглашение вернуло бы
	// человека молча, мимо согласия. pending и declined не трогаем: для
	// не-участника они верны — его как раз позвали обратно или он отказался.
	if _, err := s.invites.SetStatusIfCurrent(ctx, roomHex, targetId, api.InviteAdded, api.InviteLeft, s.now()); err != nil {
		log.Error().Err(err).Str("room", roomId).Int("member", targetId).Msg("cannot re-assert invite as left")
	}

	w.WriteHeader(http.StatusNoContent)
}

// hasOperationsMessage запасной текст отказа «на человеке висят расходы»: оба
// клиента перехватывают отказ по КОДУ и рисуют свою локализованную строку
// (message сервера всегда по-русски). Синхронизировать надо код, не формулировку.
func hasOperationsMessage(isSelf bool) string {
	if isSelf {
		return "На вас записаны расходы. Уберите себя из них — или, если вы плательщик, смените плательщика либо удалите расход. После этого сможете выйти"
	}
	return "На участнике записаны расходы. Уберите его из них — или, если он плательщик, смените плательщика либо удалите расход. После этого сможете его убрать"
}

// errJoinNeedsInvite — ссылка на комнату у человека есть, но вернуться по ней
// он больше не может.
var errJoinNeedsInvite = &httpError{http.StatusConflict, "conflict",
	"Вернуться в группу можно только по новому приглашению участника"}

// checkJoinAllowed решает, пускать ли по ссылке того, кто в комнате не состоит.
//
// Ссылка на комнату вечная и расходится по перепискам: у вышедшего она остаётся
// навсегда. Пока вступление смотрело только на членство, убранный участник
// возвращался одним тапом — то есть удаление из группы не значило ничего, а
// человек снова видел все расходы. Правило продукта («вернуться можно только по
// приглашению участника») клиенты обещают в подтверждении выхода — здесь оно
// становится настоящим.
//
// Записи нет вовсе — пускаем: так в комнату попадают посторонние по ссылке, и
// это работавший сценарий, ломать его нельзя. Ошибку чтения тоже пропускаем:
// недоступное хранилище отношений не повод запретить вход.
func (s *Server) checkJoinAllowed(ctx context.Context, roomID primitive.ObjectID, userId int) *httpError {
	if s.invites == nil {
		return nil
	}
	existing, err := s.invites.Find(ctx, roomID, userId)
	if err != nil || existing == nil {
		return nil
	}
	// pending — человека позвали заново, ссылка равна кнопке «Принять»
	if existing.Status == api.InviteLeft || existing.Status == api.InviteDeclined {
		return errJoinNeedsInvite
	}
	return nil
}

// reconcileInviteOnJoin приводит запись отношения к added, когда человек вошёл
// в комнату сам — по коду или по ссылке /join.
//
// Без этого приглашённый, который вместо кнопки «Принять» прошёл по ссылке,
// оставался бы pending, и раздел уведомлений долго показывал бы ему карточку с
// выбором для комнаты, где он уже состоит. Сбой (как и уступка условной записи
// конкуренту) только логируем: членство уже записано, а от pending у участника
// вреда нет — карточку рисует членство, «Отклонить» участнику запрещено, и
// следующее приглашение доведёт запись шагом (3) handleAddMember.
func (s *Server) reconcileInviteOnJoin(ctx context.Context, roomID primitive.ObjectID, userId int) {
	if s.invites == nil {
		return
	}
	existing, err := s.invites.Find(ctx, roomID, userId)
	if err != nil || existing == nil || existing.Status == api.InviteAdded {
		return
	}
	if err = s.reconcileInvite(ctx, roomID, userId, existing.InviterID, existing); err != nil {
		log.Error().Err(err).Msg("cannot reconcile invite on join")
	}
}

// reconcileInvite приводит запись отношения к added для человека, который УЖЕ
// участник комнаты, — но только если с момента чтения записи её никто не менял.
//
// Условие обязательно: примирение опирается на снимок комнаты, а тот стареет.
// Без него интерлив «A прочитал комнату → человек вышел → его позвали заново
// (pending) → A дописал added» затирал бы свежее приглашение: карточка с
// «Принять» исчезала бы, хотя уведомление о ней человеку уже ушло, а возврат
// записи в left после выхода (см. removeMember) к этому моменту уже отработал
// и починить состояние было бы некому.
//
// «Уступили» и «записали» вызывающему одинаковы, поэтому наружу отдаётся только
// ошибка: в обоих случаях запись в согласованном состоянии и делать больше
// нечего. Уступка безобидна потому, что решение «участник или нет» нигде больше
// не принимается по хранимому статусу: и шаг (5) handleAddMember, и карточки
// раздела уведомлений спрашивают об этом комнату (см. inviteCardStatus). Оставшийся
// pending у участника — мусор в базе, а не состояние, по которому что-то
// произойдёт.
func (s *Server) reconcileInvite(ctx context.Context, roomID primitive.ObjectID, inviteeId, inviterId int,
	existing *api.RoomInvite) error {
	_, err := s.invites.UpsertIfUnchanged(ctx, roomID, inviteeId, inviterId, api.InviteAdded, existing, s.now())
	return err
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
