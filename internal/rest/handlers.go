package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const maxRoomNameLen = 100

// maxDisplayNameLen потолок имени профиля: оно копируется во встроенные снимки
// участников всех комнат и подставляется в telegram-уведомления (лимит 4096)
const maxDisplayNameLen = 100
const defaultActivityLimit = 30

// maxActivityLimit верхняя граница limit в пагинации активности
const maxActivityLimit = 100

// httpError ошибка обработки запроса с http-статусом
type httpError struct {
	status  int
	code    string
	message string
}

func (e *httpError) write(w http.ResponseWriter) {
	writeError(w, e.status, e.code, e.message)
}

// decodeJSON декодирует json-тело запроса: 413 при превышении лимита
// maxRequestBodyBytes (см. maxBodyMiddleware), 400 при невалидном json
func decodeJSON(r *http.Request, dst interface{}) *httpError {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &httpError{http.StatusRequestEntityTooLarge, "too_large", "тело запроса слишком большое (лимит 1 МБ)"}
		}
		return &httpError{http.StatusBadRequest, "validation", "невалидный json"}
	}
	return nil
}

// currentUser возвращает профиль текущего пользователя из репозитория
func (s *Server) currentUser(ctx context.Context) (*api.User, *httpError) {
	user, err := s.userRepo.FindById(ctx, userIdFromCtx(ctx))
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, &httpError{http.StatusUnauthorized, "unauthorized", "пользователь не найден"}
		}
		log.Error().Err(err).Msg("cannot find user")
		return nil, &httpError{http.StatusInternalServerError, "internal", "не удалось получить пользователя"}
	}
	return user, nil
}

// findRoom ищет комнату по id: 404 если id невалиден или комнаты нет
func (s *Server) findRoom(ctx context.Context, roomId string) (*api.Room, *httpError) {
	if _, err := primitive.ObjectIDFromHex(roomId); err != nil {
		return nil, &httpError{http.StatusNotFound, "not_found", "комната не найдена"}
	}
	room, err := s.roomRepo.FindById(ctx, roomId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, &httpError{http.StatusNotFound, "not_found", "комната не найдена"}
		}
		log.Error().Err(err).Msgf("cannot find room id: %s", roomId)
		return nil, &httpError{http.StatusInternalServerError, "internal", "не удалось получить комнату"}
	}
	return room, nil
}

// roomForMember ищет комнату и проверяет, что пользователь — её участник
func (s *Server) roomForMember(ctx context.Context, roomId string, userId int) (*api.Room, *httpError) {
	room, hErr := s.findRoom(ctx, roomId)
	if hErr != nil {
		return nil, hErr
	}
	if !isRoomMember(room, userId) {
		return nil, &httpError{http.StatusForbidden, "forbidden", "вы не участник этой комнаты"}
	}
	return room, nil
}

// roomDebtsSafe вычисляет долги комнаты расчётом develop (service.GetRoomDebts)
// по нормализованной копии комнаты: легаси-операции без recipients_with_sum
// получают синтезированные канонические доли, драфты и архив не участвуют.
// Nil-безопасно для комнат без операций или участников.
//
// ok=false — комната «неисчислима»: на легаси-данных старых версий бота
// (recipients_with_sum ≠ sum операции, получатели вне members и т.п.) балансы
// не сходятся и GetRoomDebts возвращает ошибку. Чинить математику таких комнат
// нельзя (бот на них ошибается так же) — REST деградирует: пустые долги,
// нулевой баланс, debtsUnavailable=true в DTO. Лог — однострочный warn,
// а не error: кейс известный и срабатывает на каждый запрос к комнате
func (s *Server) roomDebtsSafe(room *api.Room) ([]api.Debt, bool) {
	norm := normalizedRoom(room)
	if len(*norm.Operations) == 0 || len(*norm.Members) == 0 {
		return []api.Debt{}, true
	}
	debts, err := service.GetRoomDebts(norm)
	if err != nil {
		log.Warn().Msgf("debts unavailable for room %s: %v", room.ID.Hex(), err)
		return []api.Debt{}, false
	}
	if debts == nil {
		return []api.Debt{}, true
	}
	return debts, true
}

// buildRoomDetail собирает RoomDetail для пользователя. Комната с неисчислимыми
// долгами (см. roomDebtsSafe) всё равно открывается: операции, участники и траты
// видны, debts=[], myBalance=0, debtsUnavailable=true
func (s *Server) buildRoomDetail(room *api.Room, userId int) *roomDetailDto {
	debts, ok := s.roomDebtsSafe(room)
	ops := activeOperations(room)
	return &roomDetailDto{
		ID:               room.ID.Hex(),
		Name:             room.Name,
		CreatedAt:        room.CreateAt,
		IsArchived:       isRoomArchived(room, userId),
		Currency:         roomCurrencyCode(room),
		Members:          toUserDtos(roomMembers(room)),
		TotalSpent:       roomTotalSpent(ops),
		MySpent:          userSpentSum(ops, userId),
		MyBalance:        balanceFromDebts(debts, userId),
		Debts:            toDebtDtos(debts),
		DebtsUnavailable: !ok,
		Operations:       toOperationDtos(ops),
	}
}

// Профиль

// handleGetMe GET /api/v1/me
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user, hErr := s.currentUser(r.Context())
	if hErr != nil {
		hErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, toMeDto(user))
}

type patchMeRequest struct {
	DisplayName    *string `json:"displayName"`
	Lang           *string `json:"lang"`
	NotificationOn *bool   `json:"notificationOn"`
}

// handlePatchMe PATCH /api/v1/me
func (s *Server) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	var req patchMeRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "validation", "имя не может быть пустым")
		return
	}
	// имя копируется во встроенные снимки участников каждой комнаты и попадает
	// в текст telegram-уведомлений: без потолка (тело ограничено лишь 1 МБ)
	// одно имя ломало бы доставку уведомлений всей комнате — как и описание
	// операции, см. maxDescriptionRunes
	if req.DisplayName != nil && utf8.RuneCountInString(strings.TrimSpace(*req.DisplayName)) > maxDisplayNameLen {
		writeError(w, http.StatusBadRequest, "validation", "имя не должно превышать 100 символов")
		return
	}
	if req.Lang != nil && *req.Lang != "ru" && *req.Lang != "en" {
		writeError(w, http.StatusBadRequest, "validation", "поддерживаемые языки: ru, en")
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
		if _, err := s.userRepo.UpsertUser(ctx, *user); err != nil {
			log.Error().Err(err).Msg("cannot update user")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось обновить профиль")
			return
		}
	}
	if req.Lang != nil {
		if err := s.userRepo.SetUserLang(ctx, userId, *req.Lang); err != nil {
			log.Error().Err(err).Msg("cannot set user lang")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось обновить язык")
			return
		}
	}
	if req.NotificationOn != nil {
		if err := s.userRepo.SetNotificationUser(ctx, userId, *req.NotificationOn); err != nil {
			log.Error().Err(err).Msg("cannot set user notification")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось обновить уведомления")
			return
		}
	}

	user, hErr = s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, toMeDto(user))
}

