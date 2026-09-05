package bot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// capturePush запоминает, кому ушёл native-пуш: push-канал обязан работать
// независимо от telegram, у google/apple-пользователя telegram нет вовсе.
type capturePush struct{ users []int }

func (c *capturePush) SendToUser(_ context.Context, user api.User, _ string, _ push.Notification) {
	c.users = append(c.users, user.ID)
}

// snapshot имитирует встроенный в комнату снимок: полей личности в нём нет
// никогда — api.User.Snapshot() обнуляет telegram_id, а снимки, записанные до
// этого плана, его и не содержали.
func snapshot(id int, name string) api.User {
	return api.User{ID: id, DisplayName: name}
}

// TestNotifyOperationCreated_NoTelegramGetsPushOnly: у пользователя нет привязки
// к telegram (вошёл через Google/Apple) — telegram-канал пропускается молча, а
// push уходит. Ошибка здесь означала бы либо падение на несуществующем chat_id,
// либо потерю единственного доступного человеку канала.
func TestNotifyOperationCreated_NoTelegramGetsPushOnly(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}
	pushes := &capturePush{}

	// у получателя (3) telegram нет; у автора (1) и донора (2) — есть
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		2: tgUser(2, 1002, "Плательщик"),
		3: {ID: 3, DisplayName: "Гость", PushTokens: []api.PushToken{{Token: "t"}}},
	}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, pushes)

	author := api.User{ID: 1, DisplayName: "Автор"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: snapshot(3, "Гость"), Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	if len(tg.sent) != 0 {
		t.Fatalf("у получателя нет telegram, но отправлено %d telegram-сообщений", len(tg.sent))
	}
	if !slices.Contains(pushes.users, 3) {
		t.Fatalf("push обязан уйти пользователю без telegram, ушло: %v", pushes.users)
	}
}

// TestNotifyOperationCreated_WithTelegramGetsBoth: у пользователя telegram есть —
// получает и push, и сообщение в telegram, причём chat id взят из КАНОНИЧЕСКОГО
// документа (telegram id), а не из номера Splitty в снимке.
func TestNotifyOperationCreated_WithTelegramGetsBoth(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}
	pushes := &capturePush{}

	guest := tgUser(3, 1003, "Гость")
	guest.PushTokens = []api.PushToken{{Token: "t"}}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		3: guest,
	}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, pushes)

	author := api.User{ID: 1, DisplayName: "Автор"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: snapshot(3, "Гость"), Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	if len(tg.sent) != 1 {
		t.Fatalf("ожидалось одно telegram-сообщение, отправлено %d", len(tg.sent))
	}
	if !slices.Contains(pushes.users, 3) {
		t.Fatalf("push тоже обязан уйти, ушло: %v", pushes.users)
	}
	if got := chatIDOf(t, tg); got != 1003 {
		t.Fatalf("chat id взят не из канонического документа: %d, ожидался telegram id 1003", got)
	}
}

// TestNotifyOperationCreated_ChatIDFromCanonicalNotSnapshot: снимок несёт номер
// Splitty (10¹²+), telegram id живёт только в каноническом документе. Если брать
// chat id из снимка, Telegram получит несуществующий chat_id и уведомления
// перестанут доходить до ВСЕХ существующих пользователей.
func TestNotifyOperationCreated_ChatIDFromCanonicalNotSnapshot(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}

	const splittyID = 1_000_000_000_042
	const telegramID = 777
	finder := stubUserFinder{users: map[int]*api.User{
		1:         tgUser(1, 1001, "Автор"),
		splittyID: tgUser(splittyID, telegramID, "Гость"),
	}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, push.NoopSender{})

	author := api.User{ID: 1, DisplayName: "Автор"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: snapshot(splittyID, "Гость"), Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	if got := chatIDOf(t, tg); got != telegramID {
		t.Fatalf("chat id = %d, ожидался telegram id %d (а не номер Splitty %d)", got, telegramID, splittyID)
	}
}

