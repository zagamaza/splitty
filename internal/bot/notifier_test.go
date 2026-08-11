package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
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

func (noopOperationService) SetNotificationSent(context.Context, string, primitive.ObjectID, []int) error {
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

func (s stubUserFinder) FindByIds(_ context.Context, ids []int) ([]api.User, error) {
	var out []api.User
	for _, id := range ids {
		if u, ok := s.users[id]; ok && u != nil {
			out = append(out, *u)
		}
	}
	return out, nil
}

// tgUser собирает КАНОНИЧЕСКОГО telegram-пользователя: номер Splitty и telegram id
// различаются намеренно — отправка и упоминания обязаны брать второй.
func tgUser(id, tgID int, name string) *api.User {
	return &api.User{ID: id, TelegramID: &tgID, DisplayName: name}
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
	// снимки (donor/recipient) телефонного id не несут — telegram_id живёт только
	// в канонических документах, которые отдаёт finder
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		2: tgUser(2, 1002, "Плательщик"),
		3: tgUser(3, 1003, "Гость"),
	}}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{}, finder, push.NoopSender{})

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
	// Канонический документ — telegram привязан, но уведомления об операциях выключены.
	tgID := 1003
	canonicalUser := &api.User{
		ID:          3,
		TelegramID:  &tgID,
		DisplayName: "Гость",
		Notify:      &api.NotifySettings{Operations: api.ChannelPrefs{Telegram: &off}},
	}

	tg := &captureSender{}
	n := NewNotifier(tg, noopOperationService{}, noopButtonService{},
		stubUserFinder{users: map[int]*api.User{3: canonicalUser}}, push.NoopSender{})

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

// captureOperationService запоминает список notification_sent, ушедший в базу.
type captureOperationService struct {
	OperationService
	sent []int
}

func (c *captureOperationService) UpdateOperation(context.Context, *api.Operation, string) error {
	return nil
}

func (c *captureOperationService) SetNotificationSent(_ context.Context, _ string, _ primitive.ObjectID, sent []int) error {
	c.sent = sent
	return nil
}

// TestNotifierRecordsAddresseesRegardlessOfDelivery — notification_sent это
// АДРЕСАТЫ события, а не отчёт о доставке, и гейты каналов его не сужают.
//
// Список — источник правды для счётчика непрочитанного (rest.notifiesUser), а
// раздел «Уведомления» это входящие в приложении, а не третий канал доставки.
// Записывай мы только доставленное — человек, отказавший приложению в push и не
// имеющий telegram (обычный вход через Google/Apple), выпадал бы из списка при
// НЕПУСТОМ списке остальных, то есть бейдж у него не поднялся бы никогда. Плюс
// правило разошлось бы с фоллбэком по долям, который никаких гейтов не знает:
// один и тот же расход считался бы непрочитанным или нет в зависимости от того,
// завели его в приложении или в боте.
func TestNotifierRecordsAddresseesRegardlessOfDelivery(t *testing.T) {
	loadLang(t)
	off := false
	silent := &api.User{ID: 3, DisplayName: "Гость", NotificationOn: &off}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		2: tgUser(2, 1002, "Плательщик"),
		3: silent, // ни telegram, ни push: уведомления выключены целиком
	}}
	tg := &captureSender{}
	ops := &captureOperationService{}
	n := NewNotifier(tg, ops, noopButtonService{}, finder, push.NoopSender{})

	author := api.User{ID: 1, DisplayName: "Автор"}
	donor := &api.User{ID: 2, DisplayName: "Плательщик"}
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: *silent, Sum: 50}},
	}

	n.NotifyOperationCreated(context.Background(), room(), op, author)

	var recorded bool
	for _, id := range ops.sent {
		if id == silent.ID {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("получатель с выключенными уведомлениями выпал из notification_sent (%v) — бейдж у него не поднимется никогда", ops.sent)
	}
	for _, text := range tg.texts() {
		if strings.Contains(text, "Гость") {
			t.Fatalf("человеку с выключенными уведомлениями ушло сообщение: %q", text)
		}
	}
}