// Комнаты

// handleListRooms GET /api/v1/rooms?archived=false
func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	var archived bool
	switch r.URL.Query().Get("archived") {
	case "", "false":
		archived = false
	case "true":
		archived = true
	default:
		writeError(w, http.StatusBadRequest, "validation", "archived должен быть true или false")
		return
	}

	var rooms *[]api.Room
	var err error
	if archived {
		rooms, err = s.roomRepo.FindArchivedRoomsByUserId(ctx, userId)
	} else {
		rooms, err = s.roomRepo.FindRoomsByUserId(ctx, userId)
	}
	if err != nil {
		log.Error().Err(err).Msg("cannot find rooms")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить комнаты")
		return
	}

	summaries := make([]roomSummaryDto, 0)
	if rooms != nil {
		for i := range *rooms {
			room := &(*rooms)[i]
			// неисчислимая комната (см. roomDebtsSafe) остаётся в списке:
			// myBalance=0, debtsUnavailable=true
			debts, ok := s.roomDebtsSafe(room)
			members := roomMembers(room)
			summaries = append(summaries, roomSummaryDto{
				ID:               room.ID.Hex(),
				Name:             room.Name,
				CreatedAt:        room.CreateAt,
				IsArchived:       isRoomArchived(room, userId),
				Currency:         roomCurrencyCode(room),
				Members:          toUserDtos(members),
				MemberCount:      len(members),
				TotalSpent:       roomTotalSpent(activeOperations(room)),
				MyBalance:        balanceFromDebts(debts, userId),
				DebtsUnavailable: !ok,
			})
		}
	}
	writeJSON(w, http.StatusOK, summaries)
}

type createRoomRequest struct {
	Name string `json:"name"`
}

// handleCreateRoom POST /api/v1/rooms
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req createRoomRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "validation", "название комнаты не может быть пустым")
		return
	}
	if utf8.RuneCountInString(name) > maxRoomNameLen {
		writeError(w, http.StatusBadRequest, "validation", "название комнаты не должно превышать 100 символов")
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	room, err := s.roomSrv.CreateRoom(ctx, &api.Room{
		Name:       name,
		Members:    &[]api.User{*user},
		Operations: &[]api.Operation{},
		CreateAt:   time.Now(),
	})
	if err != nil {
		log.Error().Err(err).Msg("cannot create room")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось создать комнату")
		return
	}

	writeJSON(w, http.StatusCreated, s.buildRoomDetail(room, user.ID))
}

// handleGetRoom GET /api/v1/rooms/{roomId}
func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	room, hErr := s.roomForMember(ctx, r.PathValue("roomId"), userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	writeJSON(w, http.StatusOK, s.buildRoomDetail(room, userId))
}

// handleJoinRoom POST /api/v1/rooms/{roomId}/join — идемпотентное присоединение
func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomId := r.PathValue("roomId")

	room, hErr := s.findRoom(ctx, roomId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	user, hErr := s.currentUser(ctx)
	if hErr != nil {
		hErr.write(w)
		return
	}

	if !isRoomMember(room, user.ID) {
		if err := s.roomRepo.JoinToRoom(ctx, *user, roomId); err != nil {
			log.Error().Err(err).Msg("cannot join to room")
			writeError(w, http.StatusInternalServerError, "internal", "не удалось присоединиться к комнате")
			return
		}
		if room, hErr = s.findRoom(ctx, roomId); hErr != nil {
			hErr.write(w)
			return
		}
	}

	writeJSON(w, http.StatusOK, s.buildRoomDetail(room, user.ID))
}

// handleArchiveRoom POST /api/v1/rooms/{roomId}/archive
func (s *Server) handleArchiveRoom(w http.ResponseWriter, r *http.Request) {
	s.setRoomArchived(w, r, true)
}

// handleUnarchiveRoom POST /api/v1/rooms/{roomId}/unarchive
func (s *Server) handleUnarchiveRoom(w http.ResponseWriter, r *http.Request) {
	s.setRoomArchived(w, r, false)
}

