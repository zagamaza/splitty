package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// inviteFixture — сервер с комнатой, где состоит только приглашающий (1),
// и пользователем 2, которого предстоит позвать.
type inviteFixture struct {
	srv      *Server
	invites  *fakeInviteStore
	notifier *fakeNotifier
	roomRepo *fakeRoomRepo
	room     *api.Room
	token    string
}

// newInviteFixture собирает окружение. sharedRoom=true добавляет ВТОРУЮ общую
// комнату — так пользователь 2 попадает в /friends приглашающего.
func newInviteFixture(t *testing.T, sharedRoom bool) *inviteFixture {
	t.Helper()

	room := &api.Room{
		ID:       primitive.NewObjectID(),
		Name:     "Поездка",
		Members:  &[]api.User{testUser1},
		CreateAt: time.Now(),
	}
	rooms := []*api.Room{room}
	if sharedRoom {
		rooms = append(rooms, &api.Room{
			ID:       primitive.NewObjectID(),
			Name:     "Квартира",
			Members:  &[]api.User{testUser1, testUser2},
			CreateAt: time.Now(),
		})
	}

	roomRepo := newFakeRoomRepo(rooms...)
	userRepo := newFakeUserRepo(testUser1, testUser2)
	srv := newTestServer(Config{}, userRepo, roomRepo)

	invites := newFakeInviteStore()
	srv.SetInvites(invites)
	notifier := newFakeNotifier()
	srv.SetNotifier(notifier)

	return &inviteFixture{
		srv: srv, invites: invites, notifier: notifier,
		roomRepo: roomRepo, room: room,
		token: mustToken(t, srv, testUser1.ID),
	}
}

func (f *inviteFixture) addMember(t *testing.T, userId int) *httpRecorder {
	t.Helper()
	target := fmt.Sprintf("/api/v1/rooms/%s/members", f.room.ID.Hex())
	rec := doRequest(t, f.srv, http.MethodPost, target, f.token, fmt.Sprintf(`{"userId":%d}`, userId))
	return &httpRecorder{t: t, rec: rec}
}

// httpRecorder — тонкая обёртка для читаемых проверок ответа.
type httpRecorder struct {
	t   *testing.T
	rec interface{ Result() *http.Response }
}

func (h *httpRecorder) expect(status int) addMemberResponse {
	h.t.Helper()
	res := h.rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != status {
		h.t.Fatalf("ожидался статус %d, получен %d", status, res.StatusCode)
	}
	var out addMemberResponse
	if status == http.StatusOK || status == http.StatusAccepted {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			h.t.Fatalf("не удалось разобрать ответ: %v", err)
		}
	}
	return out
}

// waitInvited ждёт уведомление о приглашении (оно уходит фоновой горутиной).
func waitInvited(t *testing.T, n *fakeNotifier) notifierCall {
	t.Helper()
	select {
	case c := <-n.calls:
		if c.event != "invited" {
			t.Fatalf("ожидалось уведомление invited, получено %q", c.event)
		}
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("уведомление о приглашении не пришло")
		return notifierCall{}
	}
}

