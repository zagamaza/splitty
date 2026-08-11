package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
)

// Тесты NotifyInvited.
//
// Ключи payload («roomId», «invites», «invite») закреплены здесь на СЕРВЕРНОЙ
// стороне. Клиентские тесты (PushRouteTests.swift, PushChannelRoutingTest.kt)
// сверяются с теми же литералами, но между ними и этим кодом нет ничего общего:
// без такого теста переименование ключа обратно в snake_case прошло бы с тремя
// зелёными наборами тестов — ровно тот баг, ради которого писался план.

// capturePayload запоминает уведомление целиком, а не только адресата.
type capturePayload struct {
	users  []int
	notifs []push.Notification
}

func (c *capturePayload) SendToUser(_ context.Context, user api.User, n push.Notification) {
	c.users = append(c.users, user.ID)
	c.notifs = append(c.notifs, n)
}

func inviteUser(id int, prefs *api.NotifySettings, master *bool) *api.User {
	return &api.User{
		ID: id, DisplayName: "Гость",
		PushTokens:     []api.PushToken{{Token: "t"}},
		Notify:         prefs,
		NotificationOn: master,
	}
}

func TestNotifyInvitedPushPayload(t *testing.T) {
	loadLang(t)
	pushes := &capturePayload{}
	invitee := inviteUser(3, nil, nil)
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	r := room()
	n.NotifyInvited(context.Background(), r, *invitee, api.User{ID: 1, DisplayName: "Автор"}, false)

	if len(pushes.notifs) != 1 {
		t.Fatalf("ожидался один push, отправлено %d", len(pushes.notifs))
	}
	data := pushes.notifs[0].Data
	// Ключи проверяются поимённо: клиенты читают именно эти строки.
	if data["channel"] != "invites" {
		t.Errorf("channel = %q, ожидался invites (без него Android 8+ не покажет фоновый пуш)", data["channel"])
	}
	if data["type"] != "invite" {
		t.Errorf("type = %q, ожидался invite (по нему iOS открывает раздел, а не комнату)", data["type"])
	}
	if data["roomId"] != r.ID.Hex() {
		t.Errorf("roomId = %q, ожидался %q (camelCase, как у остальных пушей)", data["roomId"], r.ID.Hex())
	}
	if _, ok := data["room_id"]; ok {
		t.Error("в payload появился snake_case room_id — клиенты читают roomId")
	}
}

// TestNotifyInvitedTextDiffersForReturn — «вас добавили» и «приглашает
// вернуться» это разные события: во втором случае от человека ЖДУТ решения, и
// одинаковый текст этого не объяснит.
func TestNotifyInvitedTextDiffersForReturn(t *testing.T) {
	loadLang(t)
	pushes := &capturePayload{}
	invitee := inviteUser(3, nil, nil)
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	inviter := api.User{ID: 1, DisplayName: "Автор"}
	n.NotifyInvited(context.Background(), room(), *invitee, inviter, false)
	n.NotifyInvited(context.Background(), room(), *invitee, inviter, true)

	if len(pushes.notifs) != 2 {
		t.Fatalf("ожидалось два push, отправлено %d", len(pushes.notifs))
	}
	if pushes.notifs[0].Body == pushes.notifs[1].Body {
		t.Fatalf("тексты добавления и возврата совпали: %q", pushes.notifs[0].Body)
	}
	if !strings.Contains(pushes.notifs[1].Body, "вернуться") {
		t.Errorf("текст возврата не объясняет, чего ждут: %q", pushes.notifs[1].Body)
	}
}

// TestNotifyInvitedRespectsCategoryAndMaster — выключатели обязаны работать.
// Передай сюда NotifyDebts вместо NotifyInvites — и категория «Приглашения»
// молча перестала бы что-либо выключать.
func TestNotifyInvitedRespectsCategoryAndMaster(t *testing.T) {
	loadLang(t)
	off := false

	tests := []struct {
		name   string
		prefs  *api.NotifySettings
		master *bool
	}{
		{
			name:  "выключена категория invites",
			prefs: &api.NotifySettings{Invites: api.ChannelPrefs{Push: &off}},
		},
		{
			name:   "выключен мастер-выключатель",
			master: &off,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pushes := &capturePayload{}
			invitee := inviteUser(3, tt.prefs, tt.master)
			finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
			n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

			n.NotifyInvited(context.Background(), room(), *invitee, api.User{ID: 1, DisplayName: "Автор"}, false)

			if len(pushes.notifs) != 0 {
				t.Fatalf("push ушёл вопреки выключателю: %+v", pushes.notifs)
			}
		})
	}
}

// TestNotifyInvitedTelegramIsLocalized — telegram-сообщение идёт через I18n, как
// все остальные сообщения бота: у англоязычного человека там не должно быть
// русского текста.
func TestNotifyInvitedTelegramIsLocalized(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}
	invitee := tgUser(3, 1003, "Guest")
	invitee.SelectedLang = "en"
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, push.NoopSender{})

	n.NotifyInvited(context.Background(), room(), *invitee, api.User{ID: 1, DisplayName: "Author"}, false)

	texts := tg.texts()
	if len(texts) != 1 {
		t.Fatalf("ожидалось одно telegram-сообщение, отправлено %d", len(texts))
	}
	if !strings.Contains(texts[0], "added you to the party") {
		t.Fatalf("текст не локализован: %q", texts[0])
	}
}