func (s *Server) setRoomArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	if _, hErr := s.roomForMember(ctx, roomId, userId); hErr != nil {
		hErr.write(w)
		return
	}

	var err error
	if archived {
		err = s.roomRepo.ArchiveRoom(ctx, userId, roomId)
	} else {
		err = s.roomRepo.UnArchiveRoom(ctx, userId, roomId)
	}
	if err != nil {
		log.Error().Err(err).Msg("cannot change room archive state")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось изменить статус архива")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Валюта

type updateCurrencyRequest struct {
	Currency string `json:"currency"`
}

// handleUpdateCurrency PUT /api/v1/rooms/{roomId}/currency — смена валюты комнаты.
// Право — любой участник комнаты (как в боте develop)
func (s *Server) handleUpdateCurrency(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roomId := r.PathValue("roomId")

	if _, hErr := s.roomForMember(ctx, roomId, userIdFromCtx(ctx)); hErr != nil {
		hErr.write(w)
		return
	}

	var req updateCurrencyRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if !api.IsSupportedCurrency(req.Currency) {
		writeError(w, http.StatusBadRequest, "validation", "неподдерживаемый код валюты")
		return
	}

	if err := s.roomRepo.UpdateCurrency(ctx, roomId, req.Currency); err != nil {
		log.Error().Err(err).Msgf("cannot update currency for room %s", roomId)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось обновить валюту")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCurrencies GET /api/v1/currencies — справочник поддерживаемых валют
// (для пикера в приложении), стабильный порядок api.CurrencyCodes
func (s *Server) handleCurrencies(w http.ResponseWriter, _ *http.Request) {
	currencies := make([]currencyInfoDto, 0, len(api.CurrencyCodes))
	for _, code := range api.CurrencyCodes {
		info := api.Currencies[code]
		currencies = append(currencies, currencyInfoDto{Code: info.Code, Symbol: info.Symbol, Flag: info.Flag})
	}
	writeJSON(w, http.StatusOK, currencies)
}

// Операции

// handleListOperations GET /api/v1/rooms/{roomId}/operations?type=all|spend|repayment
func (s *Server) handleListOperations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	room, hErr := s.roomForMember(ctx, r.PathValue("roomId"), userIdFromCtx(ctx))
	if hErr != nil {
		hErr.write(w)
		return
	}

	opType := r.URL.Query().Get("type")
	if opType == "" {
		opType = "all"
	}
	if opType != "all" && opType != "spend" && opType != "repayment" {
		writeError(w, http.StatusBadRequest, "validation", "type должен быть all, spend или repayment")
		return
	}

	var filtered []api.Operation
	for _, o := range activeOperations(room) {
		if opType == "all" || (opType == "spend" && !o.IsDebtRepayment) || (opType == "repayment" && o.IsDebtRepayment) {
			filtered = append(filtered, o)
		}
	}
	writeJSON(w, http.StatusOK, toOperationDtos(filtered))
}

type recipientSumRequest struct {
	UserId int `json:"userId"`
	Sum    int `json:"sum"`
}

type operationRequest struct {
	Description   string                `json:"description"`
	Sum           int                   `json:"sum"`
	DonorId       int                   `json:"donorId"`
	RecipientIds  []int                 `json:"recipientIds"`
	RecipientSums []recipientSumRequest `json:"recipientSums"`
	// ClientOpId опциональный клиентский идемпотентный ключ (uuid ≤ 64 симв.):
	// повтор с тем же ключом не создаёт дубль (см. docs/API.md «Идемпотентность»).
	// В PUT игнорируется — хранимый ключ операции не меняется
	ClientOpId string `json:"clientOpId"`
	// Items опциональная детализация по позициям чека (AI-распознавание). При
	// наличии сервер САМ выводит RecipientsWithSum/Sum из позиций и игнорирует
	// плоские RecipientIds/RecipientSums (см. validateItemizedRequest)
	Items []ai.DraftItem `json:"items,omitempty"`
}

// validateOperationRequest валидирует тело операции, резолвит участников комнаты.
// Ровно один режим деления:
//   - recipientIds — equally: сервер раскладывает доли канонически
//     (base = sum/n, остаток по рублю первым получателям в порядке массива);
//   - recipientSums — by_exact_amount: суммы целые положительные, Σ == sum (400 иначе).
//
// Возвращает донора, готовые доли и тип деления
func validateOperationRequest(req *operationRequest, room *api.Room) (*api.User, []api.RecipientWithSum, api.SplitType, *httpError) {
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "описание не может быть пустым"}
	}
	if utf8.RuneCountInString(req.Description) > maxDescriptionRunes {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "слишком длинное описание"}
	}
	if req.Sum < 1 || req.Sum > maxItemsTotal {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "сумма должна быть от 1 до 1 000 000 000"}
	}
	if (len(req.RecipientIds) > 0) == (len(req.RecipientSums) > 0) {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation",
			"нужен ровно один способ деления: recipientIds (поровну) или recipientSums (точными суммами)"}
	}

	donor := findMember(room, req.DonorId)
	if donor == nil {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "донор должен быть участником комнаты"}
	}

	if len(req.RecipientIds) > 0 {
		// equally: дубликаты схлопываются, доли — канонически по порядку массива
		var recipients []api.User
		seen := map[int]bool{}
		for _, id := range req.RecipientIds {
			if seen[id] {
				continue
			}
			seen[id] = true
			recipient := findMember(room, id)
			if recipient == nil {
				return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "все получатели должны быть участниками комнаты"}
			}
			recipients = append(recipients, *recipient)
		}
		withSum := make([]api.RecipientWithSum, 0, len(recipients))
		for i := range recipients {
			withSum = append(withSum, api.RecipientWithSum{
				User: recipients[i],
				Sum:  float64(api.ShareOf(req.Sum, len(recipients), i)),
			})
		}
		return donor, withSum, splitEqually, nil
	}

	// by_exact_amount: каждый получатель ровно один раз, суммы целые положительные, Σ == sum
	withSum := make([]api.RecipientWithSum, 0, len(req.RecipientSums))
	seen := map[int]bool{}
	var total int
	for _, rs := range req.RecipientSums {
		if seen[rs.UserId] {
			return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "получатель не может повторяться в recipientSums"}
		}
		seen[rs.UserId] = true
		if rs.Sum < 1 || rs.Sum > maxItemsTotal {
			return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "суммы получателей должны быть от 1 до 1 000 000 000"}
		}
		recipient := findMember(room, rs.UserId)
		if recipient == nil {
			return nil, nil, "", &httpError{http.StatusBadRequest, "validation", "все получатели должны быть участниками комнаты"}
		}
		withSum = append(withSum, api.RecipientWithSum{User: *recipient, Sum: float64(rs.Sum)})
		total += rs.Sum
	}
	if total != req.Sum {
		return nil, nil, "", &httpError{http.StatusBadRequest, "validation",
			"сумма долей получателей должна равняться сумме операции"}
	}
	return donor, withSum, splitByExactAmount, nil
}

// findMember возвращает участника комнаты по id или nil
func findMember(room *api.Room, userId int) *api.User {
	members := roomMembers(room)
	for i := range members {
		if members[i].ID == userId {
			return &members[i]
		}
	}
	return nil
}

// findOperation возвращает АКТИВНУЮ операцию комнаты по hex id (нормализованную
// копию — база не мутируется) или nil: драфты бота и архивные версии для REST
// невидимы и не редактируются
func findOperation(room *api.Room, operationId string) *api.Operation {
	ops := activeOperations(room)
	for i := range ops {
		if ops[i].ID.Hex() == operationId {
			return &ops[i]
		}
	}
	return nil
}

// Идемпотентность создания (см. docs/API.md «Идемпотентность»)

// maxClientOpIdLen лимит длины клиентского идемпотентного ключа
const maxClientOpIdLen = 64

// validateClientOpId проверяет опциональный клиентский ключ: пустой валиден
// (идемпотентность выключена), иначе ≤ 64 символов из [A-Za-z0-9-] (формат UUID)
func validateClientOpId(id string) *httpError {
	if id == "" {
		return nil
	}
	if len(id) > maxClientOpIdLen {
		return &httpError{http.StatusBadRequest, "validation", "clientOpId не должен превышать 64 символа"}
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return &httpError{http.StatusBadRequest, "validation", "clientOpId может содержать только латинские буквы, цифры и дефис"}
		}
	}
	return nil
}

// findOperationByClientOpId возвращает нормализованную операцию комнаты с данным
// client_op_id или nil. Ищет по всем операциям (не только активным): ключ уникален
// в комнате, и повтор из outbox должен находить операцию даже после её изменения
func findOperationByClientOpId(room *api.Room, clientOpId string) *api.Operation {
	if clientOpId == "" {
		return nil
	}
	for _, o := range roomOperations(room) {
		if o.ClientOpId == clientOpId {
			n := normalizedOperation(o)
			return &n
		}
	}
	return nil
}

