package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/gookit/i18n"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// loadLang поднимает реальные шаблоны из conf/lang: без них I18n возвращает
// голый ключ, и проверить подстановку экранированного текста невозможно.
func loadLang(t *testing.T) {
	t.Helper()
	i18n.Init("../../conf/lang", "ru", map[string]string{"en": "English", "ru": "Русский"})
}

// --- заглушки зависимостей Notifier ---

type captureSender struct{ sent []tgbotapi.Chattable }

func (c *captureSender) Send(m tgbotapi.Chattable) (tgbotapi.Message, error) {
	c.sent = append(c.sent, m)
	return tgbotapi.Message{}, nil
}

// texts возвращает тексты отправленных сообщений (только MessageConfig).
func (c *captureSender) texts() []string {
	var out []string
	for _, m := range c.sent {
		if msg, ok := m.(tgbotapi.MessageConfig); ok {
			out = append(out, msg.Text)
		}
	}
	return out
}

type noopOperationService struct{ OperationService }

func (noopOperationService) UpdateOperation(context.Context, *api.Operation, string) error {
	return nil
}

type noopButtonService struct{}

func (noopButtonService) Save(context.Context, *api.Button) (primitive.ObjectID, error) {
	return primitive.NewObjectID(), nil
}

func (noopButtonService) SaveAll(_ context.Context, b ...*api.Button) ([]*api.Button, error) {
	return b, nil
}

// stubUserFinder отдаёт канонические документы по id.
type stubUserFinder struct{ users map[int]*api.User }

func (s stubUserFinder) FindById(_ context.Context, id int) (*api.User, error) {
	return s.users[id], nil
}

func room() api.Room {
	return api.Room{ID: primitive.NewObjectID(), Name: "Тусa", Currency: "RUB"}
}

// TestNotifierEscapesUserInput: описание операции и название комнаты — сырой ввод,
// а сообщения уходят с ParseMode=HTML. Без экранирования "a < b" даёт 400 от
// Telegram (уведомление теряется), а "<b>" вставляет разметку в чужие ЛС.
func TestNotifierEscapesUserInput(t *testing.T) {
	loadLang(t)
	tg := &captureSender{}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, stubUserFinder{})

	r := room()
	r.Name = "Бар <b>X</b>"
	author := api.User{ID: 1, DisplayName: "Автор"}
	donor := &api.User{ID: 2, DisplayName: "Плательщик"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца a < b",
		Sum:               100,
		Donor:             donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), r, op, author)

	texts := tg.texts()
	if len(texts) == 0 {
		t.Fatal("ожидались уведомления, не отправлено ни одного")
	}
	for _, text := range texts {
		if strings.Contains(text, "a < b") {
			t.Errorf("описание не экранировано, Telegram вернёт 400: %q", text)
		}
		if strings.Contains(text, "Бар <b>X</b>") {
			t.Errorf("название комнаты не экранировано — HTML-инъекция: %q", text)
		}
		if !strings.Contains(text, "a &lt; b") {
			t.Errorf("ожидалось экранированное описание в %q", text)
		}
	}
}

// TestNotifierUsesCanonicalNotifyPrefs: получатели приходят из встроенных в
// комнату снимков, которые не обновляются при PATCH /me/notifications. Решение
// «слать или нет» обязано читаться из канонического документа пользователя,
// иначе выключенные в приложении уведомления продолжают приходить.
func TestNotifierUsesCanonicalNotifyPrefs(t *testing.T) {
	off := false
	// Снимок в комнате — «уведомления не настроены» (легаси-ветка = слать).
	stale := api.User{ID: 3, DisplayName: "Гость"}
	// Канонический документ — пользователь выключил уведомления об операциях.
	canonical := &api.User{
		ID:          3,
		DisplayName: "Гость",
		Notify:      &api.NotifySettings{Operations: api.ChannelPrefs{Telegram: &off}},
	}

	tg := &captureSender{}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{},
		stubUserFinder{users: map[int]*api.User{3: canonical}})

	author := api.User{ID: 1, DisplayName: "Автор"}
	op := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "пицца",
		Sum:               100,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: stale, Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	if len(tg.sent) != 0 {
		t.Fatalf("уведомления выключены в профиле, но отправлено %d сообщений", len(tg.sent))
	}
}