// TestNotifyOperationCreated_MentionsStayClickableForSnapshotUser — ОБЯЗАТЕЛЬНАЯ
// антирегрессия. Пользователь пришёл из СНИМКА комнаты (там telegram_id нет), но
// telegram у него привязан: упоминание обязано остаться кликабельным. Без этого
// теста «зелёный» прогон означал бы успешно сломанные упоминания у всех живых
// telegram-пользователей.
func TestNotifyOperationCreated_MentionsStayClickableForSnapshotUser(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}

	const splittyID = 1_000_000_000_042
	const telegramID = 777
	finder := stubUserFinder{users: map[int]*api.User{
		1:         tgUser(1, 1001, "Автор"),
		splittyID: tgUser(splittyID, telegramID, "Гость"),
	}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, push.NoopSender{})

	author := api.User{ID: 1, DisplayName: "Автор"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: snapshot(splittyID, "Гость"), Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	texts := tg.texts()
	if len(texts) == 0 {
		t.Fatal("ожидалось уведомление, не отправлено ни одного")
	}
	want := fmt.Sprintf("tg://user?id=%d", telegramID)
	if !strings.Contains(texts[0], want) {
		t.Fatalf("упоминание потеряло ссылку %s: %q", want, texts[0])
	}
	if strings.Contains(texts[0], fmt.Sprintf("tg://user?id=%d", splittyID)) {
		t.Fatalf("в ссылке номер Splitty вместо telegram id: %q", texts[0])
	}
}

// TestUserLink_WithoutTelegramHasNoHref: у google/apple-пользователя ссылки нет,
// остаётся экранированное имя — иначе Telegram отрисует ссылку на чужой аккаунт.
func TestUserLink_WithoutTelegramHasNoHref(t *testing.T) {
	got := userLink(&api.User{ID: 1_000_000_000_007, DisplayName: `Гость <b>`})
	if strings.Contains(got, "href") || strings.Contains(got, "tg://user") {
		t.Fatalf("у пользователя без telegram не должно быть ссылки: %q", got)
	}
	if got != "Гость &lt;b&gt;" {
		t.Fatalf("ожидалось экранированное имя без разметки, получено %q", got)
	}
}

// TestTelegramChatID: чистая логика хелпера — nil, нулевое и валидное значение.
func TestTelegramChatID(t *testing.T) {
	zero := 0
	valid := 555
	tests := []struct {
		name   string
		user   *api.User
		wantID int64
		wantOk bool
	}{
		{"nil пользователь", nil, 0, false},
		{"нет telegram_id", &api.User{ID: 7}, 0, false},
		{"telegram_id = 0", &api.User{ID: 7, TelegramID: &zero}, 0, false},
		{"валидный telegram_id", &api.User{ID: 7, TelegramID: &valid}, 555, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := telegramChatID(tt.user)
			if id != tt.wantID || ok != tt.wantOk {
				t.Fatalf("telegramChatID = (%d, %v), ожидалось (%d, %v)", id, ok, tt.wantID, tt.wantOk)
			}
		})
	}
}

// TestCanonicalUsers_FallsBackToSnapshot: резолвер недоступен или пользователь не
// найден — имя в тексте важнее ссылки, поэтому падаем на снимок и не молчим.
func TestCanonicalUsers_FallsBackToSnapshot(t *testing.T) {
	snap := snapshot(5, "Гость")
	if got := canonical(context.Background(), nil).link(&snap); got != "Гость" {
		t.Fatalf("без резолвера ожидалось имя из снимка, получено %q", got)
	}
	empty := stubUserFinder{users: map[int]*api.User{}}
	if got := canonical(context.Background(), empty).link(&snap); got != "Гость" {
		t.Fatalf("пользователь не найден — ожидалось имя из снимка, получено %q", got)
	}
}

// chatIDOf достаёт chat id первого отправленного сообщения.
func chatIDOf(t *testing.T, c *captureSender) int64 {
	t.Helper()
	if len(c.sent) == 0 {
		t.Fatal("сообщений не отправлено")
	}
	msg, ok := c.sent[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("ожидался MessageConfig, получен %T", c.sent[0])
	}
	return msg.ChatID
}