// createOperationIdempotent вставляет операцию с непустым ClientOpId ровно один раз:
// атомарный CreateOperationIfAbsent (проверка дубля + $push одним UpdateOne), при
// дубле — перечитывает комнату и возвращает существующую операцию (created=false).
// Пара попыток закрывает редчайшую гонку «дубль удалили между вставкой и перечиткой»
func (s *Server) createOperationIdempotent(ctx context.Context, operation *api.Operation, roomId string) (*api.Operation, bool, *httpError) {
	for attempt := 0; attempt < 3; attempt++ {
		created, err := s.operationSrv.CreateOperationIfAbsent(ctx, operation, roomId)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, false, &httpError{http.StatusNotFound, "not_found", "комната не найдена"}
			}
			log.Error().Err(err).Msg("create operation failed")
			return nil, false, &httpError{http.StatusInternalServerError, "internal", "не удалось сохранить операцию"}
		}
		if created {
			return operation, true, nil
		}
		room, hErr := s.findRoom(ctx, roomId)
		if hErr != nil {
			return nil, false, hErr
		}
		if existing := findOperationByClientOpId(room, operation.ClientOpId); existing != nil {
			return existing, false, nil
		}
	}
	log.Error().Msgf("idempotent create: duplicate keeps vanishing, room %s", roomId)
	return nil, false, &httpError{http.StatusInternalServerError, "internal", "не удалось сохранить операцию"}
}

// notifyOperationMutation шлёт уведомление о мутации операции в фоне (см.
// notifyAsync): room, op и author копируются до старта горутины, чтобы она
// не разделяла память с обработчиком запроса
func (s *Server) notifyOperationMutation(ctx context.Context, room *api.Room, author *api.User,
	notify func(ctx context.Context, n Notifier, room api.Room, author api.User)) {
	if author == nil {
		return
	}
	// Members и Operations — указатели на слайсы, поэтому поверхностного *room
	// мало: горутина делила бы с обработчиком те же массивы. Копируем их явно
	roomCopy := *room
	if room.Members != nil {
		members := append([]api.User(nil), *room.Members...)
		roomCopy.Members = &members
	}
	if room.Operations != nil {
		operations := append([]api.Operation(nil), *room.Operations...)
		roomCopy.Operations = &operations
	}
	authorCopy := *author
	s.notifyAsync(ctx, func(nctx context.Context, n Notifier) {
		notify(nctx, n, roomCopy, authorCopy)
	})
}

// handleCreateOperation POST /api/v1/rooms/{roomId}/operations
func (s *Server) handleCreateOperation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	var req operationRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if hErr := validateClientOpId(req.ClientOpId); hErr != nil {
		hErr.write(w)
		return
	}
	// повтор из outbox: операция с этим clientOpId уже есть — вернуть её с 200
	// ДО валидаций (состояние комнаты могло измениться после первой доставки)
	if existing := findOperationByClientOpId(room, req.ClientOpId); existing != nil {
		writeJSON(w, http.StatusOK, toOperationDto(existing))
		return
	}

	// две ветки: itemized (сервер выводит суммы из позиций) и обычная (плоские доли)
	var (
		donor             *api.User
		recipientsWithSum []api.RecipientWithSum
		splitType         api.SplitType
		sum               int
		items             []api.OperationItem
		hErr2             *httpError
	)
	if len(req.Items) > 0 {
		donor, recipientsWithSum, items, sum, hErr2 = validateItemizedRequest(&req, room)
		splitType = splitByExactAmount
	} else {
		donor, recipientsWithSum, splitType, hErr2 = validateOperationRequest(&req, room)
		sum = req.Sum
	}
	if hErr2 != nil {
		hErr2.write(w)
		return
	}

	// поля — как у бота develop (AddDonorOperation), но сразу Status=active:
	// у REST нет драфт-шага «Сохранить». Легаси-поле Recipients не заполняется
	operation := &api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       req.Description,
		Sum:               sum,
		Donor:             donor,
		RecipientsWithSum: recipientsWithSum,
		SplitType:         splitType,
		Status:            statusActive,
		IsDebtRepayment:   false,
		CreateAt:          time.Now(),
		NotificationSent:  []int{},
		Files:             []api.File{},
		ClientOpId:        req.ClientOpId,
		Items:             items,
	}
	if req.ClientOpId != "" {
		result, created, hErr := s.createOperationIdempotent(ctx, operation, roomId)
		if hErr != nil {
			hErr.write(w)
			return
		}
		status := http.StatusCreated
		if created {
			// повтор из outbox (created=false) не уведомляет второй раз
			op := *result
			s.notifyOperationMutation(ctx, room, findMember(room, userId),
				func(nctx context.Context, n Notifier, room api.Room, author api.User) {
					n.NotifyOperationCreated(nctx, room, op, author)
				})
		} else {
			status = http.StatusOK
		}
		writeJSON(w, status, toOperationDto(result))
		return
	}
	if err := s.operationSrv.CreateOperation(ctx, operation, roomId); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "not_found", "комната не найдена")
			return
		}
		log.Error().Err(err).Msg("create operation failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить операцию")
		return
	}
	op := *operation
	s.notifyOperationMutation(ctx, room, findMember(room, userId),
		func(nctx context.Context, n Notifier, room api.Room, author api.User) {
			n.NotifyOperationCreated(nctx, room, op, author)
		})
	writeJSON(w, http.StatusCreated, toOperationDto(operation))
}

