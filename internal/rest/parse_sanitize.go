package rest

import (
	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
)

// лимиты защищают от галлюцинаций модели (сотни позиций/долей)
const (
	maxDraftItems = 50
	maxItemShares = 30
)

// sanitizeDraft приводит ответ модели к безопасному виду перед показом
// пользователю: userId только из участников комнаты, неотрицательные цены,
// лимиты числа позиций/долей, нормализация надбавок, пересчёт Sum из позиций.
// Модели не доверяем — это единственный барьер между её выводом и UI.
func sanitizeDraft(d ai.Draft, members []api.User) ai.Draft {
	memberSet := make(map[int]bool, len(members))
	for _, m := range members {
		memberSet[m.ID] = true
	}

	items := make([]ai.DraftItem, 0, len(d.Items))
	for _, it := range d.Items {
		if len(items) >= maxDraftItems {
			break
		}
		if it.Price < 0 {
			it.Price = 0
		}

		if it.Kind == string(api.ItemKindSurcharge) {
			if it.Price == 0 {
				continue // надбавка без суммы бессмысленна
			}
			if it.Split != string(api.SplitProportional) && it.Split != string(api.SplitEqually) {
				it.Split = string(api.SplitProportional)
			}
			it.Shares = nil // у надбавки доли не используются
			items = append(items, it)
			continue
		}

		// обычная позиция
		it.Kind = string(api.ItemKindItem)
		if it.Price == 0 {
			continue
		}
		shares := make([]ai.ItemShare, 0, len(it.Shares))
		for _, s := range it.Shares {
			if len(shares) >= maxItemShares {
				break
			}
			if !memberSet[s.UserId] {
				continue // чужой/выдуманный userId — имя остаётся в Unknown
			}
			if s.Amount != nil && *s.Amount < 0 {
				continue
			}
			if s.Amount == nil && s.Weight <= 0 {
				s.Weight = 1
			}
			shares = append(shares, s)
		}
		it.Shares = shares
		// позиция без участников и без нераспознанных имён — мусор
		if len(it.Shares) == 0 && len(it.Unknown) == 0 {
			continue
		}
		items = append(items, it)
	}
	d.Items = items

	// Sum — производная от позиций, а не от того, что назвала модель
	sum := 0
	for _, it := range items {
		sum += it.Price
	}
	d.Sum = sum

	if d.DonorId != nil && !memberSet[*d.DonorId] {
		d.DonorId = nil
	}
	return d
}

// hasUnknown сообщает, есть ли в черновике нераспознанные имена (сохранять
// такой черновик нельзя — их сперва разрешает пользователь).
func hasUnknown(d ai.Draft) bool {
	for _, it := range d.Items {
		if len(it.Unknown) > 0 {
			return true
		}
	}
	return false
}
