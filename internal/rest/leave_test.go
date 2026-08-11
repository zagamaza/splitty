package rest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// leaveFixture — комната с тремя участниками без операций.
type leaveFixture struct {
	srv     *Server
	invites *fakeInviteStore
	room    *api.Room
}

func newLeaveFixture(t *testing.T, ops ...api.Operation) *leaveFixture {
	t.Helper()
	room := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Квартира",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &ops,
		CreateAt:   time.Now(),
	}
	roomRepo := newFakeRoomRepo(room)
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), roomRepo)
	invites := newFakeInviteStore()
	srv.SetInvites(invites)
	return &leaveFixture{srv: srv, invites: invites, room: room}
}

func (f *leaveFixture) leave(t *testing.T, userId int) int {
	t.Helper()
	token := mustToken(t, f.srv, userId)
	target := fmt.Sprintf("/api/v1/rooms/%s/members/me", f.room.ID.Hex())
	return doRequest(t, f.srv, http.MethodDelete, target, token, "").Code
}

func (f *leaveFixture) remove(t *testing.T, actorId, targetId int) int {
	t.Helper()
	token := mustToken(t, f.srv, actorId)
	target := fmt.Sprintf("/api/v1/rooms/%s/members/%d", f.room.ID.Hex(), targetId)
	return doRequest(t, f.srv, http.MethodDelete, target, token, "").Code
}

func TestLeaveRoomWithoutOperations(t *testing.T) {
	f := newLeaveFixture(t)

	if code := f.leave(t, testUser2.ID); code != http.StatusNoContent {
		t.Fatalf("ожидался 204, получен %d", code)
	}
	if isRoomMember(f.room, testUser2.ID) {
		t.Fatal("вышедший остался участником")
	}

	inv, err := f.invites.Find(context.Background(), f.room.ID, testUser2.ID)
	if err != nil {
		t.Fatalf("запись left не создана: %v", err)
	}
	if inv.Status != api.InviteLeft {
		t.Fatalf("статус после выхода: %q вместо left", inv.Status)
	}
}

// TestLeaveRoomBlockedByLegacyOperation — ОБЯЗАТЕЛЬНЫЙ тест из плана.
//
// Легаси-операции эпохи бота хранят recipients без recipients_with_sum и без
// status; доли синтезируются только при нормализации, в памяти. Если проверять
// участие фильтром mongo или по сырым данным, такие долги окажутся невидимы —
// и человек с реальной задолженностью спокойно выйдет, а кредитор молча
// потеряет деньги.
func TestLeaveRoomBlockedByLegacyOperation(t *testing.T) {
	donor := testUser1
	legacy := api.Operation{
		ID:          primitive.NewObjectID(),
		Description: "Ужин",
		Sum:         100,
		Donor:       &donor,
		// Ни recipients_with_sum, ни status — ровно как в старых данных.
		Recipients: &[]api.User{testUser1, testUser2},
		CreateAt:   time.Now(),
	}
	f := newLeaveFixture(t, legacy)

	code := f.leave(t, testUser2.ID)
	if code != http.StatusConflict {
		t.Fatalf("выход с легаси-долгом должен быть отклонён 409, получен %d", code)
	}
	if !isRoomMember(f.room, testUser2.ID) {
		t.Fatal("человека убрали из комнаты, несмотря на долг")
	}
}

// TestLeaveRoomBlockedForDonor — плательщик тоже заблокирован: он участник
// операции, даже если его нет среди получателей.
func TestLeaveRoomBlockedForDonor(t *testing.T) {
	donor := testUser2
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Такси", Sum: 60,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 60}},
		CreateAt:          time.Now(),
	}
	f := newLeaveFixture(t, op)

	if code := f.leave(t, testUser2.ID); code != http.StatusConflict {
		t.Fatalf("плательщик должен быть заблокирован 409, получен %d", code)
	}
}

func TestRemoveMemberWithoutOperations(t *testing.T) {
	f := newLeaveFixture(t)

	if code := f.remove(t, testUser1.ID, testUser3.ID); code != http.StatusNoContent {
		t.Fatalf("ожидался 204, получен %d", code)
	}
	if isRoomMember(f.room, testUser3.ID) {
		t.Fatal("удалённый остался участником")
	}

	// Запись left обязана быть про УБРАННОГО, а не про убравшего: перепутав их,
	// мы оставили бы ушедшего без следа отношения (следующее приглашение вернёт
	// его молча) и повесили бы left на оставшегося участника.
	ctx := context.Background()
	inv, err := f.invites.Find(ctx, f.room.ID, testUser3.ID)
	if err != nil {
		t.Fatalf("записи left про убранного нет: %v", err)
	}
	if inv.Status != api.InviteLeft {
		t.Fatalf("статус убранного: %q вместо left", inv.Status)
	}
	if inv.InviterID != testUser1.ID {
		t.Fatalf("в записи должен остаться убравший (%d), получен %d", testUser1.ID, inv.InviterID)
	}
	if _, err := f.invites.Find(ctx, f.room.ID, testUser1.ID); err == nil {
		t.Fatal("убравшему записали отношение — он из комнаты не выходил")
	}
}

