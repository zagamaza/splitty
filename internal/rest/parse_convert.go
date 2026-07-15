package rest

import (
	"net/http"
	"strings"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
)

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
	// нераспознанные имена сохранять нельзя
	for _, it := range req.Items {
		if len(it.Unknown) > 0 {
			return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "сначала выберите, кто такие: " + strings.Join(it.Unknown, ", ")}
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

	shares, total, err := api.DeriveShares(apiItems)
	if err != nil {
		return nil, nil, nil, 0, &httpError{http.StatusBadRequest, "validation", "не удалось разложить позиции: " + err.Error()}
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