// handleUpdateOperation PUT /api/v1/rooms/{roomId}/operations/{operationId}
func (s *Server) handleUpdateOperation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	operation := findOperation(room, r.PathValue("operationId"))
	if operation == nil {
		writeError(w, http.StatusNotFound, "not_found", "операция не найдена")
		return
	}
	// Редактировать расход может любой участник комнаты (Splitwise-семантика);
	// членство проверено выше, автор изменения виден в Telegram-уведомлении.
	if operation.IsDebtRepayment {
		writeError(w, http.StatusConflict, "conflict", "погашение долга нельзя редактировать")
		return
	}

	var req operationRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}

	// две ветки, как в create: itemized (сервер выводит суммы, Items сохраняются)
	// и обычная (плоские доли; Items ПРИНУДИТЕЛЬНО затираются — иначе на операции
	// осталась бы протухшая разбивка, не соответствующая новым суммам)
	var (
		donor             *api.User
		recipientsWithSum []api.RecipientWithSum
		splitType         api.SplitType
		newSum            int
		items             []api.OperationItem
		hErr2             *httpError
	)
	if len(req.Items) > 0 {
		donor, recipientsWithSum, items, newSum, hErr2 = validateItemizedRequest(&req, room)
		splitType = splitByExactAmount
	} else {
		donor, recipientsWithSum, splitType, hErr2 = validateOperationRequest(&req, room)
		newSum = req.Sum
	}
	if hErr2 != nil {
		hErr2.write(w)
		return
	}

	// копия до мутации — по диффу old/new собираются уведомления участникам
	oldOp := *operation
	operation.Description = req.Description
	operation.Sum = newSum
	operation.Donor = donor
	operation.RecipientsWithSum = recipientsWithSum
	operation.SplitType = splitType
	operation.Status = statusActive
	// плоское обновление затирает позиции; itemized — сохраняет новые
	operation.Items = items
	// легаси-поле обнуляется: доли теперь только в recipients_with_sum
	operation.Recipients = nil
	if err := s.operationSrv.UpdateOperation(ctx, operation, roomId); err != nil {
		if err == mongo.ErrNoDocuments {
			// комната или операция исчезла между чтением и атомарным обновлением
			writeError(w, http.StatusNotFound, "not_found", "операция не найдена")
			return
		}
		log.Error().Err(err).Msg("update operation failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить операцию")
		return
	}
	newOp := *operation
	s.notifyOperationMutation(ctx, room, findMember(room, userId),
		func(nctx context.Context, n Notifier, room api.Room, author api.User) {
			n.NotifyOperationUpdated(nctx, room, oldOp, newOp, author)
		})
	writeJSON(w, http.StatusOK, toOperationDto(operation))
}

// handleDeleteOperation DELETE /api/v1/rooms/{roomId}/operations/{operationId}
func (s *Server) handleDeleteOperation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	operation := findOperation(room, r.PathValue("operationId"))
	if operation == nil {
		writeError(w, http.StatusNotFound, "not_found", "операция не найдена")
		return
	}
	// Удалить расход может любой участник комнаты (Splitwise-семантика).
	if err := s.operationSrv.DeleteOperation(ctx, roomId, operation.ID); err != nil {
		log.Error().Err(err).Msg("delete operation failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось удалить операцию")
		return
	}
	// удаление погашения не уведомляет: у бота нет такого сценария/текста
	if !operation.IsDebtRepayment {
		op := *operation
		s.notifyOperationMutation(ctx, room, findMember(room, userId),
			func(nctx context.Context, n Notifier, room api.Room, author api.User) {
				n.NotifyOperationDeleted(nctx, room, op, author)
			})
	}
	w.WriteHeader(http.StatusNoContent)
}

// Долги

// handleListDebts GET /api/v1/rooms/{roomId}/debts?involving=all|me
func (s *Server) handleListDebts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	room, hErr := s.roomForMember(ctx, r.PathValue("roomId"), userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	involving := r.URL.Query().Get("involving")
	if involving == "" {
		involving = "all"
	}
	if involving != "all" && involving != "me" {
		writeError(w, http.StatusBadRequest, "validation", "involving должен быть all или me")
		return
	}

	// неисчислимая комната (см. roomDebtsSafe) отдаёт пустой список долгов
	debts, _ := s.roomDebtsSafe(room)
	if involving == "me" {
		var mine []api.Debt
		for _, d := range debts {
			if (d.Debtor != nil && d.Debtor.ID == userId) || (d.Lender != nil && d.Lender.ID == userId) {
				mine = append(mine, d)
			}
		}
		debts = mine
	}
	writeJSON(w, http.StatusOK, toDebtDtos(debts))
}

type repaymentRequest struct {
	DebtorId int `json:"debtorId"`
	LenderId int `json:"lenderId"`
	Sum      int `json:"sum"`
	// ClientOpId опциональный клиентский идемпотентный ключ (uuid ≤ 64 симв.):
	// повтор с тем же ключом не создаёт дубль (см. docs/API.md «Идемпотентность»)
	ClientOpId string `json:"clientOpId"`
}

