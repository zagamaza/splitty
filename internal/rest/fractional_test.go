package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// fractionalRoom — туса в заданной валюте с расходом 21 на двоих: доли 11 и 10.
// Неровное деление намеренно: на нём видно, сходятся ли доли с итогом.
func fractionalRoom(currency string, on bool) *api.Room {
	room := newTestRoom()
	room.Currency = currency
	v := on
	room.FractionalAmounts = &v
	ops := *room.Operations
	ops[0].Sum = 21
	ops[0].SplitType = api.SplitTypeByExactAmount
	ops[0].RecipientsWithSum = []api.RecipientWithSum{
		{User: testUser1, Sum: 11},
		{User: testUser2, Sum: 10},
	}
	room.Operations = &ops
	return room
}

func setFractional(t *testing.T, s *Server, roomId, body string) *roomDetailDto {
	t.Helper()
	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+roomId+"/fractional",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room %q: %v", rec.Body.String(), err)
	}
	return &detail
}

// Включение и выключение копеек — обычная настройка: суммы не меняются.
func TestSetFractionalDoesNotChangeAmounts(t *testing.T) {
	room := fractionalRoom("USD", false)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	before := readTotal(t, s, room.ID.Hex())

	on := setFractional(t, s, room.ID.Hex(), `{"fractional":true}`)
	if !on.Fractional {
		t.Error("признак не включился")
	}
	if on.TotalSpent != before {
		t.Errorf("сумма изменилась от включения копеек: %d, было %d", on.TotalSpent, before)
	}

	off := setFractional(t, s, room.ID.Hex(), `{"fractional":false}`)
	if off.Fractional {
		t.Error("признак не выключился")
	}
	if off.TotalSpent != before {
		t.Errorf("сумма изменилась от выключения копеек: %d, было %d", off.TotalSpent, before)
	}
}

func readTotal(t *testing.T, s *Server, roomId string) int {
	t.Helper()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+roomId, mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room: %v", err)
	}
	return detail.TotalSpent
}

// У иены дробной части нет: включить копейки нельзя, и отказ объясняет причину.
func TestSetFractionalRejectedForCurrencyWithoutIt(t *testing.T) {
	room := fractionalRoom("JPY", false)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/fractional",
		mustToken(t, s, testUser1.ID), `{"fractional":true}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Отсутствие поля — не то же самое, что «выключить».
func TestSetFractionalRequiresExplicitValue(t *testing.T) {
	room := fractionalRoom("USD", true)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/fractional",
		mustToken(t, s, testUser1.ID), `{}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Чужой в тусу не ходит.
func TestSetFractionalRequiresMembership(t *testing.T) {
	room := fractionalRoom("USD", false)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/fractional",
		mustToken(t, s, testUser3.ID), `{"fractional":true}`)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 404 или 403", rec.Code)
	}
}

// Смена валюты на ту, где дробной части нет, гасит признак — и не трогает суммы.
func TestChangeCurrencyTurnsFractionOff(t *testing.T) {
	room := fractionalRoom("USD", true)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)
	before := readTotal(t, s, room.ID.Hex())

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/currency",
		mustToken(t, s, testUser1.ID), `{"currency":"JPY"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	if api.RoomFractional(repo.rooms[room.ID.Hex()]) {
		t.Error("признак копеек остался включённым у иены")
	}
	if got := readTotal(t, s, room.ID.Hex()); got != before {
		t.Errorf("сумма изменилась от смены валюты: %d, было %d", got, before)
	}
}
