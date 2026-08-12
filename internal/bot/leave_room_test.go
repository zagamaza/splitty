package bot

import (
	"context"
	"errors"
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
func (s *leaveRoomService) FindById(context.Context, string) (*api.Room, error)  { return s.room, nil }
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

// leftRecord — запись отношения целиком: статуса мало, перепутанные комната
// или человек невидимы, а именно они решают, увидит ли следующее приглашение
// прошлый выход.
type leftRecord struct {
	roomID    primitive.ObjectID
	inviteeID int
	inviterID int
	status    api.InviteStatus
}

// recordingInvites запоминает записи отношения «человек × комната».
type recordingInvites struct {
	records []leftRecord
	// err — сбой записи: проверяем, что без следа отношения выхода не будет
	err error
	// status — текущее состояние записи, из которого исходит compare-and-set.
	// Тесты подменяют его на added: так выглядит запись, которую конкурентное
	// примирение в REST записало по устаревшему снимку комнаты
	status api.InviteStatus
	// reasserts — попытки вернуть запись в left уже ПОСЛЕ выхода
	reasserts []leftRecord
}

func (r *recordingInvites) Upsert(_ context.Context, roomID primitive.ObjectID, inviteeID, inviterID int,
	status api.InviteStatus, _ time.Time) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, leftRecord{roomID, inviteeID, inviterID, status})
	return nil
}

func (r *recordingInvites) SetStatusIfCurrent(_ context.Context, roomID primitive.ObjectID, inviteeID int,
	from, to api.InviteStatus, _ time.Time) (bool, error) {
	r.reasserts = append(r.reasserts, leftRecord{roomID, inviteeID, inviteeID, to})
	if r.err != nil {
		return false, r.err
	}
	if r.status != from {
		return false, nil
	}
	r.status = to
	return true, nil
}

// wroteLeft — ровно одна запись left про этого человека в этой комнате.
func (r *recordingInvites) wroteLeft(t *testing.T, room *api.Room, userID int) {
	t.Helper()
	if len(r.records) != 1 {
		t.Fatalf("ожидалась одна запись отношения, получено %d: %+v", len(r.records), r.records)
	}
	got := r.records[0]
	if got.status != api.InviteLeft {
		t.Fatalf("статус записи %q вместо left", got.status)
	}
	if got.roomID != room.ID {
		t.Fatalf("запись ушла в чужую комнату %s вместо %s", got.roomID.Hex(), room.ID.Hex())
	}
	if got.inviteeID != userID || got.inviterID != userID {
		t.Fatalf("запись про чужого человека: invitee=%d inviter=%d, выходил %d",
			got.inviteeID, got.inviterID, userID)
	}
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
	if len(invites.records) != 0 {
		t.Fatalf("записей отношения быть не должно: %+v", invites.records)
	}
}

// TestBotLeaveAllowsWhenLegacyRecipientsAreStale — операцию с легаси-полем
// recipients отредактировали в боте: он копирует старый список в новую запись и
// никогда его не чистит, а доли пишет заново. Считая recipients поверх
// recipients_with_sum, бот запирал бы человека, которого REST выпускает (он
// смотрит только на актуальные доли), — и выхода из телеграма не осталось бы
// вовсе: очистить recipients бот не умеет.
func TestBotLeaveAllowsWhenLegacyRecipientsAreStale(t *testing.T) {
	loadLang(t)
	donor := api.User{ID: 1, DisplayName: "Автор"}
	edited := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor: &donor,
		// свежие доли уже без участника 2
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 1}, Sum: 100}},
		// а протухший легаси-список всё ещё с ним
		Recipients: &[]api.User{{ID: 1}, {ID: 2}},
		Status:     active,
	}
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{edited}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatal("протухший legacy-список запер человека в комнате навсегда — REST его выпускает")
	}
	invites.wroteLeft(t, r, 2)
}

// TestBotLeaveAllowsWhenActiveOperationHasNoShares — активная операция без
// долей это битые данные; REST понижает её до драфта (api.NormalizedOperation)
// именно чтобы не запирать людей. Бот обязан решать так же, иначе донор такой
// операции выходит через приложение и не выходит через телеграм.
func TestBotLeaveAllowsWhenActiveOperationHasNoShares(t *testing.T) {
	loadLang(t)
	donor := api.User{ID: 2, DisplayName: "Гость"}
	broken := api.Operation{
		ID: primitive.NewObjectID(), Description: "Битая", Sum: 100,
		Donor:  &donor,
		Status: active,
	}
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{broken}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatal("активная операция без долей заперла донора в комнате — REST его выпускает")
	}
	invites.wroteLeft(t, r, 2)
}

// TestBotLeaveRoomWithoutOperations — у комнат, созданных мимо бота, поле
// operations не заведено вовсе. Разыменование nil роняло хендлер: тап по
// «выйти» не делал ничего.
func TestBotLeaveRoomWithoutOperations(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatal("выход из комнаты без операций не состоялся")
	}
	invites.wroteLeft(t, r, 2)
}

// TestBotLeaveOperationWithoutDonor — плательщика в операции может не быть
// (черновик бота, битые данные). Проверка донора обязана это пережить.
func TestBotLeaveOperationWithoutDonor(t *testing.T) {
	loadLang(t)
	orphan := api.Operation{
		ID: primitive.NewObjectID(), Description: "Без плательщика", Sum: 100,
		RecipientsWithSum: []api.RecipientWithSum{{User: api.User{ID: 1}, Sum: 100}},
		Status:            active,
	}
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{orphan}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatal("операция без плательщика заперла постороннего человека в комнате")
	}
	invites.wroteLeft(t, r, 2)
}

