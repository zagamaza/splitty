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
	"expense_added":        {Params: map[string][]string{"method": {"manual", "voice", "receipt"}, "edited": {"true", "false"}}},
	"expense_parse_failed": {Params: map[string][]string{"kind": {"voice", "receipt"}, "reason": {"quota", "rate_limited", "unsupported_media", "too_large", "validation", "network", "internal"}}},
	"settle_up_opened":     {},
	"settle_up_done":       {},
	"paywall_shown":        {Params: map[string][]string{"from": {"quota", "account"}}},
	"paywall_dismissed":    {Params: map[string][]string{"from": {"quota", "account"}}},
	"purchase_started":     {Params: map[string][]string{"product": {"monthly", "yearly"}}},
	"purchase_completed":   {Params: map[string][]string{"product": {"monthly", "yearly"}}},
	"purchase_failed":      {Params: map[string][]string{"reason": {"cancelled", "store", "verify", "network"}}},
	"invite_sent":          {Params: map[string][]string{"channel": {"link", "share"}}},

	// Экраны. Список закрытый: свободное имя экрана превратило бы агрегат в
	// свалку, а переименование в коде молча развело бы один экран на два.
	"screen_view": {Params: map[string][]string{"screen": {
		"groups", "group", "group_settings", "balances", "add_expense", "operation",
		"settle_up", "account", "paywall", "invite", "archive", "activity", "friends", "welcome",
	}}},

	// Настройки и профиль.
	"settings_changed": {Params: map[string][]string{"what": {"theme", "language", "name", "avatar", "notifications"}}},
	"account_linked":   {Params: map[string][]string{"provider": {"google", "apple", "telegram", "password"}}},
	"account_unlinked": {Params: map[string][]string{"provider": {"google", "apple", "telegram", "password"}}},
	"logout":           {},

	// Люди в тусе.
	"member_added":      {Params: map[string][]string{"via": {"friends"}}},
	"member_add_failed": {Params: map[string][]string{"reason": {"not_found", "already_member", "forbidden", "network"}}},
	"member_removed":    {},
	"room_left":         {},

	// Жизнь тусы.
	"room_archived":         {},
	"room_unarchived":       {},
	"room_settings_changed": {Params: map[string][]string{"what": {"name", "avatar", "currency"}}},

	// Путь распознавания. Между «начал вводить» и «добавил расход» лежит всё
	// самое интересное: где обрывается и что потом переделывают руками.
	"capture_started":          {Params: map[string][]string{"kind": {"voice", "camera", "gallery"}}},
	"capture_cancelled":        {Params: map[string][]string{"kind": {"voice", "camera", "gallery"}}},
	"parse_started":            {Params: map[string][]string{"kind": {"voice", "receipt"}}},
	"parse_succeeded":          {Params: map[string][]string{"kind": {"voice", "receipt"}, "items": {"none", "few", "many", "lots"}}},
	"parse_retried":            {Params: map[string][]string{"kind": {"voice", "receipt"}}},
	"receipt_item_edited":      {},
	"receipt_unknown_resolved": {},
}

// ItemsBucket — сколько позиций распозналось, бакетом.
//
// Точное число в параметр не кладём: оно не группируется в агрегате, а читать
// «в чеке было 12» всё равно некому. Живёт здесь, а не на клиентах: два
// клиента поделили бы диапазоны по-своему, и один и тот же чек попал бы в
// разные корзины.
func ItemsBucket(n int) string {
	switch {
	case n <= 0:
		return "none"
	case n <= 3:
		return "few"
	case n <= 10:
		return "many"
	default:
		return "lots"
	}
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
