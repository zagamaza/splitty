package rest

import (
	"unicode/utf8"

	"github.com/almaznur91/splitty/internal/ai"
	"github.com/almaznur91/splitty/internal/api"
)

// truncateRunes режет строку по рунам (не по байтам — иначе кириллица бьётся).
func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// лимиты защищают от галлюцинаций модели (сотни позиций/долей)
const (
	maxDraftItems = 50
	maxItemShares = 30
)

// sanitizeDraft приводит ответ модели к безопасному виду перед показом
// пользователю: userId только из участников комнаты, неотрицательные цены,
// лимиты числа позиций/долей, нормализация надбавок; Sum при позициях —
// производная от них, у плоского черновика — сумма модели (в разумных пределах).
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
		// верхнюю границу цены черновик тоже обязан держать: без неё сумма
		// позиций ниже переполняла int и уходила в UI отрицательной, а клиенты
		// (iOS считает amount*weight на Int и трапится) падали на первом же
		// derivedShares
		if it.Price > maxItemPrice {
			it.Price = maxItemPrice
		}
		// та же причина, что и у цены: write-path отбивает длинное название 400,
		// а UI успевал показать черновик — пользователь видел необъяснимую ошибку
		it.Name = truncateRunes(it.Name, maxItemNameRunes)

		if it.Kind == string(api.ItemKindSurcharge) {
			if it.Price == 0 {
				continue // надбавка без суммы бессмысленна
			}
			if it.Split != string(api.SplitProportional) && it.Split != string(api.SplitEqually) {
				it.Split = string(api.SplitProportional)
			}
			it.Shares = nil // у надбавки доли не используются
			// Unknown у надбавки тоже нужно снять: UI не даёт разрешить имя там,
			// где нет пикера долей, а validateItemizedRequest режет ЛЮБУЮ позицию
			// с непустым Unknown — черновик становился несохраняемым навсегда
			it.Unknown = nil
			items = append(items, it)
			continue
		}

		// обычная позиция; price=0 допустим В ЧЕРНОВИКЕ — «цена не определена»
		// (модель услышала блюдо и участников, но не цену): UI пометит и попросит
		// заполнить, сохранение заблокировано до ввода цены.
		it.Kind = string(api.ItemKindItem)
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
			// верхние границы веса/фикса — те же, что проверит write-path:
			// иначе модель могла выдать черновик, который UI показывает, а
			// сохранение отбивает 400 без объяснимой для пользователя причины
			if s.Weight > maxShareWeight {
				s.Weight = maxShareWeight
			}
			if s.Amount != nil && *s.Amount > maxShareAmount {
				capped := maxShareAmount
				s.Amount = &capped
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
	// Чек из ОДНИХ надбавок невалиден: базовых долей нет, делить сбор не по чему
	// (модель порой метит такси/билеты как surcharge). Схлопываем в плоский
	// черновик — сумма сохраняется, деление пользователь задаст сам.
	hasRegular := false
	for _, it := range items {
		if it.Kind == string(api.ItemKindItem) {
			hasRegular = true
			break
		}
	}
	if !hasRegular && len(items) > 0 {
		sum := 0
		for _, it := range items {
			sum += it.Price
		}
		if d.Sum < sum {
			d.Sum = sum
		}
		items = nil
	}
	d.Items = items

	// Sum: при позициях с ценами — производная от них, а не от того, что назвала
	// модель. Но позиции БЕЗ цен допустимы («услышал блюда и людей, цену — нет»):
	// пересчёт по ним дал бы 0 и стёр сумму, названную вслух («пицца и салат,
	// всего 1200»). Поэтому сумму позиций берём, только если она положительная.
	itemsSum := 0
	for _, it := range items {
		itemsSum += it.Price
	}
	if itemsSum > 0 {
		d.Sum = itemsSum
	} else if d.Sum < 0 || d.Sum > maxItemsTotal {
		d.Sum = 0
	}

	d.Description = truncateRunes(d.Description, maxDescriptionRunes)

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
