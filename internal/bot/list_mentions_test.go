package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Списочные экраны рисуют участников из встроенных снимков комнаты/операции,
// а там telegram_id нет никогда (api.User.Snapshot() его обнуляет). Пока
// упоминание собиралось по снимку, кликабельные ссылки теряли ВСЕ живые
// telegram-пользователи. Резолв обязан идти по каноническим документам и
// обязан быть батчевым: экран комнаты рисуется на каждое нажатие кнопки.

const (
	memberWithTgID   = 1_000_000_000_777
	memberWithTgTgID = 555_444_333
	memberNoTgID     = 1_000_000_000_888
)

// countingUserService считает, чем именно резолвился список: батчем или
// поштучно. Без счётчиков тест «упоминание кликабельно» прошёл бы и на N
// запросах вместо одного
type countingUserService struct {
	UserService
	users      map[int]*api.User
	byIDCalls  int
	batchCalls int
	batchedIds [][]int
}

func (s *countingUserService) FindById(_ context.Context, id int) (*api.User, error) {
	s.byIDCalls++
	return s.users[id], nil
}

func (s *countingUserService) FindByIds(_ context.Context, ids []int) ([]api.User, error) {
	s.batchCalls++
	s.batchedIds = append(s.batchedIds, ids)
	var out []api.User
	for _, id := range ids {
		if u, ok := s.users[id]; ok && u != nil {
			out = append(out, *u)
		}
	}
	return out, nil
}

// canonicalMembers — канонические документы двух участников: один с telegram,
// второй вошёл через Google/Apple
func canonicalMembers() map[int]*api.User {
	tg := memberWithTgTgID
	return map[int]*api.User{
		memberWithTgID: {ID: memberWithTgID, DisplayName: "С телеграмом", TelegramID: &tg},
		memberNoTgID:   {ID: memberNoTgID, DisplayName: "Без телеграма"},
	}
}

// roomWithMembers — комната со снимками участников: как в базе, БЕЗ telegram_id
func roomWithMembers() *api.Room {
	return &api.Room{
		ID:       primitive.NewObjectID(),
		Name:     "Тусa",
		Currency: "RUB",
		Members: &[]api.User{
			{ID: canonicalUserID, DisplayName: "Канонический"},
			{ID: memberWithTgID, DisplayName: "С телеграмом"},
			{ID: memberNoTgID, DisplayName: "Без телеграма"},
		},
	}
}

func screenText(t *testing.T, resp api.TelegramMessage) string {
	t.Helper()
	if len(resp.Chattable) != 1 {
		t.Fatalf("экран не отрисован: %+v", resp)
	}
	msg, ok := resp.Chattable[0].(tgbotapi.EditMessageTextConfig)
	if !ok {
		t.Fatalf("неожиданный тип сообщения: %T", resp.Chattable[0])
	}
	return msg.Text
}

