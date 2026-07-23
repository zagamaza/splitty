package api

import "testing"

func boolp(b bool) *bool { return &b }

// Мастер-тумблер (NotificationOn) — глобальный kill-switch: выключен, значит ни
// telegram, ни push не шлём ни по одной категории, даже если per-category
// настройки явно включены.
func TestNotificationOnKillSwitch(t *testing.T) {
	off := &User{
		NotificationOn: boolp(false),
		Notify: &NotifySettings{
			Operations: ChannelPrefs{Telegram: boolp(true), Push: boolp(true)},
			Debts:      ChannelPrefs{Telegram: boolp(true), Push: boolp(true)},
		},
	}
	for _, c := range []NotifyCategory{NotifyOperations, NotifyDebts} {
		if off.AllowsTelegram(c) {
			t.Fatalf("мастер выключен — AllowsTelegram(%s) должен быть false", c)
		}
		if off.WantsPush(c) {
			t.Fatalf("мастер выключен — WantsPush(%s) должен быть false", c)
		}
	}
}

// Мастер включён — действуют per-category × per-channel настройки.
func TestNotificationOnAllowsGranular(t *testing.T) {
	on := &User{
		NotificationOn: boolp(true),
		Notify: &NotifySettings{
			Operations: ChannelPrefs{Telegram: boolp(true), Push: boolp(true)},
			Debts:      ChannelPrefs{Telegram: boolp(false), Push: boolp(false)},
		},
	}
	if !on.AllowsTelegram(NotifyOperations) || !on.WantsPush(NotifyOperations) {
		t.Fatal("operations включены по обоим каналам")
	}
	if on.AllowsTelegram(NotifyDebts) || on.WantsPush(NotifyDebts) {
		t.Fatal("debts выключены по обоим каналам")
	}
}

// Дефолт (мастер не задан → включён, тонких настроек нет): telegram включён для
// обеих категорий, push выключен (нужно явное включение).
func TestNotificationDefaults(t *testing.T) {
	u := &User{}
	if !u.AllowsTelegram(NotifyOperations) || !u.AllowsTelegram(NotifyDebts) {
		t.Fatal("telegram по умолчанию включён для обеих категорий")
	}
	if u.WantsPush(NotifyOperations) || u.WantsPush(NotifyDebts) {
		t.Fatal("push по умолчанию выключен")
	}
}