// TestBotLeaveKeepsMembershipWhenLeftRecordFails — паритет с REST
// (rest.removeMember): сбой записи left отменяет выход. Иначе человек оказался
// бы вне комнаты без следа отношения, и следующее приглашение вернуло бы его
// молча, мимо «после выхода — только с явного согласия».
func TestBotLeaveKeepsMembershipWhenLeftRecordFails(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{}}
	h, rs, invites := leaveHandler(r, 1, 2)
	invites.err = errors.New("mongo недоступна")

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 0 {
		t.Fatal("человек вышел, хотя записать след отношения не удалось — следующее приглашение вернёт его молча")
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
	invites.wroteLeft(t, r, 2)
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

	if len(invites.records) != 0 {
		t.Fatalf("не-участнику записали отношение: %+v", invites.records)
	}
}

// TestBotLeaveLastMemberBlocked — паритет с REST (409 last_member): комната без
// участников осталась бы бесхозной, а удаления комнаты нет ни в REST, ни в боте.
// Правила в боте не было вовсе — через телеграм комнату можно было осиротить.
func TestBotLeaveLastMemberBlocked(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Соло",
		Members: &[]api.User{{ID: 2}}, Operations: &[]api.Operation{}}
	h, rs, invites := leaveHandler(r, 2)

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 0 {
		t.Fatal("последний участник вышел — комната осталась бесхозной, а удалить её нечем")
	}
	if len(invites.records) != 0 {
		t.Fatalf("отказанный выход не должен писать отношение: %+v", invites.records)
	}
}

// TestBotLeaveWithoutInviteStoreKeepsMembership — без хранилища приглашений
// след выхода записать нечем, и молчаливый пропуск записи открыл бы ровно тот
// тихий возврат, ради которого она заведена. Выход отменяется.
func TestBotLeaveWithoutInviteStoreKeepsMembership(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{}}
	rs := &leaveRoomService{room: r, members: map[int]bool{1: true, 2: true}}
	h := NewSelectedLeaveRoom(nil, nil, rs, nil, nil, &Config{})

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 0 {
		t.Fatal("человек вышел без следа отношения — следующее приглашение вернёт его молча")
	}
}

// TestBotLeaveBadRoomIdKeepsMembership — то же для неразбираемого id комнаты:
// запись left в него не уйдёт, значит и выхода быть не должно.
func TestBotLeaveBadRoomIdKeepsMembership(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{}}
	h, rs, invites := leaveHandler(r, 1, 2)

	h.OnMessage(context.Background(), leaveUpdate("не-hex", 2))

	if rs.calls != 0 {
		t.Fatal("выход состоялся с id комнаты, в который след отношения записать нельзя")
	}
	if len(invites.records) != 0 {
		t.Fatalf("записей отношения быть не должно: %+v", invites.records)
	}
}

// TestBotLeaveReassertsLeftAfterConcurrentAdded — примирение записи в REST
// (шаг (3) handleAddMember) читает комнату РАНЬШЕ выхода и может записать added
// уже после нашего left. Осталось бы «в комнате нет, а приглашение added», и
// следующее приглашение вернуло бы человека молча, мимо согласия. Поэтому после
// выхода запись возвращается в left compare-and-set'ом.
func TestBotLeaveReassertsLeftAfterConcurrentAdded(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{}}
	h, rs, invites := leaveHandler(r, 1, 2)
	// запись, которую конкурентное примирение увело в added по устаревшему снимку
	invites.status = api.InviteAdded

	h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if rs.calls != 1 {
		t.Fatalf("выход не состоялся, вызовов LeaveRoom: %d", rs.calls)
	}
	if len(invites.reasserts) != 1 {
		t.Fatalf("после выхода запись не проверялась на added: %+v", invites.reasserts)
	}
	got := invites.reasserts[0]
	if got.roomID != r.ID || got.inviteeID != 2 || got.status != api.InviteLeft {
		t.Fatalf("проверка ушла не туда: %+v", got)
	}
	if invites.status != api.InviteLeft {
		t.Fatalf("запись осталась в %q — человек вне комнаты с приглашением added", invites.status)
	}
}

// TestBotLeaveBlockedWhenOperationLandsDuringLeave — расход на выходящего могли
// завести уже ПОСЛЕ проверки api.HasOperations: его ловит фильтр LeaveRoom, и
// matched==0 при сохранившемся членстве означает не выход, а отказ. Бот обязан
// сказать то же, что сказала бы проверка, иначе отрапортует о выходе, которого
// не было.
func TestBotLeaveBlockedWhenOperationLandsDuringLeave(t *testing.T) {
	loadLang(t)
	r := &api.Room{ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{{ID: 1}, {ID: 2}}, Operations: &[]api.Operation{}}
	// членство в комнате есть, а LeaveRoom не сработал — так выглядит фильтр,
	// увидевший расход, которого не было в прочитанном снимке
	rs := &leaveRoomService{room: r, members: map[int]bool{1: true}}
	invites := &recordingInvites{}
	h := NewSelectedLeaveRoom(nil, nil, rs, invites, nil, &Config{})

	resp := h.OnMessage(context.Background(), leaveUpdate(r.ID.Hex(), 2))

	if resp.CallbackConfig == nil {
		t.Fatal("отказ выхода без ответа человеку")
	}
	if resp.CallbackConfig.Text != I18n(&api.User{ID: 2}, "msg_you_can_not_leave") {
		t.Fatalf("человеку сообщили %q вместо отказа по расходам", resp.CallbackConfig.Text)
	}
	if resp.Redirect != nil {
		t.Fatal("бот увёл человека на стартовый экран, будто выход состоялся")
	}
	if len(invites.reasserts) != 0 {
		t.Fatalf("отказанный выход не должен трогать запись: %+v", invites.reasserts)
	}
}