// TestRemoveMemberThenInviteAsksConsent — сквозной смысл записи left: убранного
// нельзя вернуть молча. Повторное приглашение обязано ждать его решения (202
// pending), а не добавлять в комнату (200 added).
func TestRemoveMemberThenInviteAsksConsent(t *testing.T) {
	f := newLeaveFixture(t)

	if code := f.remove(t, testUser1.ID, testUser3.ID); code != http.StatusNoContent {
		t.Fatalf("удаление участника: ожидался 204, получен %d", code)
	}

	token := mustToken(t, f.srv, testUser1.ID)
	target := fmt.Sprintf("/api/v1/rooms/%s/members", f.room.ID.Hex())
	body := fmt.Sprintf(`{"userId":%d}`, testUser3.ID)
	rec := doRequest(t, f.srv, http.MethodPost, target, token, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("после удаления приглашение обязано ждать согласия (202), получен %d (%s)",
			rec.Code, rec.Body.String())
	}
	if isRoomMember(f.room, testUser3.ID) {
		t.Fatal("убранного вернули в комнату без его согласия")
	}
}

func TestRemoveMemberBlockedByOperations(t *testing.T) {
	donor := testUser1
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Продукты", Sum: 90,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser3, Sum: 90}},
		CreateAt:          time.Now(),
	}
	f := newLeaveFixture(t, op)

	if code := f.remove(t, testUser1.ID, testUser3.ID); code != http.StatusConflict {
		t.Fatalf("удаление должника должно быть отклонено 409, получен %d", code)
	}
}

// TestLeaveRoomLastMemberBlocked — комната без участников осталась бы
// бесхозной, а удаления комнаты в REST нет: только персональный архив.
func TestLeaveRoomLastMemberBlocked(t *testing.T) {
	room := &api.Room{
		ID: primitive.NewObjectID(), Name: "Соло",
		Members: &[]api.User{testUser1}, CreateAt: time.Now(),
	}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(room))
	srv.SetInvites(newFakeInviteStore())

	token := mustToken(t, srv, testUser1.ID)
	target := fmt.Sprintf("/api/v1/rooms/%s/members/me", room.ID.Hex())
	rec := doRequest(t, srv, http.MethodDelete, target, token, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("последний участник не должен выходить, ожидался 409, получен %d", rec.Code)
	}
}

func TestRemoveMemberSelfRedirectedToMe(t *testing.T) {
	f := newLeaveFixture(t)
	if code := f.remove(t, testUser1.ID, testUser1.ID); code != http.StatusBadRequest {
		t.Fatalf("удаление себя через {userId} должно давать 400, получен %d", code)
	}
}

func TestRemoveMemberRequiresMembership(t *testing.T) {
	f := newLeaveFixture(t)
	outsider := api.User{ID: 99, DisplayName: "Чужой"}
	srv := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, outsider), newFakeRoomRepo(f.room))
	srv.SetInvites(newFakeInviteStore())

	token := mustToken(t, srv, outsider.ID)
	target := fmt.Sprintf("/api/v1/rooms/%s/members/%d", f.room.ID.Hex(), testUser2.ID)
	if rec := doRequest(t, srv, http.MethodDelete, target, token, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("посторонний не может убирать участников, ожидался 403, получен %d", rec.Code)
	}
}

func TestRemoveMemberNotInRoom(t *testing.T) {
	f := newLeaveFixture(t)
	if code := f.remove(t, testUser1.ID, 999); code != http.StatusNotFound {
		t.Fatalf("несуществующий участник — 404, получен %d", code)
	}
}

// TestLeaveRoomKeepsMembershipWhenLeftRecordFails — выход без записи left
// открывал бы тихий возврат: следующее приглашение не увидело бы прошлого
// выхода и вернуло бы человека в комнату без спроса. Поэтому сбой записи
// отменяет весь выход.
func TestLeaveRoomKeepsMembershipWhenLeftRecordFails(t *testing.T) {
	f := newLeaveFixture(t)
	f.invites.upsertErr = errors.New("mongo недоступна")

	if code := f.leave(t, testUser2.ID); code != http.StatusInternalServerError {
		t.Fatalf("сбой записи left обязан отдавать 500, получен %d", code)
	}
	if !isRoomMember(f.room, testUser2.ID) {
		t.Fatal("человек вышел из комнаты, но следа отношения не осталось — следующее приглашение вернёт его молча")
	}
}

// TestLeaveRoomKeepsOthersDebts — инвариант: уход человека без операций не
// должен менять расчёты остальных.
func TestLeaveRoomKeepsOthersDebts(t *testing.T) {
	donor := testUser1
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:             &donor,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
		CreateAt:          time.Now(),
	}
	f := newLeaveFixture(t, op)

	before, _ := service.GetRoomDebts(normalizedRoom(f.room))
	if code := f.leave(t, testUser3.ID); code != http.StatusNoContent {
		t.Fatalf("выход участника без операций отклонён: %d", code)
	}
	after, _ := service.GetRoomDebts(normalizedRoom(f.room))

	if len(before) != len(after) {
		t.Fatalf("число долгов изменилось: было %d, стало %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Sum != after[i].Sum ||
			before[i].Debtor.ID != after[i].Debtor.ID ||
			before[i].Lender.ID != after[i].Lender.ID {
			t.Fatalf("долг #%d изменился: было %+v, стало %+v", i, before[i], after[i])
		}
	}
}