// handleCreateRepayment POST /api/v1/rooms/{roomId}/repayments — погашение долга
func (s *Server) handleCreateRepayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	var req repaymentRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	if hErr := validateClientOpId(req.ClientOpId); hErr != nil {
		hErr.write(w)
		return
	}
	// повтор из outbox: погашение с этим clientOpId уже есть — вернуть его с 200
	// ДО проверки долга (первая доставка уже погасила долг, повтор получил бы 409)
	if existing := findOperationByClientOpId(room, req.ClientOpId); existing != nil {
		writeJSON(w, http.StatusOK, toOperationDto(existing))
		return
	}
	if req.Sum < 1 {
		writeError(w, http.StatusBadRequest, "validation", "сумма должна быть не меньше 1")
		return
	}
	if req.DebtorId == req.LenderId {
		writeError(w, http.StatusBadRequest, "validation", "должник и кредитор должны быть разными")
		return
	}
	if userId != req.DebtorId && userId != req.LenderId {
		writeError(w, http.StatusForbidden, "forbidden", "погашение может создать только должник или кредитор")
		return
	}

	debtor := findMember(room, req.DebtorId)
	lender := findMember(room, req.LenderId)
	if debtor == nil || lender == nil {
		writeError(w, http.StatusBadRequest, "validation", "должник и кредитор должны быть участниками комнаты")
		return
	}

	// проверяем текущий долг debtor→lender по нормализованному расчёту develop.
	// В неисчислимой комнате (см. roomDebtsSafe) долгов «нет» — гасить нечего,
	// внятный 409 вместо 500
	debts, ok := s.roomDebtsSafe(room)
	if !ok {
		writeError(w, http.StatusConflict, "conflict",
			"долги этой группы не считаются из-за старых данных")
		return
	}
	var debt *api.Debt
	for i := range debts {
		if debts[i].Debtor != nil && debts[i].Lender != nil &&
			debts[i].Debtor.ID == req.DebtorId && debts[i].Lender.ID == req.LenderId {
			debt = &debts[i]
			break
		}
	}
	if debt == nil || debt.Sum <= 0 {
		writeError(w, http.StatusConflict, "conflict", "долга нет")
		return
	}
	if req.Sum > debt.Sum {
		writeError(w, http.StatusConflict, "conflict", "сумма превышает текущий долг")
		return
	}

	// поля — как у бота develop (AddRecepientOperation): погашение сразу active,
	// единственный получатель-кредитор на всю сумму, SplitType не заполняется
	operation := &api.Operation{
		ID:                primitive.NewObjectID(),
		Sum:               req.Sum,
		Donor:             debtor,
		RecipientsWithSum: []api.RecipientWithSum{{User: *lender, Sum: float64(req.Sum)}},
		IsDebtRepayment:   true,
		Status:            statusActive,
		CreateAt:          time.Now(),
		ClientOpId:        req.ClientOpId,
	}
	if req.ClientOpId != "" {
		result, created, hErr := s.createOperationIdempotent(ctx, operation, roomId)
		if hErr != nil {
			hErr.write(w)
			return
		}
		if !created {
			// дубль вставил конкурентный повтор — мы ничего не добавили,
			// компенсация переплаты не нужна
			writeJSON(w, http.StatusOK, toOperationDto(result))
			return
		}
	} else if err := s.operationSrv.CreateOperation(ctx, operation, roomId); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "not_found", "комната не найдена")
			return
		}
		log.Error().Err(err).Msg("create operation failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить погашение")
		return
	}

	// Компенсация вместо транзакции (mongo standalone транзакций не поддерживает):
	// проверка долга выше и вставка погашения неатомарны — конкурентный запрос мог
	// погасить тот же долг между ними. Перечитываем комнату и проверяем переплату;
	// нашли — удаляем вставленную операцию и отвечаем 409. Семантика at-most-once:
	// если оба конкурента откатились, долг остаётся непогашенным и клиент повторяет
	// запрос по актуальному состоянию — переплата в базе не задерживается никогда
	roomAfter, hErrAfter := s.findRoom(ctx, roomId)
	var overpaid, verified bool
	if hErrAfter == nil {
		var err error
		if overpaid, err = repaymentOverpaid(roomAfter, req.DebtorId, req.LenderId); err == nil {
			verified = true
		} else {
			// расчёт после вставки сломался (данные комнаты стали неисчислимыми
			// конкурентно) — это не 500: погашение сохранено, деградируем ниже
			log.Warn().Msgf("cannot verify repayment for room %s: %v", roomId, err)
		}
	}
	if !verified {
		// перепроверить не удалось: погашение уже сохранено, вслепую не откатываем
		if hErrAfter != nil {
			log.Warn().Msgf("cannot verify repayment for room %s: reread failed", roomId)
		}
	} else if overpaid {
		if delErr := s.operationSrv.DeleteOperation(ctx, roomId, operation.ID); delErr != nil {
			log.Error().Err(delErr).Msgf("cannot rollback overpaid repayment %s", operation.ID.Hex())
			writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить погашение")
			return
		}
		writeError(w, http.StatusConflict, "conflict", "долг уже погашен конкурентным запросом")
		return
	}

	// уведомляем после компенсационной проверки: откатившееся погашение не уведомляет
	op := *operation
	s.notifyOperationMutation(ctx, room, findMember(room, userId),
		func(nctx context.Context, n Notifier, room api.Room, author api.User) {
			n.NotifyRepaymentCreated(nctx, room, op, author)
		})
	writeJSON(w, http.StatusCreated, toOperationDto(operation))
}

// repaymentOverpaid распознаёт переплату после вставки погашения. В расчёте develop
// (AddReturnToDebts) излишек возврата не инвертирует долг, а молча теряется, поэтому
// по итоговым долгам гонку не увидеть. Сравниваем валовый долг пары (расчёт по одним
// расходам, без погашений) с суммой прямых возвратов debtor→lender: возвратов больше
// валового долга — конкурентное погашение перегасило тот же долг
func repaymentOverpaid(room *api.Room, debtorId, lenderId int) (bool, error) {
	norm := normalizedRoom(room)
	spends := make([]api.Operation, 0, len(*norm.Operations))
	var returns float64
	for _, o := range *norm.Operations {
		if !o.IsDebtRepayment {
			spends = append(spends, o)
			continue
		}
		if o.Donor != nil && o.Donor.ID == debtorId {
			for _, r := range o.RecipientsWithSum {
				if r.User.ID == lenderId {
					returns += r.Sum
				}
			}
		}
	}
	grossDebts, err := service.GetRoomDebts(api.Room{ID: room.ID, Members: norm.Members, Operations: &spends})
	if err != nil {
		return false, err
	}
	var gross float64
	for _, d := range grossDebts {
		if d.Debtor != nil && d.Lender != nil && d.Debtor.ID == debtorId && d.Lender.ID == lenderId {
			gross += float64(d.Sum)
		}
	}
	return returns > gross, nil
}

// Статистика

// byDayWindowDays окно графика трат по дням — последние 30 календарных дней
const byDayWindowDays = 30

// byMonthWindowMonths окно графика трат по месяцам — последние 6 календарных
// месяцев (включая текущий)
const byMonthWindowMonths = 6

// topOperationsLimit размер топа операций по сумме
const topOperationsLimit = 5

// handleStatistics GET /api/v1/rooms/{roomId}/statistics — расширенная статистика
// для дашборда. Считается по нормализованной комнате (activeOperations): только
// active-расходы, погашения исключены, легаси-операции — с синтезированными долями,
// доли получателей — канонические целые (recipientShare)
func (s *Server) handleStatistics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	roomId := r.PathValue("roomId")

	room, hErr := s.roomForMember(ctx, roomId, userId)
	if hErr != nil {
		hErr.write(w)
		return
	}

	// только расходы: погашения не траты и в статистику не входят
	var spends []api.Operation
	for _, o := range activeOperations(room) {
		if !o.IsDebtRepayment {
			spends = append(spends, o)
		}
	}

	now := s.now()
	stats := statisticsDto{
		Currency:       roomCurrencyCode(room),
		TotalSpent:     roomTotalSpent(spends),
		OperationCount: len(spends),
		MonthSpent:     monthSpent(spends, now),
		ByDay:          spentByDay(spends, now),
		ByMonth:        spentByMonth(spends, now),
		PaidByMember:   paidByMember(spends),
		ShareByMember:  shareByMember(spends),
		TopOperations:  topOperations(spends),
	}
	writeJSON(w, http.StatusOK, stats)
}

// monthSpent сумма расходов текущего календарного месяца (по create_at)
func monthSpent(spends []api.Operation, now time.Time) int {
	var total int
	for i := range spends {
		if spends[i].CreateAt.Year() == now.Year() && spends[i].CreateAt.Month() == now.Month() {
			total += spends[i].Sum
		}
	}
	return total
}

