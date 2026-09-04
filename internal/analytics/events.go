// Package analytics — контракт продуктовых событий.
//
// Набор имён и допустимых значений описан в docs/analytics-events.md; он
// источник правды для обоих клиентов и для этого списка. Контракт-тест рядом
// сверяет одно с другим и падает, когда они разошлись.
//
// Незнакомое имя отбивается, а не пишется: иначе первый же эксперимент в
// клиенте насыплет в коллекцию мусор, который потом не отличить от данных.
package analytics

import "fmt"

// Event — что можно прислать под этим именем: ключ параметра → закрытое
// множество его значений. Свободного текста в параметрах не бывает.
type Event struct {
	Params map[string][]string
}

// Events — белый список. Меняется ТОЛЬКО вместе с docs/analytics-events.md.
var Events = map[string]Event{
	"app_open":             {Params: map[string][]string{"cold": {"true", "false"}}},
	"login_completed":      {Params: map[string][]string{"method": {"telegram", "google", "apple", "password", "code", "dev"}}},
	"onboarding_started":   {},
	"onboarding_step":      {Params: map[string][]string{"step": {"group", "dictate", "who_paid", "transfers"}}},
	"onboarding_completed": {},
	"onboarding_skipped":   {},
	"room_created":         {},
	"room_joined":          {Params: map[string][]string{"via": {"link", "code", "invite"}}},
	"room_join_failed":     {Params: map[string][]string{"reason": {"not_found", "deleted", "forbidden", "network"}}},
	"expense_added":        {Params: map[string][]string{"method": {"manual", "voice", "receipt"}}},
	"expense_parse_failed": {Params: map[string][]string{"reason": {"quota", "rate_limited", "unsupported_media", "too_large", "validation", "network", "internal"}}},
	"settle_up_opened":     {},
	"settle_up_done":       {},
	"paywall_shown":        {Params: map[string][]string{"from": {"quota", "account"}}},
	"paywall_dismissed":    {Params: map[string][]string{"from": {"quota", "account"}}},
	"purchase_started":     {Params: map[string][]string{"product": {"monthly", "yearly"}}},
	"purchase_completed":   {Params: map[string][]string{"product": {"monthly", "yearly"}}},
	"purchase_failed":      {Params: map[string][]string{"reason": {"cancelled", "store", "verify", "network"}}},
	"invite_sent":          {Params: map[string][]string{"channel": {"link", "share"}}},
}

// Validate проверяет имя события и его параметры по белому списку.
//
// Отсутствующий параметр разрешён: клиент старой сборки может не уметь его
// заполнять, и отбивать за это всё событие значило бы потерять и то, что он
// прислать умеет. А вот незнакомый ключ или значение вне множества — отказ:
// именно так в агрегаты попадает мусор.
func Validate(name string, params map[string]string) error {
	event, ok := Events[name]
	if !ok {
		return fmt.Errorf("неизвестное событие %q", name)
	}
	for key, value := range params {
		allowed, ok := event.Params[key]
		if !ok {
			return fmt.Errorf("у события %q нет параметра %q", name, key)
		}
		if !contains(allowed, value) {
			return fmt.Errorf("у %q.%q недопустимое значение %q", name, key, value)
		}
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
