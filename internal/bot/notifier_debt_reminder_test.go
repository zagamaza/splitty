package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/push"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Напоминание о долге в telegram — запасной канал рассылки: пуш умеет только
// приложение, а девять должников из десяти живут в боте.

// failingSender — telegram не принял сообщение.
type failingSender struct{ captureSender }

func (f *failingSender) Send(tgbotapi.Chattable) (tgbotapi.Message, error) {
	return tgbotapi.Message{}, errUnavailable
}

var errUnavailable = &tgError{}

type tgError struct{}

func (*tgError) Error() string { return "telegram недоступен" }

func TestSendDebtReminderGoesToUserChat(t *testing.T) {
	loadLang(t)
	sender := &captureSender{}
	user := tgUser(7, 900700, "Артур")
	n := NewNotifier(sender, noopOperationService{}, noopButtonService{}, stubUserFinder{users: map[int]*api.User{}}, push.NoopSender{})

	roomId := primitive.NewObjectID().Hex()
	if err := n.SendDebtReminder(context.Background(), user, "Вы должны 1 707 ₽ в «Ночь»", roomId); err != nil {
		t.Fatalf("отправка: %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("отправлено сообщений: %d", len(sender.sent))
	}
	msg, ok := sender.sent[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("отправлено не текстовое сообщение: %T", sender.sent[0])
	}
	// Уходит в личный чат человека, а не в комнату
	if msg.ChatID != int64(*user.TelegramID) {
		t.Errorf("chat id = %d, ожидался %d", msg.ChatID, *user.TelegramID)
	}
	if !strings.Contains(msg.Text, "1 707") {
		t.Errorf("сумма потерялась: %q", msg.Text)
	}
	// Кнопка ведёт в комнату — туда же, куда уводит тап по пушу
	if msg.ReplyMarkup == nil {
		t.Error("сообщение без кнопки «открыть группу»")
	}
}

// Текст собирает джоб, в нём название группы, которое писал человек. Сообщения
// бота уходят с ParseMode=HTML, поэтому «<» обязан быть экранирован — иначе
// telegram отвергнет сообщение целиком.
func TestSendDebtReminderEscapesText(t *testing.T) {
	loadLang(t)
	sender := &captureSender{}
	n := NewNotifier(sender, noopOperationService{}, noopButtonService{}, stubUserFinder{users: map[int]*api.User{}}, push.NoopSender{})

	err := n.SendDebtReminder(context.Background(), tgUser(7, 900700, "Артур"),
		`Вы должны 100 ₽ в «<b>Дача</b>»`, "")
	if err != nil {
		t.Fatalf("отправка: %v", err)
	}

	msg := sender.sent[0].(tgbotapi.MessageConfig)
	if strings.Contains(msg.Text, "<b>") {
		t.Errorf("разметка из названия группы уехала как есть: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "&lt;b&gt;") {
		t.Errorf("текст не экранирован: %q", msg.Text)
	}
}

// Человеку без telegram слать некуда, и это ошибка, а не тихий успех: джоб
// иначе списал бы ему попытку за неотправленное сообщение.
func TestSendDebtReminderFailsWithoutTelegram(t *testing.T) {
	loadLang(t)
	sender := &captureSender{}
	n := NewNotifier(sender, noopOperationService{}, noopButtonService{}, stubUserFinder{users: map[int]*api.User{}}, push.NoopSender{})

	if err := n.SendDebtReminder(context.Background(), &api.User{ID: 7, DisplayName: "Гость"}, "текст", ""); err == nil {
		t.Error("отправка человеку без telegram прошла успешно")
	}
	if len(sender.sent) != 0 {
		t.Errorf("что-то всё-таки отправили: %d", len(sender.sent))
	}
}

// Telegram отверг сообщение — ошибка обязана дойти до джоба, иначе он спишет
// попытку за недоставленное напоминание.
func TestSendDebtReminderReturnsSendError(t *testing.T) {
	loadLang(t)
	n := NewNotifier(&failingSender{}, noopOperationService{}, noopButtonService{}, stubUserFinder{users: map[int]*api.User{}}, push.NoopSender{})

	if err := n.SendDebtReminder(context.Background(), tgUser(7, 900700, "Артур"), "текст", ""); err == nil {
		t.Error("сбой telegram проглочен")
	}
}
