package rest

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

type aliasRequest struct {
	Alias string `json:"alias"`
}

// Пределы величин itemized-операции. Щедрые для реальных денег, но на порядки
// ниже границы переполнения int64 при взвешенном делении (amount*weight),
// поэтому DeriveShares не может уйти в зацикливание на раздаче остатка.
const (
	maxItemPrice   = 100_000_000   // цена одной позиции
	maxShareWeight = 100_000       // вес доли участника
	maxShareAmount = 100_000_000   // фиксированная сумма доли
	maxItemsTotal  = 1_000_000_000 // суммарная цена всех позиций
	// название позиции клиент шлёт напрямую, а оно попадает в общее сообщение
	// бота: без ограничения одна позиция раздувает экран операции для всей комнаты
	maxItemNameRunes = 100
	maxAliasRunes    = 64 // максимальная длина прозвища
)

// handleAddAlias POST /api/v1/users/{userId}/aliases
// Добавляет прозвище участнику, чтобы AI сматчил его в следующий раз. Писать
// алиас в чужой профиль можно только тому, с кем есть общая комната.
func (s *Server) handleAddAlias(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	callerId := userIdFromCtx(ctx)

	targetId, err := strconv.Atoi(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "невалидный userId")
		return
	}

	var req aliasRequest
	if hErr := decodeJSON(r, &req); hErr != nil {
		hErr.write(w)
		return
	}
	alias := strings.ToLower(strings.TrimSpace(req.Alias))
	if alias == "" {
		writeError(w, http.StatusBadRequest, "validation", "прозвище не может быть пустым")
		return
	}
	if len([]rune(alias)) > maxAliasRunes {
		writeError(w, http.StatusBadRequest, "validation", "прозвище слишком длинное")
		return
	}

	if callerId != targetId && !s.shareRoom(ctx, callerId, targetId) {
		writeError(w, http.StatusForbidden, "forbidden", "нельзя добавить прозвище пользователю без общей комнаты")
		return
	}

	if err := s.userRepo.AddAlias(ctx, targetId, alias); err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, "not_found", "пользователь не найден")
			return
		}
		log.Error().Err(err).Msg("cannot add alias")
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить прозвище")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shareRoom сообщает, состоят ли двое в общей комнате.
func (s *Server) shareRoom(ctx context.Context, a, b int) bool {
	rooms, err := s.roomRepo.FindRoomsByUserId(ctx, a)
	if err != nil || rooms == nil {
		return false
	}
	for i := range *rooms {
		if isRoomMember(&(*rooms)[i], b) {
			return true
		}
	}
	return false
}

// toApiItems конвертирует транспортные позиции (ai.DraftItem) в доменные
// (api.OperationItem).
func toApiItems(items []ai.DraftItem) []api.OperationItem {
	out := make([]api.OperationItem, 0, len(items))
	for _, it := range items {
		shares := make([]api.ItemShare, 0, len(it.Shares))
		for _, s := range it.Shares {
			shares = append(shares, api.ItemShare{UserId: s.UserId, Weight: s.Weight, Amount: s.Amount})
		}
		out = append(out, api.OperationItem{
			Name:    it.Name,
			Price:   it.Price,
			Qty:     it.Qty,
			Shares:  shares,
			Kind:    api.ItemKind(it.Kind),
			Split:   api.SplitRule(it.Split),
			Percent: it.Percent,
		})
	}
	return out
}

