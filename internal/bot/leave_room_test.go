package bot

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Тесты выхода из комнаты через бота.
//
// Выход через бота и через REST обязаны решать одинаково: правило «пока на
// человеке висят расходы — не выпускаем» и запись left, закрывающая тихий
// возврат, живут в обоих путях, а расходятся они молча.

// leaveRoomService — минимальный RoomService: отдаёт одну комнату и считает
// выходы. Членство проверяется как в mongo (LeaveRoom возвращает matched > 0).
type leaveRoomService struct {
	room    *api.Room
	members map[int]bool
	calls   int
}

func (s *leaveRoomService) JoinToRoom(context.Context, api.User, string) error { return nil }

func (s *leaveRoomService) LeaveRoom(_ context.Context, userId int, _ string) (bool, error) {
	s.calls++
	if !s.members[userId] {
		return false, nil
	}
	delete(s.members, userId)
	return true, nil
}

func (s *leaveRoomService) CreateRoom(context.Context, *api.Room) (*api.Room, error) {
	return nil, nil
}
func (s *leaveRoomService) FindById(context.Context, string) (*api.Room, error) { return s.room, nil }
func (s *leaveRoomService) UpdateCurrency(context.Context, string, string) error { return nil }
func (s *leaveRoomService) FindRoomsByUserId(context.Context, int) (*[]api.Room, error) {
	return nil, nil
}
func (s *leaveRoomService) FindArchivedRoomsByUserId(context.Context, int) (*[]api.Room, error) {
	return nil, nil
}
func (s *leaveRoomService) FindRoomsByLikeName(context.Context, int, string) (*[]api.Room, error) {
	return nil, nil
}

// recordingInvites запоминает записи отношения «человек × комната».
type recordingInvites struct{ statuses []api.InviteStatus }

func (r *recordingInvites) Upsert(_ context.Context, _ primitive.ObjectID, _, _ int,
	status api.InviteStatus, _ time.Time) error {
	r.statuses = append(r.statuses, status)
	return nil
}

func leaveUpdate(roomId string, userId int) *api.Update {
	user := &api.User{ID: userId, DisplayName: "Гость"}
	return &api.Update{
		User:          user,
		Button:        &api.Button{ID: primitive.NewObjectID(), CallbackData: &api.CallbackData{RoomId: roomId}},
		CallbackQuery: &api.CallbackQuery{ID: "cb"},
	}
}

func leaveHandler(room *api.Room, members ...int) (*SelectedLeaveRoom, *leaveRoomService, *recordingInvites) {
	rs := &leaveRoomService{room: room, members: map[int]bool{}}
	for _, id := range members {
		rs.members[id] = true
	}
	invites := &recordingInvites{}
	return NewSelectedLeaveRoom(nil, nil, rs, invites, nil, &Config{}), rs, invites
}

// TestBotLeaveBlockedByLegacyOperation — легаси-операции эпохи master-2021
// хранят получателей в recipients, без долей. Проверяя только
// recipients_with_sum, бот выпустил бы должника — а REST его держит (409).
func TestBotLeaveBlockedByLegacyOperation(t *testing.T) {
	loadLang(t)
	donor := api.User{ID: 1, DisplayName: "Автор"}
	legacy := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:      &donor,
		Recipients: &[]api.User{{ID: 1}, {ID: 2}},
	}
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{legacy}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 0 {
		t.Fatal("должника выпустили из комнаты — кредитор молча теряет деньги")
	}
	if len(invites.statuses) != 0 {
		t.Fatalf("записей отношения быть не должно: %v", invites.statuses)
	}
}

// TestBotLeaveIgnoresArchivedOperation — архивные версии отредактированных
// расходов в долгах не участвуют (как в REST). Считая их, бот запирал бы
// человека в комнате навсегда: убрать себя из архивной версии нельзя.
func TestBotLeaveIgnoresArchivedOperation(t *testing.T) {
	loadLang(t)
	donor := api.User{ID: 1, DisplayName: "Автор"}
	archived := api.Operation{
		ID: primitive.NewObjectID(), Description: "Старая версия", Sum: 100,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 2}, Sum: 100}},
		Status:            archive,
	}
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{archived}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatal("архивная версия расхода заперла человека в комнате")
	}
	if len(invites.statuses) != 1 || invites.statuses[0] != api.InviteLeft {
		t.Fatalf("после выхода обязана остаться запись left: %v", invites.statuses)
	}
}

// TestBotLeaveNonMemberWritesNothing — выход того, кого в комнате нет, ничего
// не меняет. Запись left ему погнала бы следующее приглашение через лишний
// pending: человек ждал бы решения по комнате, из которой не выходил.
func TestBotLeaveNonMemberWritesNothing(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}}, Operations: &[]api.Operation{}}
	h, _, invites := leaveHandler(r, 1)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if len(invites.statuses) != 0 {
		t.Fatalf("не-участнику записали отношение: %v", invites.statuses)
	}
}
