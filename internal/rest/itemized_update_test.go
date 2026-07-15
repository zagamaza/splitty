package rest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// комната с готовой itemized-операцией (пицца 300 на 1 и 2, splitType by_exact_amount)
func roomWithItemizedOp() (*api.Room, primitive.ObjectID) {
	opId := primitive.NewObjectID()
	op := api.Operation{
		ID:          opId,
		Description: "Ужин",
		Sum:         300,
		Donor:       &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: testUser1, Sum: 150}, {User: testUser2, Sum: 150},
		},
		SplitType: splitByExactAmount,
		Status:    statusActive,
		Items: []api.OperationItem{{
			Name: "Пицца", Price: 300, Kind: api.ItemKindItem,
			Shares: []api.ItemShare{{UserId: 1, Weight: 1}, {UserId: 2, Weight: 1}},
		}},
	}
	room := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "AI room",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &[]api.Operation{op},
	}
	return room, opId
}

func TestUpdate_PlainClearsItems(t *testing.T) {
	room, opId := roomWithItemizedOp()
	rr := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), rr)

	// плоское обновление БЕЗ items → позиции должны быть затёрты
	body := `{"description":"Ужин исправлен","donorId":1,"sum":400,"recipientIds":[1,2]}`
	url := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), opId.Hex())
	rec := doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	if len(op.Items) != 0 {
		t.Fatalf("плоский PUT не затёр items: %+v", op.Items)
	}
	saved := (*rr.rooms[room.ID.Hex()].Operations)[0]
	if saved.Items != nil {
		t.Fatalf("в хранилище Items не очищены: %+v", saved.Items)
	}
	if saved.Sum != 400 {
		t.Fatalf("плоские суммы не сохранены: %d", saved.Sum)
	}
}

func TestUpdate_ItemizedUpdatesItems(t *testing.T) {
	room, opId := roomWithItemizedOp()
	rr := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), rr)

	// itemized обновление: другая раскладка → сервер пересчитывает суммы
	body := `{
	  "description":"Ужин","donorId":1,"sum":0,
	  "items":[{"name":"Пицца","price":300,"kind":"item",
	    "shares":[{"userId":1,"weight":2},{"userId":2,"weight":1}]}]
	}`
	url := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), opId.Hex())
	rec := doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	// 300 по весам 2:1 → 200/100
	if recipientSum(op, 1) != 200 || recipientSum(op, 2) != 100 {
		t.Fatalf("itemized PUT неверно пересчитал: %+v", op.Recipients)
	}
	saved := (*rr.rooms[room.ID.Hex()].Operations)[0]
	if len(saved.Items) != 1 || saved.Items[0].Shares[0].Weight != 2 {
		t.Fatalf("новые items не сохранены: %+v", saved.Items)
	}
}