// TestViewRoomMembersMentionCanonicalTelegramID: в списке участников комнаты
// telegram-пользователь остаётся кликабельным, хотя в снимке telegram_id нет
func TestViewRoomMembersMentionCanonicalTelegramID(t *testing.T) {
	loadLang(t)
	room := roomWithMembers()
	us := &countingUserService{users: canonicalMembers()}
	screen := NewViewRoom(noopButtonService{}, &recordingRoomService{room: room}, noopChatStateService{}, us, &Config{})

	upd := canonicalUpdate(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	text := screenText(t, screen.OnMessage(context.Background(), upd))

	wantLink := `<a href="tg://user?id=555444333">С телеграмом</a>`
	if !strings.Contains(text, wantLink) {
		t.Errorf("участник с telegram не кликабелен, ожидали %q в:\n%s", wantLink, text)
	}
	if !strings.Contains(text, "- Без телеграма\n") {
		t.Errorf("участник без telegram отрисован не простым именем:\n%s", text)
	}
	if strings.Contains(text, `id=1000000000888`) || strings.Contains(text, `id=1000000000777`) {
		t.Errorf("в упоминание уехал номер Splitty вместо telegram id:\n%s", text)
	}
}

// TestViewRoomResolvesMembersInOneBatch: N участников — один запрос, а не N.
// Экран комнаты рисуется на каждое нажатие кнопки, поштучный резолв здесь —
// прямой путь к нагрузке на mongo
func TestViewRoomResolvesMembersInOneBatch(t *testing.T) {
	loadLang(t)
	room := roomWithMembers()
	us := &countingUserService{users: canonicalMembers()}
	screen := NewViewRoom(noopButtonService{}, &recordingRoomService{room: room}, noopChatStateService{}, us, &Config{})

	screen.OnMessage(context.Background(), canonicalUpdate(viewRoom, &api.CallbackData{RoomId: room.ID.Hex()}))

	if us.batchCalls != 1 {
		t.Errorf("ожидали ровно 1 батч-запрос на экран, получили %d (%v)", us.batchCalls, us.batchedIds)
	}
	if us.byIDCalls != 0 {
		t.Errorf("поштучных чтений быть не должно, получили %d", us.byIDCalls)
	}
	if len(us.batchedIds) == 1 && len(us.batchedIds[0]) != 3 {
		t.Errorf("в батч ушли не все участники: %v", us.batchedIds[0])
	}
}

// TestJoinRoomMembersMentionCanonicalTelegramID: тот же список после join
func TestJoinRoomMembersMentionCanonicalTelegramID(t *testing.T) {
	loadLang(t)
	room := roomWithMembers()
	us := &countingUserService{users: canonicalMembers()}
	screen := NewJoinRoom(noopChatStateService{}, noopButtonService{}, &joiningRoomService{room: room}, us, &Config{})

	upd := canonicalUpdate(joinRoom, &api.CallbackData{RoomId: room.ID.Hex()})
	text := screenText(t, screen.OnMessage(context.Background(), upd))

	if !strings.Contains(text, `<a href="tg://user?id=555444333">С телеграмом</a>`) {
		t.Errorf("участник с telegram не кликабелен:\n%s", text)
	}
	if us.batchCalls != 1 {
		t.Errorf("ожидали ровно 1 батч-запрос, получили %d", us.batchCalls)
	}
}

// TestAllRoomInlineReusesResolverAcrossRooms: в inline-выдаче несколько комнат
// с пересекающимися участниками — кеш резолвера общий на всю выдачу, повторно
// тех же людей не перечитываем
func TestAllRoomInlineReusesResolverAcrossRooms(t *testing.T) {
	loadLang(t)
	first, second := *roomWithMembers(), *roomWithMembers()
	rs := &inlineRoomService{rooms: []api.Room{first, second}}
	us := &countingUserService{users: canonicalMembers()}
	screen := NewAllRoomInline(noopChatStateService{}, noopButtonService{}, rs, &recordingStatisticService{}, us, &Config{})

	upd := canonicalUpdate(viewAllRooms, &api.CallbackData{})
	upd.InlineQuery = &api.InlineQuery{ID: "q1", From: api.User{ID: rawTelegramID}}
	screen.OnMessage(context.Background(), upd)

	if us.batchCalls != 1 {
		t.Errorf("ожидали 1 батч на всю выдачу, получили %d (%v)", us.batchCalls, us.batchedIds)
	}
	if us.byIDCalls != 0 {
		t.Errorf("поштучных чтений быть не должно, получили %d", us.byIDCalls)
	}
}

// stubDebtOperationService отдаёт историю возвратов долга
type stubDebtOperationService struct {
	OperationService
	ops []api.Operation
}

func (s *stubDebtOperationService) GetAllDebtOperations(context.Context, string) (*[]api.Operation, error) {
	return &s.ops, nil
}

// inlineRoomService — RoomService для inline-выдачи AllRoomInline; запоминает,
// каким номером его спросили (см. TestInlineRoomSearchUsesCanonicalUserID)
type inlineRoomService struct {
	RoomService
	rooms        []api.Room
	askedUserIDs []int
}

func (r *inlineRoomService) FindRoomsByLikeName(_ context.Context, userId int, _ string) (*[]api.Room, error) {
	r.askedUserIDs = append(r.askedUserIDs, userId)
	return &r.rooms, nil
}

// joiningRoomService — RoomService для JoinRoom: join no-op, комната одна;
// запоминает пользователя, ушедшего в снимок участников
type joiningRoomService struct {
	RoomService
	room   *api.Room
	joined []api.User
}

func (r *joiningRoomService) JoinToRoom(_ context.Context, u api.User, _ string) error {
	r.joined = append(r.joined, u)
	return nil
}
func (r *joiningRoomService) FindById(context.Context, string) (*api.Room, error) {
	return r.room, nil
}

// TestDebtHistoryMentionsCanonicalTelegramID: в истории возвратов долга
// упоминания тоже собираются по каноническим документам, одним запросом
func TestDebtHistoryMentionsCanonicalTelegramID(t *testing.T) {
	loadLang(t)
	room := roomWithMembers()
	os := &stubDebtOperationService{ops: []api.Operation{{
		ID:    primitive.NewObjectID(),
		Sum:   500,
		Donor: &api.User{ID: memberWithTgID, DisplayName: "С телеграмом"},
		RecipientsWithSum: []api.RecipientWithSum{
			{User: api.User{ID: memberNoTgID, DisplayName: "Без телеграма"}, Sum: 500},
		},
	}}}
	us := &countingUserService{users: canonicalMembers()}
	screen := NewViewAllDebtOperations(noopChatStateService{}, &recordingRoomService{room: room}, noopButtonService{}, os, us, &Config{})

	upd := canonicalUpdate(viewAllDebtOperations, &api.CallbackData{RoomId: room.ID.Hex()})
	text := screenText(t, screen.OnMessage(context.Background(), upd))

	if !strings.Contains(text, `<a href="tg://user?id=555444333">С телеграмом</a>`) {
		t.Errorf("донор с telegram не кликабелен:\n%s", text)
	}
	if !strings.Contains(text, "Без телеграма") || strings.Contains(text, `id=1000000000888`) {
		t.Errorf("получатель без telegram отрисован неверно:\n%s", text)
	}
	if us.batchCalls != 1 {
		t.Errorf("ожидали 1 батч на страницу истории, получили %d", us.batchCalls)
	}
	if us.byIDCalls != 0 {
		t.Errorf("поштучных чтений быть не должно, получили %d", us.byIDCalls)
	}
}

// TestWarmDoesNotRereadMissingUsers: участника нет в коллекции user (снимок
// пережил владельца). Батч его не вернул — повторно поштучно не читаем, иначе
// один «мёртвый» участник вернул бы экрану N запросов
func TestWarmDoesNotRereadMissingUsers(t *testing.T) {
	us := &countingUserService{users: canonicalMembers()}
	cu := canonical(context.Background(), us)
	ghost := &api.User{ID: 42, DisplayName: "Призрак"}

	cu.warm([]int{ghost.ID})
	if got := cu.link(ghost); got != "Призрак" {
		t.Errorf("ожидали простое имя, получили %q", got)
	}
	if got := cu.link(ghost); got != "Призрак" {
		t.Errorf("повторный рендер сломался: %q", got)
	}
	if us.byIDCalls != 0 {
		t.Errorf("отсутствующего пользователя перечитывали поштучно %d раз", us.byIDCalls)
	}
}
