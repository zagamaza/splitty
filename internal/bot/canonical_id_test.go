package bot

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Пользователь, ради которого существует Task 6: пришёл через Google (номер
// Splitty ≥ 10^12), затем привязал telegram (id порядка 10^9). getFrom(u).ID
// вернёт для него telegram id — то есть чужой или несуществующий номер.
const (
	canonicalUserID = 1_000_000_000_321
	rawTelegramID   = 987_654_321
)

// canonicalUpdate — апдейт, в котором сырой пользователь и канонический РАЗНЫЕ.
// Это ключ теста: пока _id == telegram id, подмена getFrom на u.User незаметна
func canonicalUpdate(action api.Action, data *api.CallbackData) *api.Update {
	tg := rawTelegramID
	return &api.Update{
		CallbackQuery: &api.CallbackQuery{
			From:    api.User{ID: rawTelegramID, DisplayName: "Сырой"},
			Message: &api.Message{Chat: &api.Chat{ID: rawTelegramID, Type: "private"}},
		},
		Button: &api.Button{Action: action, CallbackData: data},
		User: &api.User{
			ID: canonicalUserID, DisplayName: "Канонический",
			TelegramID: &tg, CountInPage: 5, UserLang: "ru",
		},
	}
}

// --- заглушки, запоминающие, каким номером их спросили ---

type recordingRoomService struct {
	RoomService
	askedUserIDs []int
	rooms        []api.Room
	room         *api.Room
}

func (r *recordingRoomService) FindRoomsByUserId(_ context.Context, id int) (*[]api.Room, error) {
	r.askedUserIDs = append(r.askedUserIDs, id)
	return &r.rooms, nil
}

func (r *recordingRoomService) FindArchivedRoomsByUserId(_ context.Context, id int) (*[]api.Room, error) {
	r.askedUserIDs = append(r.askedUserIDs, id)
	return &r.rooms, nil
}

func (r *recordingRoomService) FindById(context.Context, string) (*api.Room, error) {
	return r.room, nil
}

type recordingOperationService struct {
	OperationService
	askedUserIDs []int
	debts        []api.Debt
	debt         *api.Debt
	created      *api.Operation
}

func (o *recordingOperationService) GetUserDebts(_ context.Context, userId int, _ string) (*[]api.Debt, error) {
	o.askedUserIDs = append(o.askedUserIDs, userId)
	return &o.debts, nil
}

func (o *recordingOperationService) GetUserDebt(_ context.Context, debtorId, _ int, _ string) (*api.Debt, error) {
	o.askedUserIDs = append(o.askedUserIDs, debtorId)
	return o.debt, nil
}

func (o *recordingOperationService) CreateOperation(_ context.Context, op *api.Operation, _ string) error {
	o.created = op
	return nil
}

type noopChatStateService struct{}

func (noopChatStateService) Save(context.Context, *api.ChatState) error { return nil }
func (noopChatStateService) DeleteById(context.Context, primitive.ObjectID) error {
	return nil
}
func (noopChatStateService) FindByUserId(context.Context, int) (*api.ChatState, error) {
	return nil, nil
}
func (noopChatStateService) CleanChatState(context.Context, *api.ChatState) {}

type stubBotUserService struct {
	UserService
	users map[int]*api.User
}

func (s stubBotUserService) FindById(_ context.Context, id int) (*api.User, error) {
	return s.users[id], nil
}

type noopRoomStateService struct{ RoomStateService }

func (noopRoomStateService) DefinePaidOfDebtsUserIdsAndSave(context.Context, *api.Room) error {
	return nil
}

// TestAllRoomUsesCanonicalUserID: список комнат ищется по номеру Splitty. По
// сырому telegram id пользователь увидел бы пустой список — комнаты хранят
// канонические id участников
func TestAllRoomUsesCanonicalUserID(t *testing.T) {
	loadLang(t)
	rs := &recordingRoomService{rooms: []api.Room{{ID: primitive.NewObjectID(), Name: "Тусa"}}}
	screen := NewAllRoom(noopChatStateService{}, noopButtonService{}, rs, &Config{})

	screen.OnMessage(context.Background(), canonicalUpdate(viewAllRooms, &api.CallbackData{}))

	assertAskedCanonical(t, rs.askedUserIDs)
}

// TestArchivedRoomsUsesCanonicalUserID — то же для архива
func TestArchivedRoomsUsesCanonicalUserID(t *testing.T) {
	loadLang(t)
	rs := &recordingRoomService{rooms: []api.Room{{ID: primitive.NewObjectID(), Name: "Архив"}}}
	screen := NewArchivedRooms(noopChatStateService{}, noopButtonService{}, rs, &Config{})

	screen.OnMessage(context.Background(), canonicalUpdate(viewArchivedRooms, &api.CallbackData{}))

	assertAskedCanonical(t, rs.askedUserIDs)
}

// TestViewRoomChecksMembershipByCanonicalID: участник комнаты записан в снимке
// по номеру Splitty. Проверка по telegram id ответила бы «вы не участник»
func TestViewRoomChecksMembershipByCanonicalID(t *testing.T) {
	loadLang(t)
	roomID := primitive.NewObjectID()
	r := &recordingRoomService{room: &api.Room{
		ID:      roomID,
		Name:    "Тусa",
		Members: &[]api.User{{ID: canonicalUserID, DisplayName: "Канонический"}},
	}}
	screen := NewViewRoom(noopButtonService{}, r, noopChatStateService{}, &Config{})

	upd := canonicalUpdate(viewRoom, &api.CallbackData{RoomId: roomID.Hex()})
	resp := screen.OnMessage(context.Background(), upd)

	if len(resp.Chattable) != 1 {
		t.Fatalf("экран комнаты не отрисован: %+v", resp)
	}
	msg, ok := resp.Chattable[0].(tgbotapi.MessageConfig)
	if ok && msg.Text == I18n(upd.User, "msg_not_be_in_rooms") {
		t.Fatal("участнику показано «вы не участник» — проверка идёт по сырому telegram id")
	}
}

// TestViewUserDebtsUsesCanonicalUserID: долги ищутся по номеру Splitty
func TestViewUserDebtsUsesCanonicalUserID(t *testing.T) {
	loadLang(t)
	roomID := primitive.NewObjectID()
	rs := &recordingRoomService{room: &api.Room{ID: roomID, Name: "Тусa", Currency: "RUB"}}
	os := &recordingOperationService{debts: []api.Debt{{
		Debtor: &api.User{ID: canonicalUserID, DisplayName: "Канонический"},
		Lender: &api.User{ID: 7, DisplayName: "Кредитор"},
		Sum:    100,
	}}}
	screen := NewViewUserDebts(noopChatStateService{}, rs, noopButtonService{}, os, &Config{})

	screen.OnMessage(context.Background(), canonicalUpdate(viewUserDebts, &api.CallbackData{RoomId: roomID.Hex()}))

	assertAskedCanonical(t, os.askedUserIDs)
}

// TestStatisticUsesCanonicalUserID: статистика по номеру Splitty
func TestStatisticUsesCanonicalUserID(t *testing.T) {
	loadLang(t)
	roomID := primitive.NewObjectID()
	rs := &recordingRoomService{room: &api.Room{ID: roomID, Name: "Тусa", Currency: "RUB"}}
	ss := &recordingStatisticService{}
	screen := NewStatistic(noopButtonService{}, rs, noopChatStateService{}, ss, &Config{})

	screen.OnMessage(context.Background(), canonicalUpdate(statistics, &api.CallbackData{RoomId: roomID.Hex()}))

	assertAskedCanonical(t, ss.askedUserIDs)
}

type recordingStatisticService struct{ askedUserIDs []int }

func (s *recordingStatisticService) GetAllCostsSum(context.Context, string) (int, error) {
	return 0, nil
}
func (s *recordingStatisticService) GetAllDebtsSum(context.Context, string) (int, error) {
	return 0, nil
}
func (s *recordingStatisticService) GetUserCostsSum(_ context.Context, userId int, _ string) (int, error) {
	s.askedUserIDs = append(s.askedUserIDs, userId)
	return 0, nil
}

func (s *recordingStatisticService) GetUserDebtAndLendSum(_ context.Context, userId int, _ string) (int, int, error) {
	s.askedUserIDs = append(s.askedUserIDs, userId)
	return 0, 0, nil
}

// TestArchiveRoomUsesCanonicalUserID: архивация по сырому telegram id была бы
// молчаливым no-op — RoomStates.Archived хранит номера Splitty
func TestArchiveRoomUsesCanonicalUserID(t *testing.T) {
	loadLang(t)
	roomID := primitive.NewObjectID()
	rs := &recordingRoomService{room: &api.Room{ID: roomID, Name: "Тусa"}}
	rss := &recordingRoomStateService{}
	viewSetting := NewRoomSetting(noopButtonService{}, rs, noopChatStateService{}, &Config{})
	screen := NewArchiveRoom(noopButtonService{}, rss, rs, noopChatStateService{}, &Config{}, viewSetting)

	screen.OnMessage(context.Background(), canonicalUpdate(archiveRoom, &api.CallbackData{RoomId: roomID.Hex()}))

	assertAskedCanonical(t, rss.askedUserIDs)
}

type recordingRoomStateService struct {
	RoomStateService
	askedUserIDs []int
}

func (r *recordingRoomStateService) ArchiveRoom(_ context.Context, userId int, _ string) error {
	r.askedUserIDs = append(r.askedUserIDs, userId)
	return nil
}

func (r *recordingRoomStateService) UnArchiveRoom(_ context.Context, userId int, _ string) error {
	r.askedUserIDs = append(r.askedUserIDs, userId)
	return nil
}

// TestDebtRepaymentOperationDonorIsCanonical — САМЫЙ ТЯЖЁЛЫЙ случай задачи:
// donor уходит в Operation.Donor и дальше в документ комнаты как участник
// расчёта. Сырой telegram id здесь означает порчу входных данных для долгов, а
// не косметику
func TestDebtRepaymentOperationDonorIsCanonical(t *testing.T) {
	loadLang(t)
	roomID := primitive.NewObjectID()
	lender := &api.User{ID: 7, DisplayName: "Кредитор"}
	rs := &recordingRoomService{room: &api.Room{ID: roomID, Name: "Тусa", Currency: "RUB"}}
	os := &recordingOperationService{debt: &api.Debt{
		Debtor: &api.User{ID: canonicalUserID, DisplayName: "Канонический"},
		Lender: lender,
		Sum:    500,
	}}
	us := stubBotUserService{users: map[int]*api.User{7: lender, canonicalUserID: {ID: canonicalUserID}}}
	screen := NewAddRecepientOperation(noopChatStateService{}, noopButtonService{}, os, us, rs, noopRoomStateService{}, &Config{})

	upd := canonicalUpdate(addRecipientOperation, &api.CallbackData{RoomId: roomID.Hex(), UserId: 7})
	upd.CallbackQuery = nil
	upd.Message = &api.Message{Text: "100", From: api.User{ID: rawTelegramID}, Chat: &api.Chat{ID: rawTelegramID, Type: "private"}}
	upd.ChatState = &api.ChatState{
		UserId:       canonicalUserID,
		Action:       addRecipientOperation,
		CallbackData: &api.CallbackData{RoomId: roomID.Hex(), UserId: 7},
	}

	screen.OnMessage(context.Background(), upd)

	if os.created == nil {
		t.Fatal("операция возврата долга не создана")
	}
	if os.created.Donor == nil {
		t.Fatal("у операции нет донора")
	}
	if os.created.Donor.ID != canonicalUserID {
		t.Fatalf("donor.id = %d, want %d: в документ комнаты записан сырой telegram id", os.created.Donor.ID, canonicalUserID)
	}
	assertAskedCanonical(t, os.askedUserIDs)
}

// TestRoomCreatorIsCanonical: создатель комнаты попадает в снимок участников, и
// его _id участвует в расчёте долгов
func TestRoomCreatorIsCanonical(t *testing.T) {
	loadLang(t)
	rs := &capturingCreateRoomService{}
	screen := NewRoomSetName(noopChatStateService{}, noopButtonService{}, rs, &Config{})

	upd := canonicalUpdate(createRoom, &api.CallbackData{})
	upd.CallbackQuery = nil
	upd.Message = &api.Message{Text: "Тусa", From: api.User{ID: rawTelegramID, DisplayName: "Сырой"}, Chat: &api.Chat{ID: rawTelegramID, Type: "private"}}
	upd.ChatState = &api.ChatState{UserId: canonicalUserID, Action: createRoom}

	screen.OnMessage(context.Background(), upd)

	if rs.created == nil || rs.created.Members == nil || len(*rs.created.Members) != 1 {
		t.Fatalf("комната создана без участников: %+v", rs.created)
	}
	if got := (*rs.created.Members)[0].ID; got != canonicalUserID {
		t.Fatalf("создатель записан с id=%d, want %d — в снимок ушёл сырой telegram id", got, canonicalUserID)
	}
}

type capturingCreateRoomService struct {
	RoomService
	created *api.Room
}

func (c *capturingCreateRoomService) CreateRoom(_ context.Context, r *api.Room) (*api.Room, error) {
	c.created = r
	saved := *r
	saved.ID = primitive.NewObjectID()
	return &saved, nil
}

// assertAskedCanonical — сервис спрошен номером Splitty, а не telegram id
func assertAskedCanonical(t *testing.T, asked []int) {
	t.Helper()
	if len(asked) == 0 {
		t.Fatal("сервис не вызван — тест ничего не проверил")
	}
	for _, id := range asked {
		if id == rawTelegramID {
			t.Fatalf("сервис спрошен сырым telegram id %d вместо номера Splitty %d", id, canonicalUserID)
		}
		if id != canonicalUserID {
			t.Fatalf("сервис спрошен номером %d, want %d", id, canonicalUserID)
		}
	}
}