// spentByDay траты по дням за последние byDayWindowDays календарных дней
// (включая сегодня): только дни с тратами, ISO-даты, по возрастанию
func spentByDay(spends []api.Operation, now time.Time) []dailySumDto {
	from := now.AddDate(0, 0, -(byDayWindowDays - 1)).Format("2006-01-02")
	to := now.Format("2006-01-02")
	byDate := map[string]int{}
	for i := range spends {
		date := spends[i].CreateAt.Format("2006-01-02")
		if date < from || date > to {
			continue
		}
		byDate[date] += spends[i].Sum
	}
	days := make([]dailySumDto, 0, len(byDate))
	for date, sum := range byDate {
		days = append(days, dailySumDto{Date: date, Sum: sum})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days
}

// spentByMonth траты по календарным месяцам за последние byMonthWindowMonths
// месяцев (включая текущий): месяцы без трат присутствуют с нулевой суммой,
// месяц — "yyyy-mm", по возрастанию
func spentByMonth(spends []api.Operation, now time.Time) []monthlySumDto {
	sums := map[string]int{}
	for i := range spends {
		sums[spends[i].CreateAt.Format("2006-01")] += spends[i].Sum
	}
	// якорь — первое число текущего месяца: AddDate по месяцам от него
	// не переполняется на коротких месяцах
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	months := make([]monthlySumDto, 0, byMonthWindowMonths)
	for i := -(byMonthWindowMonths - 1); i <= 0; i++ {
		month := first.AddDate(0, i, 0).Format("2006-01")
		months = append(months, monthlySumDto{Month: month, Sum: sums[month]})
	}
	return months
}

// paidByMember кто сколько заплатил (доноры расходов), по убыванию суммы
func paidByMember(spends []api.Operation) []memberSumDto {
	sums := map[int]int{}
	users := map[int]*api.User{}
	for i := range spends {
		if spends[i].Donor == nil {
			continue
		}
		sums[spends[i].Donor.ID] += spends[i].Sum
		users[spends[i].Donor.ID] = spends[i].Donor
	}
	return sortedMemberSums(sums, users)
}

// shareByMember чья доля потребления (получатели расходов с каноническими
// целыми долями), по убыванию суммы
func shareByMember(spends []api.Operation) []memberSumDto {
	sums := map[int]int{}
	users := map[int]*api.User{}
	for i := range spends {
		o := &spends[i]
		for j := range o.RecipientsWithSum {
			u := &o.RecipientsWithSum[j].User
			sums[u.ID] += recipientShare(o, j)
			users[u.ID] = u
		}
	}
	return sortedMemberSums(sums, users)
}

// sortedMemberSums суммы участников по убыванию (при равенстве — по id
// для детерминированного порядка)
func sortedMemberSums(sums map[int]int, users map[int]*api.User) []memberSumDto {
	result := make([]memberSumDto, 0, len(sums))
	for id, sum := range sums {
		result = append(result, memberSumDto{User: toUserDto(users[id]), Sum: sum})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Sum != result[j].Sum {
			return result[i].Sum > result[j].Sum
		}
		return result[i].User.ID < result[j].User.ID
	})
	return result
}

// topOperations топ-topOperationsLimit расходов по сумме
// (при равенстве — новые первыми)
func topOperations(spends []api.Operation) []topOperationDto {
	sorted := make([]api.Operation, len(spends))
	copy(sorted, spends)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Sum != sorted[j].Sum {
			return sorted[i].Sum > sorted[j].Sum
		}
		return sorted[i].CreateAt.After(sorted[j].CreateAt)
	})
	if len(sorted) > topOperationsLimit {
		sorted = sorted[:topOperationsLimit]
	}
	top := make([]topOperationDto, 0, len(sorted))
	for i := range sorted {
		top = append(top, topOperationDto{
			ID:          sorted[i].ID.Hex(),
			Description: sorted[i].Description,
			Sum:         sorted[i].Sum,
			Donor:       toUserDto(sorted[i].Donor),
			CreatedAt:   sorted[i].CreateAt,
		})
	}
	return top
}

// Друзья и активность

// handleFriends GET /api/v1/friends — балансы по со-участникам всех неархивных комнат
func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	rooms, err := s.roomRepo.FindRoomsByUserId(ctx, userId)
	if err != nil {
		log.Error().Err(err).Msg("cannot find rooms")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить комнаты")
		return
	}

	friends := map[int]*friendBalanceDto{}
	// итог по другу считается отдельно на каждую валюту:
	// суммы в разных валютах складывать нельзя
	totalsByFriend := map[int]map[string]int{}
	if rooms != nil {
		for i := range *rooms {
			room := &(*rooms)[i]
			for _, m := range roomMembers(room) {
				if m.ID == userId {
					continue
				}
				if _, ok := friends[m.ID]; !ok {
					friends[m.ID] = &friendBalanceDto{User: toUserDto(&m), Rooms: []friendRoomBalanceDto{}}
					totalsByFriend[m.ID] = map[string]int{}
				}
			}

			// неисчислимая комната (см. roomDebtsSafe) не рушит агрегат:
			// её вклад пропускается, друзья из неё остаются в списке
			debts, ok := s.roomDebtsSafe(room)
			if !ok {
				continue
			}
			// баланс по каждому другу в этой комнате: >0 — друг должен мне
			balances := map[int]int{}
			for _, d := range debts {
				if d.Debtor == nil || d.Lender == nil {
					continue
				}
				if d.Lender.ID == userId {
					balances[d.Debtor.ID] += d.Sum
				}
				if d.Debtor.ID == userId {
					balances[d.Lender.ID] -= d.Sum
				}
			}
			currency := roomCurrencyCode(room)
			for friendId, balance := range balances {
				friend, ok := friends[friendId]
				if !ok || balance == 0 {
					continue
				}
				totalsByFriend[friendId][currency] += balance
				friend.Rooms = append(friend.Rooms, friendRoomBalanceDto{
					RoomId:   room.ID.Hex(),
					RoomName: room.Name,
					Currency: currency,
					Balance:  balance,
				})
			}
		}
	}

	result := make([]friendBalanceDto, 0, len(friends))
	// «вес» друга для сортировки — сумма модулей итогов по всем валютам
	totalAbs := map[int]int{}
	for id, f := range friends {
		f.TotalsByCurrency = currencyTotals(totalsByFriend[id])
		for _, t := range f.TotalsByCurrency {
			totalAbs[id] += abs(t.Sum)
		}
		result = append(result, *f)
	}
	// сначала ненулевые балансы по модулю убыванию, потом рассчитанные
	sort.Slice(result, func(i, j int) bool {
		ai, aj := totalAbs[result[i].User.ID], totalAbs[result[j].User.ID]
		if (ai == 0) != (aj == 0) {
			return ai != 0
		}
		if ai != aj {
			return ai > aj
		}
		return result[i].User.DisplayName < result[j].User.DisplayName
	})
	writeJSON(w, http.StatusOK, result)
}