// validateItemizedRequest валидирует itemized-операцию и выводит из позиций
// плоские доли. Сервер — единственный источник доверия: RecipientsWithSum и Sum
// считаются из Items, клиентские плоские поля игнорируются. Нельзя сохранить
// черновик с нераспознанными именами (Unknown) — сперва их разрешает пользователь.
func validateItemizedRequest(req *operationRequest, room *api.Room) (*api.User, []api.RecipientWithSum, []api.OperationItem, int, *httpError) {
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "описание не может быть пустым"}
	}
	donor := findMember(room, req.DonorId)
	if donor == nil {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "донор должен быть участником комнаты"}
	}
	if len(req.Items) == 0 {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "нужна хотя бы одна позиция"}
	}
	// лимиты числа позиций/долей: sanitizeDraft держит их для вывода модели, но
	// клиент шлёт JSON напрямую — без этой проверки в Mongo уходил бы чек на
	// десятки тысяч позиций
	if len(req.Items) > maxDraftItems {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "слишком много позиций в чеке"}
	}
	for _, it := range req.Items {
		// нераспознанные имена сохранять нельзя
		if len(it.Unknown) > 0 {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "сначала выберите, кто такие: " + strings.Join(it.Unknown, ", ")}
		}
		if len(it.Shares) > maxItemShares {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "слишком много участников у позиции"}
		}
		if utf8.RuneCountInString(it.Name) > maxItemNameRunes {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "слишком длинное название позиции"}
		}
		// kind/split — сырые строки от клиента: без проверки они ложились в базу
		// как есть и возвращались наружу, а неизвестный split молча трактовался
		// как equally
		if it.Kind != string(api.ItemKindItem) && it.Kind != string(api.ItemKindSurcharge) {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "неизвестный тип позиции"}
		}
		if it.Kind == string(api.ItemKindSurcharge) &&
			it.Split != string(api.SplitProportional) && it.Split != string(api.SplitEqually) {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "неизвестный способ деления надбавки"}
		}
	}

	apiItems := toApiItems(req.Items)

	// все userId позиций — участники комнаты
	for _, it := range apiItems {
		for _, s := range it.Shares {
			if findMember(room, s.UserId) == nil {
				return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "все участники позиций должны быть в комнате"}
			}
		}
	}

	// границы величин — защита от переполнения при взвешенном делении (DoS)
	priceSum := 0
	for _, it := range apiItems {
		// price ≥ 1: черновик допускает price=0 («цена не определена»),
		// но сохранять получек нельзя — клиент блокирует, сервер перепроверяет
		if it.Price < 1 || it.Price > maxItemPrice {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "у каждой позиции должна быть цена"}
		}
		priceSum += it.Price
		if priceSum > maxItemsTotal {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "суммарная стоимость позиций слишком велика"}
		}
		for _, sh := range it.Shares {
			if sh.Weight < 0 || sh.Weight > maxShareWeight {
				return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "вес доли вне допустимого диапазона"}
			}
			if sh.Amount != nil && (*sh.Amount < 0 || *sh.Amount > maxShareAmount) {
				return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "фиксированная сумма доли вне допустимого диапазона"}
			}
		}
	}

	shares, total, err := api.DeriveShares(apiItems)
	if err != nil {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "не удалось разложить позиции: " + err.Error()}
	}
	// операция должна иметь положительный итог и хотя бы одного получателя:
	// иначе (например, единственная позиция с price:0 и пустыми shares) сохранился
	// бы активный расход с sum=0 без долей, который normalizedOperation прячет как драфт
	if total < 1 || len(shares) == 0 {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "расход должен иметь положительную сумму и хотя бы одного участника"}
	}

	// userId → embedded User через участников комнаты (стабильный порядок по позициям)
	withSum := make([]api.RecipientWithSum, 0, len(shares))
	for _, it := range apiItems {
		for _, s := range it.Shares {
			if _, ok := shares[s.UserId]; !ok {
				continue
			}
			// добавляем каждого получателя один раз
			if containsRecipient(withSum, s.UserId) {
				continue
			}
			member := findMember(room, s.UserId)
			withSum = append(withSum, api.RecipientWithSum{User: *member, Sum: float64(shares[s.UserId])})
		}
	}

	return donor, withSum, apiItems, total, nil
}

func containsRecipient(rs []api.RecipientWithSum, userId int) bool {
	for i := range rs {
		if rs[i].User.ID == userId {
			return true
		}
	}
	return false
}