func expectNoNotification(t *testing.T, n *fakeNotifier) {
	t.Helper()
	select {
	case c := <-n.calls:
		t.Fatalf("уведомления быть не должно, пришло %q", c.event)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAddMemberAddsFriend(t *testing.T) {
	f := newInviteFixture(t, true)

	got := f.addMember(t, testUser2.ID).expect(http.StatusOK)
	if got.Status != api.InviteAdded {
		t.Fatalf("ожидался статус added, получен %q", got.Status)
	}

	if !isRoomMember(f.room, testUser2.ID) {
		t.Fatal("приглашённый не стал участником комнаты")
	}
	inv, err := f.invites.Find(context.Background(), f.room.ID, testUser2.ID)
	if err != nil {
		t.Fatalf("запись приглашения не создана: %v", err)
	}
	if inv.Status != api.InviteAdded || inv.InviterID != testUser1.ID {
		t.Fatalf("запись приглашения неверная: %+v", inv)
	}

	call := waitInvited(t, f.notifier)
	if call.invitee.ID != testUser2.ID || call.isReturn {
		t.Fatalf("уведомление неверное: %+v", call)
	}
}

// TestAddMemberIdempotent — повторный вызов не плодит участников и, главное,
// НЕ шлёт второй push: иначе человека засыпало бы уведомлениями при ретраях.
func TestAddMemberIdempotent(t *testing.T) {
	f := newInviteFixture(t, true)

	f.addMember(t, testUser2.ID).expect(http.StatusOK)
	waitInvited(t, f.notifier)

	f.addMember(t, testUser2.ID).expect(http.StatusOK)
	expectNoNotification(t, f.notifier)

	var count int
	for _, m := range roomMembers(f.room) {
		if m.ID == testUser2.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("участник продублировался: встречается %d раз", count)
	}
}

func TestAddMemberRejectsStranger(t *testing.T) {
	// Общей комнаты нет — значит и в /friends человека нет.
	f := newInviteFixture(t, false)

	f.addMember(t, testUser2.ID).expect(http.StatusForbidden)

	if isRoomMember(f.room, testUser2.ID) {
		t.Fatal("посторонний попал в комнату")
	}
	expectNoNotification(t, f.notifier)
}

// TestAddMemberReinviteAfterLeaveWithSingleSharedRoom — обязательный тест из
// плана. /friends строится из ТЕКУЩИХ участников, поэтому вышедший перестаёт
// быть другом, если общая комната была единственной. Без второй ветки проверки
// связи («есть запись по этой комнате») повторное приглашение было бы
// недостижимо в самом типовом случае — и фикстура со ВТОРОЙ общей комнатой
// замаскировала бы дефект, поэтому здесь её сознательно нет.
func TestAddMemberReinviteAfterLeaveWithSingleSharedRoom(t *testing.T) {
	f := newInviteFixture(t, false)

	// Человек когда-то был в этой комнате и вышел: он НЕ участник и НЕ друг.
	if err := f.invites.Upsert(context.Background(), f.room.ID, testUser2.ID, testUser1.ID, api.InviteLeft, time.Now()); err != nil {
		t.Fatalf("подготовка записи left: %v", err)
	}

	got := f.addMember(t, testUser2.ID).expect(http.StatusAccepted)
	if got.Status != api.InvitePending {
		t.Fatalf("ожидался статус pending, получен %q", got.Status)
	}
	if isRoomMember(f.room, testUser2.ID) {
		t.Fatal("вышедшего вернули в комнату молча, без его согласия")
	}

	call := waitInvited(t, f.notifier)
	if !call.isReturn {
		t.Fatal("уведомление должно быть про возврат, а не про добавление")
	}
}

// TestAddMemberReconcilesSelfJoinedViaLink — человек вошёл сам по ссылке
// /join/{roomId}, которая про приглашения ничего не знает. Без примирения он
// остался бы участником со статусом pending навсегда, и следующее приглашение
// повело бы себя как для вышедшего.
func TestAddMemberReconcilesSelfJoinedViaLink(t *testing.T) {
	f := newInviteFixture(t, true)

	if err := f.invites.Upsert(context.Background(), f.room.ID, testUser2.ID, testUser1.ID, api.InvitePending, time.Now()); err != nil {
		t.Fatalf("подготовка записи pending: %v", err)
	}
	// Вошёл сам по ссылке.
	if err := f.roomRepo.JoinToRoom(context.Background(), testUser2, f.room.ID.Hex()); err != nil {
		t.Fatalf("вход по ссылке: %v", err)
	}

	got := f.addMember(t, testUser2.ID).expect(http.StatusOK)
	if got.Status != api.InviteAdded {
		t.Fatalf("ожидался статус added, получен %q", got.Status)
	}

	inv, err := f.invites.Find(context.Background(), f.room.ID, testUser2.ID)
	if err != nil {
		t.Fatalf("запись приглашения пропала: %v", err)
	}
	if inv.Status != api.InviteAdded {
		t.Fatalf("запись не примирена: статус %q вместо added", inv.Status)
	}
}

// TestAddMemberRecoversAfterUpsertFailure — членство записалось, а запись
// отношения нет (упал второй шаг). Повторный вызов обязан довести дело до
// конца, иначе человек остался бы в группе навсегда без карточки.
func TestAddMemberRecoversAfterUpsertFailure(t *testing.T) {
	f := newInviteFixture(t, true)

	// Имитируем состояние после сбоя: участник есть, записи нет.
	if err := f.roomRepo.JoinToRoom(context.Background(), testUser2, f.room.ID.Hex()); err != nil {
		t.Fatalf("подготовка членства: %v", err)
	}
	if _, err := f.invites.Find(context.Background(), f.room.ID, testUser2.ID); err != mongo.ErrNoDocuments {
		t.Fatalf("подготовка неверна: запись не должна существовать, получено %v", err)
	}

	f.addMember(t, testUser2.ID).expect(http.StatusOK)

	inv, err := f.invites.Find(context.Background(), f.room.ID, testUser2.ID)
	if err != nil {
		t.Fatalf("повторный вызов не создал запись: %v", err)
	}
	if inv.Status != api.InviteAdded {
		t.Fatalf("статус после восстановления: %q", inv.Status)
	}
}

func TestAddMemberValidation(t *testing.T) {
	f := newInviteFixture(t, true)

	tests := []struct {
		name   string
		userId int
		status int
	}{
		{"нулевой userId", 0, http.StatusBadRequest},
		{"сам себя", testUser1.ID, http.StatusBadRequest},
		{"несуществующий", 999, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.addMember(t, tt.userId).expect(tt.status)
		})
	}
}

// TestAddMemberRejectsDeletedInvitee — удалённые остаются во встроенных снимках
// комнат (анонимизированными), поэтому попадают и в /friends. Приглашать их
// некуда: аккаунта больше нет.
func TestAddMemberRejectsDeletedInvitee(t *testing.T) {
	deleted := testUser2
	now := time.Now()
	deleted.DeletedAt = &now

	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Поездка",
		Members: &[]api.User{testUser1}, CreateAt: time.Now(),
	}
	shared := &api.Room{
		ID: primitive.NewObjectID(), Name: "Квартира",
		Members: &[]api.User{testUser1, testUser2}, CreateAt: time.Now(),
	}
	roomRepo := newFakeRoomRepo(room, shared)
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, deleted), roomRepo)
	srv.SetInvites(newFakeInviteStore())

	token := mustToken(t, srv, testUser1.ID)
	target := fmt.Sprintf("/api/v1/rooms/%s/members", room.ID.Hex())
	rec := doRequest(t, srv, http.MethodPost, target, token, fmt.Sprintf(`{"userId":%d}`, deleted.ID))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ожидался 404 для удалённого, получен %d", rec.Code)
	}
}

// TestAddMemberRequiresMembership — не участник комнаты не может звать в неё
// людей: иначе посторонний открывал бы доступ к чужим данным.
func TestAddMemberRequiresMembership(t *testing.T) {
	f := newInviteFixture(t, true)
	outsiderToken := mustToken(t, f.srv, testUser2.ID)

	target := fmt.Sprintf("/api/v1/rooms/%s/members", f.room.ID.Hex())
	rec := doRequest(t, f.srv, http.MethodPost, target, outsiderToken, `{"userId":1}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403 для не-участника, получен %d", rec.Code)
	}
}
