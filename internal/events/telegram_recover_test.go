package events

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

// Одно кривое сообщение не должно уносить сервис.
//
// Бот и REST живут в ОДНОМ процессе: паника в обработке чужого сообщения
// закрывала приложение всем. Причин хватало — пост в канале приходит без
// отправителя, у некоторых апдейтов нет чата, а любой дефект экрана бота
// всплывал наружу как паника.

// panicBot — экран, который всегда паникует.
type panicBot struct{ calls int }

func (b *panicBot) OnMessage(_ context.Context, _ *api.Update) api.TelegramMessage {
	b.calls++
	panic("что-то пошло не так внутри экрана")
}

func (b *panicBot) HasReact(_ *api.Update) bool { return true }

// countingUserService считает вызовы и отдаёт канонического пользователя.
type countingUserService struct{ calls int }

func (s *countingUserService) UpsertUser(_ context.Context, u api.User) (*api.User, error) {
	return &u, nil
}

func (s *countingUserService) UpsertTelegramUser(_ context.Context, telegramID int, username, displayName, lang string) (*api.User, error) {
	s.calls++
	return &api.User{ID: telegramID, Username: username, DisplayName: displayName, UserLang: lang}, nil
}

// TestHandleUpdateSurvivesPanic — паника внутри экрана не выходит наружу, и
// следующее сообщение обрабатывается как ни в чём не бывало.
func TestHandleUpdateSurvivesPanic(t *testing.T) {
	bots := &panicBot{}
	users := &countingUserService{}
	l := &TelegramListener{
		Bots:             bots,
		UserService:      users,
		ChatStateService: &stubChatStateService{states: map[int]*api.ChatState{}},
	}

	update := tbapi.Update{Message: &tbapi.Message{
		MessageID: 1,
		From:      &tbapi.User{ID: 42, UserName: "zagir", FirstName: "Загир"},
		Chat:      &tbapi.Chat{ID: 42, Type: "private"},
		Text:      "привет",
	}}

	l.handleUpdate(context.Background(), update)
	l.handleUpdate(context.Background(), update)

	if bots.calls != 2 {
		t.Fatalf("экран вызван %d раз(а) — паника прервала обработку следующих сообщений", bots.calls)
	}
}

// TestHandleUpdateWithoutSender — пост в канале приходит без отправителя.
// Обработчик обязан отказаться от такого обновления, а не разыменовать nil.
func TestHandleUpdateWithoutSender(t *testing.T) {
	bots := &panicBot{}
	users := &countingUserService{}
	l := &TelegramListener{
		Bots:             bots,
		UserService:      users,
		ChatStateService: &stubChatStateService{states: map[int]*api.ChatState{}},
	}

	update := tbapi.Update{Message: &tbapi.Message{
		MessageID: 1,
		Chat:      &tbapi.Chat{ID: -100, Type: "channel"},
		Text:      "пост в канале",
	}}

	l.handleUpdate(context.Background(), update)

	if users.calls != 0 {
		t.Fatalf("пользователь без отправителя всё-таки записан (%d вызовов)", users.calls)
	}
	if bots.calls != 0 {
		t.Fatalf("обновление без отправителя дошло до экрана (%d вызовов)", bots.calls)
	}
}

// TestTransformUserHandlesNil — сам перенос отправителя обязан переживать nil:
// на нём падали и callback-запросы, и inline-запросы.
func TestTransformUserHandlesNil(t *testing.T) {
	if got := transformUser(nil); got.ID != 0 || got.Username != "" {
		t.Fatalf("пустой отправитель дал %+v", got)
	}
}

// TestTransformMessageWithoutChat — апдейт без чата не должен ронять перенос.
func TestTransformMessageWithoutChat(t *testing.T) {
	msg := transform(&tbapi.Message{MessageID: 7, Text: "без чата"})
	if msg == nil {
		t.Fatal("сообщение потерялось")
	}
	if msg.Chat != nil {
		t.Fatalf("чат взялся из ниоткуда: %+v", msg.Chat)
	}
}
