package api

import "testing"

// Категория «правки» — единственная, у которой дефолты каналов расходятся:
// telegram слал переименования с самого начала и остаётся включённым, а push
// добавлен позже и по умолчанию молчит. Без этого теста любое упрощение
// notifyDefaults до общего «включено» прошло бы незамеченным и завалило бы
// людей пушами на каждое переименование.
func TestOperationEditsDefaults(t *testing.T) {
	u := &User{ID: 1}

	if !u.AllowsTelegram(NotifyOperationEdits) {
		t.Error("telegram у правок должен быть включён по умолчанию — так работало до появления категории")
	}
	if u.WantsPush(NotifyOperationEdits) {
		t.Error("push у правок должен быть выключен по умолчанию")
	}
	// Остальные категории не задеты
	for _, c := range []NotifyCategory{NotifyOperations, NotifyDebts, NotifyInvites} {
		if !u.WantsPush(c) {
			t.Errorf("push категории %q должен остаться включённым по умолчанию", c)
		}
	}
}

// Явное включение перебивает дефолт, мастер-выключатель перебивает всё.
func TestOperationEditsExplicitAndMaster(t *testing.T) {
	on, off := true, false

	u := &User{ID: 1, Notify: &NotifySettings{Edits: ChannelPrefs{Push: &on}}}
	if !u.WantsPush(NotifyOperationEdits) {
		t.Error("явно включённый push правок должен работать")
	}

	u = &User{ID: 1, Notify: &NotifySettings{Edits: ChannelPrefs{Telegram: &off}}}
	if u.AllowsTelegram(NotifyOperationEdits) {
		t.Error("явно выключенный telegram правок должен молчать")
	}
	if !u.AllowsTelegram(NotifyOperations) {
		t.Error("выключение правок не должно задевать операции")
	}

	u = &User{ID: 1, NotificationOn: &off, Notify: &NotifySettings{Edits: ChannelPrefs{Push: &on}}}
	if u.WantsPush(NotifyOperationEdits) {
		t.Error("мастер-выключатель обязан гасить и явно включённый push правок")
	}
}
