package bot

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Найдено на ревью: тумблер «Правки расходов» работал наполовину. Бот-путь
// (переименование операции из бота) читал настройки из ВСТРОЕННОГО СНИМКА
// участника, где Notify всегда nil, поэтому канонический edits.telegram=false
// там не действовал вовсе и переименование уходило всё равно.
//
// Остальные категории намеренно остались на снимке — их перевод меняет давнее
// поведение бота и идёт отдельной задачей.
func TestBotPathHonoursCanonicalEditsSetting(t *testing.T) {
	loadLang(t)
	off := false

	// Канонический документ: telegram по правкам ВЫКЛЮЧЕН. В снимке этого нет.
	canonicalRecipient := tgUser(3, 1003, "Гость")
	canonicalRecipient.Notify = &api.NotifySettings{Edits: api.ChannelPrefs{Telegram: &off}}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		3: canonicalRecipient,
	}}
	cu := canonical(context.Background(), finder)

	r := room()
	// Снимки — без Notify и без telegram_id, как они и лежат в комнате.
	old := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "Чай",
		Sum:               200,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 100}},
	}
	upd := old
	upd.Description = "Чай (красный)"

	editor := &api.Update{User: tgUser(1, 1001, "Автор")}
	_, messages := notificationWhenUpdateOperation(cu, editor, old, upd, &r, nil, nil)

	for _, m := range messages {
		if msg, ok := m.(tgbotapi.MessageConfig); ok && msg.ChatID == 1003 {
			t.Fatalf("переименование ушло тому, кто выключил категорию «правки»: %q", msg.Text)
		}
	}
}

// Обратная сторона: с включённой категорией сообщение из бота приходит.
func TestBotPathSendsEditsWhenAllowed(t *testing.T) {
	loadLang(t)
	on := true

	canonicalRecipient := tgUser(3, 1003, "Гость")
	canonicalRecipient.Notify = &api.NotifySettings{Edits: api.ChannelPrefs{Telegram: &on}}
	finder := stubUserFinder{users: map[int]*api.User{
		1: tgUser(1, 1001, "Автор"),
		3: canonicalRecipient,
	}}
	cu := canonical(context.Background(), finder)

	r := room()
	old := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "Чай",
		Sum:               200,
		Donor:             &api.User{ID: 1, DisplayName: "Автор"},
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 3, DisplayName: "Гость"}, Sum: 100}},
	}
	upd := old
	upd.Description = "Чай (красный)"

	editor := &api.Update{User: tgUser(1, 1001, "Автор")}
	_, messages := notificationWhenUpdateOperation(cu, editor, old, upd, &r, nil, nil)

	for _, m := range messages {
		if msg, ok := m.(tgbotapi.MessageConfig); ok && msg.ChatID == 1003 {
			return
		}
	}
	t.Fatal("переименование не дошло до получателя с включённой категорией")
}
