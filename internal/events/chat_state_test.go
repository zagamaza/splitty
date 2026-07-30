package events

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// stubChatStateService — состояния по ключу user_id и журнал запрошенных ключей.
// Журнал важен: без него нельзя отличить «нашли по каноническому» от «нашли по
// сырому telegram id», а именно это и ломается у аккаунтов с синтетическим _id.
type stubChatStateService struct {
	states  map[int]*api.ChatState
	queried []int
}

func (s *stubChatStateService) FindByUserId(_ context.Context, userId int) (*api.ChatState, error) {
	s.queried = append(s.queried, userId)
	return s.states[userId], nil
}

// telegram id порядка 10^9 и номер Splitty ≥ 10^12 — ровно тот случай, ради
// которого задача существует: google-первый аккаунт, привязавший telegram
const (
	testRawTelegramID = 1_234_567_890
	testCanonicalID   = 1_000_000_000_123
)

func updateWithMessage(rawTelegramID, canonicalID int) *api.Update {
	return &api.Update{
		Message: &api.Message{From: api.User{ID: rawTelegramID}, Chat: &api.Chat{ID: int64(rawTelegramID), Type: "private"}},
		User:    &api.User{ID: canonicalID, TelegramID: &[]int{rawTelegramID}[0]},
	}
}

// TestPopulateChatStateUsesCanonicalID — состояние, сохранённое экраном по
// каноническому номеру (все api.ChatState в internal/bot пишут u.User.ID),
// находится между шагами. Поиск по сырому telegram id не нашёл бы его никогда,
// и многошаговые сценарии (ввод суммы, добавление файла) молча ломались бы
func TestPopulateChatStateUsesCanonicalID(t *testing.T) {
	css := &stubChatStateService{states: map[int]*api.ChatState{
		testCanonicalID: {UserId: testCanonicalID, Action: "addDonorOperation"},
	}}
	l := &TelegramListener{ChatStateService: css}

	upd := updateWithMessage(testRawTelegramID, testCanonicalID)
	if err := l.populateChatState(context.Background(), upd); err != nil {
		t.Fatalf("populateChatState: %v", err)
	}
	if upd.ChatState == nil {
		t.Fatal("состояние не найдено — многошаговый сценарий бота оборвался бы")
	}
	if upd.ChatState.UserId != testCanonicalID {
		t.Fatalf("найдено чужое состояние: user_id=%d", upd.ChatState.UserId)
	}
	if len(css.queried) != 1 || css.queried[0] != testCanonicalID {
		t.Fatalf("запрошены ключи %v, ожидался только канонический %d", css.queried, testCanonicalID)
	}
}

// TestPopulateChatStateFallsBackToTelegramID — переходный fallback: в момент
// выкатки у людей есть незавершённые сценарии, записанные по telegram/chat id
func TestPopulateChatStateFallsBackToTelegramID(t *testing.T) {
	css := &stubChatStateService{states: map[int]*api.ChatState{
		testRawTelegramID: {UserId: testRawTelegramID, Action: "createRoom"},
	}}
	l := &TelegramListener{ChatStateService: css}

	upd := updateWithMessage(testRawTelegramID, testCanonicalID)
	if err := l.populateChatState(context.Background(), upd); err != nil {
		t.Fatalf("populateChatState: %v", err)
	}
	if upd.ChatState == nil || upd.ChatState.UserId != testRawTelegramID {
		t.Fatalf("состояние переходного периода потеряно: %+v", upd.ChatState)
	}
	want := []int{testCanonicalID, testRawTelegramID}
	if len(css.queried) != len(want) || css.queried[0] != want[0] || css.queried[1] != want[1] {
		t.Fatalf("порядок поиска %v, ожидался %v (канонический первым)", css.queried, want)
	}
}

// TestPopulateChatStateHistoricalUserQueriedOnce — у исторического аккаунта
// _id == telegram id, второй поход в базу был бы холостым
func TestPopulateChatStateHistoricalUserQueriedOnce(t *testing.T) {
	css := &stubChatStateService{states: map[int]*api.ChatState{}}
	l := &TelegramListener{ChatStateService: css}

	upd := updateWithMessage(555, 555)
	if err := l.populateChatState(context.Background(), upd); err != nil {
		t.Fatalf("populateChatState: %v", err)
	}
	if upd.ChatState != nil {
		t.Fatalf("состояния нет, а вернулось %+v", upd.ChatState)
	}
	if len(css.queried) != 1 {
		t.Fatalf("запросов к базе %d, ожидался 1: %v", len(css.queried), css.queried)
	}
}

// TestPopulateChatStateCallbackQuery — тот же ключ и для нажатия кнопки
func TestPopulateChatStateCallbackQuery(t *testing.T) {
	css := &stubChatStateService{states: map[int]*api.ChatState{
		testCanonicalID: {UserId: testCanonicalID, Action: "addFileToOperation"},
	}}
	l := &TelegramListener{ChatStateService: css}

	upd := &api.Update{
		CallbackQuery: &api.CallbackQuery{
			From:    api.User{ID: testRawTelegramID},
			Message: &api.Message{Chat: &api.Chat{ID: testRawTelegramID, Type: "private"}},
		},
		User: &api.User{ID: testCanonicalID},
	}
	if err := l.populateChatState(context.Background(), upd); err != nil {
		t.Fatalf("populateChatState: %v", err)
	}
	if upd.ChatState == nil || upd.ChatState.UserId != testCanonicalID {
		t.Fatalf("состояние не найдено по каноническому id: %+v", upd.ChatState)
	}
}

// TestPopulateChatStateMultiStepScenario — сквозной многошаговый сценарий: экран
// сохраняет состояние под u.User.ID, следующий апдейт того же пользователя его
// подхватывает. Именно эта связка рвалась бы при синтетическом _id
func TestPopulateChatStateMultiStepScenario(t *testing.T) {
	css := &stubChatStateService{states: map[int]*api.ChatState{}}
	l := &TelegramListener{ChatStateService: css}

	// шаг 1: экран бота записал состояние — все места в internal/bot пишут u.User.ID
	upd := updateWithMessage(testRawTelegramID, testCanonicalID)
	saved := &api.ChatState{UserId: upd.User.ID, Action: "setSumDonorOperation"}
	css.states[saved.UserId] = saved

	// шаг 2: следующий апдейт того же человека
	next := updateWithMessage(testRawTelegramID, testCanonicalID)
	if err := l.populateChatState(context.Background(), next); err != nil {
		t.Fatalf("populateChatState: %v", err)
	}
	if next.ChatState == nil || next.ChatState.Action != "setSumDonorOperation" {
		t.Fatalf("состояние между шагами потеряно: %+v", next.ChatState)
	}
}
