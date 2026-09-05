package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
)

// captureLocalized запоминает язык записи вместе с текстом: без языка тест не
// отличил бы «ушло два разных текста» от «ушло два одинаковых на один язык».
type captureLocalized struct {
	locales []string
	notifs  []push.Notification
}

func (c *captureLocalized) SendToUser(_ context.Context, _ api.User, locale string, n push.Notification) {
	c.locales = append(c.locales, locale)
	c.notifs = append(c.notifs, n)
}

// TestPushSplitsByDeviceLanguage — у человека русский телефон и английский
// планшет. Очередь кладёт текст на пользователя, а не на токен, поэтому без
// разбиения по языкам одно из устройств получило бы чужой язык.
func TestPushSplitsByDeviceLanguage(t *testing.T) {
	loadLang(t)
	pushes := &captureLocalized{}
	invitee := &api.User{
		ID: 3, DisplayName: "Гость",
		PushTokens: []api.PushToken{
			{Token: "phone", Platform: "ios", Locale: "ru"},
			{Token: "tablet", Platform: "ios", Locale: "en"},
		},
	}
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	n.NotifyInvited(context.Background(), room(), *invitee, api.User{ID: 1, DisplayName: "Автор"}, false)

	if len(pushes.notifs) != 2 {
		t.Fatalf("ожидалось два push (ru и en), отправлено %d", len(pushes.notifs))
	}
	byLocale := map[string]string{}
	for i, loc := range pushes.locales {
		byLocale[loc] = pushes.notifs[i].Body
	}
	if _, ok := byLocale["ru"]; !ok {
		t.Fatalf("нет записи на ru, языки: %v", pushes.locales)
	}
	if _, ok := byLocale["en"]; !ok {
		t.Fatalf("нет записи на en, языки: %v", pushes.locales)
	}
	if byLocale["ru"] == byLocale["en"] {
		t.Fatalf("тексты на ru и en совпали: %q", byLocale["ru"])
	}
	if !strings.Contains(byLocale["en"], "added you to a group") {
		t.Errorf("английский текст не английский: %q", byLocale["en"])
	}
}

// TestPushDedupesSameLanguage — два русских устройства это ОДНА запись очереди.
// Иначе человек с телефоном и планшетом получал бы по два одинаковых пуша.
func TestPushDedupesSameLanguage(t *testing.T) {
	loadLang(t)
	pushes := &captureLocalized{}
	invitee := &api.User{
		ID: 3, DisplayName: "Гость",
		PushTokens: []api.PushToken{
			{Token: "phone", Locale: "ru"},
			{Token: "tablet", Locale: "ru"},
		},
	}
	finder := stubUserFinder{users: map[int]*api.User{1: tgUser(1, 1001, "Автор"), 3: invitee}}
	n := NewNotifier(&captureSender{}, noopOperationService{}, noopButtonService{}, finder, pushes)

	n.NotifyInvited(context.Background(), room(), *invitee, api.User{ID: 1, DisplayName: "Автор"}, false)

	if len(pushes.notifs) != 1 {
		t.Fatalf("одинаковый язык у двух токенов должен давать одну запись, отправлено %d", len(pushes.notifs))
	}
}