// currencyTotals ненулевые итоги по валютам в стабильном порядке справочника
// (api.CurrencyCodes; неизвестные коды — после, по алфавиту)
func currencyTotals(sums map[string]int) []currencySumDto {
	totals := make([]currencySumDto, 0, len(sums))
	for _, code := range api.CurrencyCodes {
		if sum := sums[code]; sum != 0 {
			totals = append(totals, currencySumDto{Currency: code, Sum: sum})
		}
	}
	var rest []string
	for code, sum := range sums {
		if !api.IsSupportedCurrency(code) && sum != 0 {
			rest = append(rest, code)
		}
	}
	sort.Strings(rest)
	for _, code := range rest {
		totals = append(totals, currencySumDto{Currency: code, Sum: sums[code]})
	}
	return totals
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// handleActivity GET /api/v1/activity?limit=30&offset=0 — лента операций моих комнат
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

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

	rooms, err := s.roomRepo.FindRoomsByUserId(ctx, userId)
	if err != nil {
		log.Error().Err(err).Msg("cannot find rooms")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить комнаты")
		return
	}

	var items []activityItemDto
	if rooms != nil {
		for i := range *rooms {
			room := &(*rooms)[i]
			for _, o := range activeOperations(room) {
				op := o
				items = append(items, activityItemDto{
					RoomId:       room.ID.Hex(),
					RoomName:     room.Name,
					RoomCurrency: roomCurrencyCode(room),
					Operation:    toOperationDto(&op),
				})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Operation.CreatedAt.After(items[j].Operation.CreatedAt)
	})

	// клампим границы: queryInt гарантирует offset/limit >= 0, здесь ограничиваем
	// limit диапазоном [1, maxActivityLimit] — offset+limit после этого не переполняется
	if limit < 1 {
		limit = 1
	}
	if limit > maxActivityLimit {
		limit = maxActivityLimit
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[offset:end]
	if page == nil {
		page = []activityItemDto{}
	}
	writeJSON(w, http.StatusOK, page)
}

// queryInt парсит неотрицательный int-параметр запроса
func queryInt(r *http.Request, name string, defaultValue int) (int, *httpError) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, &httpError{http.StatusBadRequest, "validation", name + " должен быть неотрицательным числом"}
	}
	return value, nil
}

// Файлы

type tgFileResponse struct {
	Ok     bool `json:"ok"`
	Result struct {
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// handleGetFile GET /api/v1/files/{fileId} — проксирует файл операции из Telegram
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)
	fileId := r.PathValue("fileId")

	if s.cfg.TgToken == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "telegram-бот не сконфигурирован")
		return
	}

	allowed, hErr := s.userHasFile(ctx, userId, fileId)
	if hErr != nil {
		hErr.write(w)
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "forbidden", "нет доступа к этому файлу")
		return
	}

	// оба запроса к telegram привязаны к контексту входящего запроса: клиент отключился
	// или сервер останавливается — скачивание прерывается сразу, без висящих горутин
	getFileURL := fmt.Sprintf("%s/bot%s/getFile?file_id=%s", s.tgApiURL, s.cfg.TgToken, url.QueryEscape(fileId))
	resp, err := s.tgGet(ctx, getFileURL)
	if err != nil {
		log.Error().Err(err).Msg("telegram getFile failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить файл из telegram")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var fileResp tgFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil || !fileResp.Ok || fileResp.Result.FilePath == "" {
		log.Error().Err(err).Msgf("telegram getFile bad response, status %d", resp.StatusCode)
		writeError(w, http.StatusNotFound, "not_found", "файл не найден в telegram")
		return
	}

	downloadURL := fmt.Sprintf("%s/file/bot%s/%s", s.tgApiURL, s.cfg.TgToken, fileResp.Result.FilePath)
	fileResp2, err := s.tgGet(ctx, downloadURL)
	if err != nil {
		log.Error().Err(err).Msg("telegram file download failed")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать файл из telegram")
		return
	}
	defer func() { _ = fileResp2.Body.Close() }()
	if fileResp2.StatusCode != http.StatusOK {
		log.Error().Msgf("telegram file download status %d", fileResp2.StatusCode)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось скачать файл из telegram")
		return
	}

	contentType := fileResp2.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		if byExt := mime.TypeByExtension(path.Ext(fileResp.Result.FilePath)); byExt != "" {
			contentType = byExt
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// вложение загружает любой участник комнаты, а отдаём мы его со своего
	// origin: html-файл без этих заголовков исполнялся бы как наша страница
	if !allowedInlineTypes[contentType] {
		contentType = "application/octet-stream"
		w.Header().Set("Content-Disposition", "attachment")
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if cl := fileResp2.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, fileResp2.Body); err != nil {
		log.Error().Err(err).Msg("cannot stream file")
	}
}

// tgGet выполняет GET к telegram api в контексте входящего запроса.
// У httpClient нет общего Timeout (он обрезал бы длинные скачивания больших файлов),
// таймаут только на фазу соединения/заголовков — Transport.ResponseHeaderTimeout
func (s *Server) tgGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, redactTgToken(err)
	}
	resp, err := s.httpClient.Do(req)
	return resp, redactTgToken(err)
}

// redactTgToken вычищает bot-токен из ошибки: *url.Error печатает полный URL, а
// токен у telegram лежит в ПУТИ (/bot<token>/getFile) — штатный stripPassword его
// не трогает, и любой таймаут/обрыв клиента сливал токен в логи.
func redactTgToken(err error) error {
	if err == nil {
		return nil
	}
	var uErr *url.Error
	if errors.As(err, &uErr) {
		if i := strings.Index(uErr.URL, "/bot"); i >= 0 {
			if j := strings.Index(uErr.URL[i+4:], "/"); j >= 0 {
				uErr.URL = uErr.URL[:i+4] + "***" + uErr.URL[i+4+j:]
			} else {
				uErr.URL = uErr.URL[:i+4] + "***"
			}
		}
	}
	return err
}

// userHasFile проверяет, что fileId встречается в операциях комнат пользователя (включая архивные)
func (s *Server) userHasFile(ctx context.Context, userId int, fileId string) (bool, *httpError) {
	for _, find := range []func(context.Context, int) (*[]api.Room, error){
		s.roomRepo.FindRoomsByUserId,
		s.roomRepo.FindArchivedRoomsByUserId,
	} {
		rooms, err := find(ctx, userId)
		if err != nil {
			log.Error().Err(err).Msg("cannot find rooms")
			return false, &httpError{http.StatusInternalServerError, "internal", "не удалось получить комнаты"}
		}
		if rooms == nil {
			continue
		}
		for i := range *rooms {
			for _, o := range roomOperations(&(*rooms)[i]) {
				for _, f := range o.Files {
					if f.FileId == fileId {
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
}
