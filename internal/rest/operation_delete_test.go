package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Удаление расхода через API: снаружи операция исчезает, внутри остаётся.
//
// Удалить расход в общей группе может любой участник, поэтому цена ошибки —
// чужие данные. Мягкое удаление делает её обратимой, но только если операция
// действительно осталась в документе, а не просто перестала показываться.

func TestDeleteOperationHidesButKeepsRecord(t *testing.T) {
	room := newTestRoom()
	repo := newFakeRoomRepo(room)
	operationId := (*room.Operations)[0].ID
	before := len(*room.Operations)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	rec := doRequest(t, s, http.MethodDelete,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId.Hex()),
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}

	stored := repo.rooms[room.ID.Hex()]
	if got := len(*stored.Operations); got != before {
		t.Fatalf("операций в комнате %d, было %d — расход вырезали, восстанавливать нечего", got, before)
	}
	if st := storedOperationStatus(t, stored, operationId); st != api.StatusArchive {
		t.Fatalf("статус удалённой операции %q, ожидался archive", st)
	}
	if ops := api.ActiveOperations(stored); len(ops) != before-1 {
		t.Fatalf("активных операций %d, ожидалось %d — удалённый расход виден", len(ops), before-1)
	}

	// со стороны клиента расхода в комнате больше нет
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(),
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, operationId.Hex()) {
		t.Fatalf("удалённая операция осталась в ответе комнаты: %s", body)
	}
}

// Повторное удаление — 404, а не молчаливый успех: иначе клиент, повторивший
// запрос после обрыва связи, не отличит «удалено» от «удалять было нечего».
func TestDeleteOperationTwiceIsNotFound(t *testing.T) {
	room := newTestRoom()
	operationId := (*room.Operations)[0].ID
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	path := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId.Hex())

	if rec := doRequest(t, s, http.MethodDelete, path, mustToken(t, s, testUser1.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("первое удаление: status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	rec := doRequest(t, s, http.MethodDelete, path, mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("повторное удаление: status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

// Гонка «двое удаляют один расход». Комнату мы уже прочитали, операция в ней
// была — но пока запрос шёл до записи, её удалил другой участник. Без ответа
// репозитория обработчик отдал бы 204, и клиент показал бы удаление успешным,
// хотя удалил его кто-то другой.
func TestDeleteOperationLostRaceIsNotFound(t *testing.T) {
	room := newTestRoom()
	repo := newFakeRoomRepo(room)
	operationId := (*room.Operations)[0].ID
	// конкурент успевает первым, между чтением комнаты и нашей записью
	repo.beforeDelete = func(roomId string, id primitive.ObjectID) {
		repo.beforeDelete = nil
		if _, err := repo.DeleteOperation(context.Background(), roomId, id); err != nil {
			t.Errorf("конкурентное удаление не прошло: %v", err)
		}
	}
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	rec := doRequest(t, s, http.MethodDelete,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId.Hex()),
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

// Удалённый расход не участвует в долгах.
func TestDeleteOperationRemovesDebt(t *testing.T) {
	room := newTestRoom()
	operationId := (*room.Operations)[0].ID
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodDelete,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId.Hex()),
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex()+"/debts",
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var debts []struct {
		Sum int `json:"sum"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &debts); err != nil {
		t.Fatalf("долги не разобрались: %v, тело: %s", err, rec.Body.String())
	}
	if len(debts) != 0 {
		t.Fatalf("после удаления единственного расхода остались долги: %s", rec.Body.String())
	}
}

func storedOperationStatus(t *testing.T, room *api.Room, id primitive.ObjectID) api.OperationStatus {
	t.Helper()
	if room == nil || room.Operations == nil {
		t.Fatal("комната без операций")
	}
	for _, op := range *room.Operations {
		if op.ID == id {
			return op.Status
		}
	}
	t.Fatal("операция пропала из комнаты")
	return ""
}
