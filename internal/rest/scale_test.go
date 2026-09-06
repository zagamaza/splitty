package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// scaleRoom — комната в заданной валюте с расходом 21 на двоих: доли 11 и 10.
// Неровное деление взято намеренно: именно на нём видно, сходятся ли доли с
// итогом после смены шкалы.
//
// Шкала проставлена явно нулём, а не взята из умолчания валюты: у доллара
// умолчание двойка, и тест «включили копейки» на нём ничего бы не проверял.
func scaleRoom(currency string) *api.Room {
	room := newTestRoom()
	room.Currency = currency
	zero := 0
	room.DisplayExponent = &zero
	ops := *room.Operations
	ops[0].Sum = 21
	ops[0].RecipientsWithSum = []api.RecipientWithSum{
		{User: testUser1, Sum: 11},
		{User: testUser2, Sum: 10},
	}
	room.Operations = &ops
	return room
}

func setScale(t *testing.T, s *Server, roomId, body string) *roomDetailDto {
	t.Helper()
	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+roomId+"/scale", mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room %q: %v", rec.Body.String(), err)
	}
	return &detail
}

// Включение копеек ничего не теряет: суммы на вид те же, а версия шкалы
// выросла — по ней офлайн-очередь поймёт, что её снимок устарел.
func TestSetScaleUpIsExact(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	detail := setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)
	if detail.DisplayExponent != 2 {
		t.Errorf("displayExponent = %d, want 2", detail.DisplayExponent)
	}
	if detail.ScaleVersion != 1 {
		t.Errorf("scaleVersion = %d, want 1", detail.ScaleVersion)
	}
	if detail.TotalSpent != 21 {
		t.Errorf("totalSpent = %d, want 21 — на вид сумма не менялась", detail.TotalSpent)
	}

	op := (*repo.rooms[room.ID.Hex()].Operations)[0]
	if op.SumMinor == nil || *op.SumMinor != 2100 {
		t.Fatalf("sumMinor = %v, want 2100", op.SumMinor)
	}
	var shares int64
	for _, r := range op.RecipientsWithSum {
		if r.SumMinor == nil {
			t.Fatal("у доли нет минорного значения")
		}
		shares += *r.SumMinor
	}
	if shares != 2100 {
		t.Errorf("сумма долей = %d, want 2100", shares)
	}
}

// Выключение копеек округляет — и доли обязаны сойтись с округлённым итогом,
// иначе в группе появляются деньги, которых нет.
func TestSetScaleDownKeepsSharesConsistent(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	// 21.00 превращаем в 20.55, чтобы округление было заметным
	stored := repo.rooms[room.ID.Hex()]
	ops := *stored.Operations
	sum, a, b := int64(2055), int64(1028), int64(1027)
	ops[0].SumMinor = &sum
	ops[0].RecipientsWithSum[0].SumMinor = &a
	ops[0].RecipientsWithSum[1].SumMinor = &b

	detail := setScale(t, s, room.ID.Hex(), `{"displayExponent":0}`)
	if detail.DisplayExponent != 0 {
		t.Errorf("displayExponent = %d, want 0", detail.DisplayExponent)
	}
	if detail.ScaleVersion != 2 {
		t.Errorf("scaleVersion = %d, want 2", detail.ScaleVersion)
	}
	if detail.TotalSpent != 21 {
		t.Errorf("totalSpent = %d, want 21 (20.55 округляется вверх)", detail.TotalSpent)
	}

	op := (*repo.rooms[room.ID.Hex()].Operations)[0]
	var shares int64
	for _, r := range op.RecipientsWithSum {
		shares += *r.SumMinor
	}
	if shares != 21 {
		t.Errorf("сумма долей = %d, а итог 21 — деньги разошлись", shares)
	}
}

// У иены минорной единицы нет в обороте: включить копейки нельзя, и отказ
// обязан объяснять причину, а не просто «нельзя».
func TestSetScaleRejectedForCurrencyWithoutMinorUnit(t *testing.T) {
	room := scaleRoom("JPY")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/scale",
		mustToken(t, s, testUser1.ID), `{"displayExponent":2}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Отсутствие поля — не то же самое, что ноль: молча выключить копейки по
// пустому телу нельзя.
func TestSetScaleRequiresExplicitValue(t *testing.T) {
	room := scaleRoom("USD")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/scale",
		mustToken(t, s, testUser1.ID), `{}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Шкала за пределами валюты отвергается.
func TestSetScaleRejectsOutOfRange(t *testing.T) {
	room := scaleRoom("USD")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	for _, body := range []string{`{"displayExponent":3}`, `{"displayExponent":-1}`} {
		rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/scale",
			mustToken(t, s, testUser1.ID), body)
		assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	}
}

// Чужой в комнату со шкалой не ходит — как и во все прочие её ручки.
func TestSetScaleRequiresMembership(t *testing.T) {
	room := scaleRoom("USD")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/scale",
		mustToken(t, s, testUser3.ID), `{"displayExponent":2}`)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 404 или 403", rec.Code)
	}
}

// Чужая запись в комнату отменяет попытку — обработчик повторяет её сам.
func TestSetScaleRetriesWhenRoomBusy(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	repo.busyOnScale = 2
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	detail := setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)
	if detail.DisplayExponent != 2 {
		t.Errorf("displayExponent = %d, want 2 — повтор не сработал", detail.DisplayExponent)
	}
}

// Если комнату пишут без остановки, человек получает понятный отказ, а не
// молчание и не половину пересчитанной группы.
func TestSetScaleGivesUpWhenRoomStaysBusy(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	repo.busyOnScale = 10
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/scale",
		mustToken(t, s, testUser1.ID), `{"displayExponent":2}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")
}

// Комнату с копейками нельзя перевести в валюту, где копеек нет: записанное
// 2080 (20,80 $) стало бы 2080 иенами.
func TestChangeCurrencyRejectedWhenRoomHasCents(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/currency",
		mustToken(t, s, testUser1.ID), `{"currency":"JPY"}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	if got := repo.rooms[room.ID.Hex()].Currency; got != "USD" {
		t.Errorf("валюта = %q, want USD — отказ не должен ничего менять", got)
	}
}

// А без копеек — можно: число остаётся тем же, меняется обозначение.
func TestChangeCurrencyAllowedWithoutCents(t *testing.T) {
	room := scaleRoom("RUB")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+room.ID.Hex()+"/currency",
		mustToken(t, s, testUser1.ID), `{"currency":"JPY"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	if got := repo.rooms[room.ID.Hex()].Currency; got != "JPY" {
		t.Errorf("валюта = %q, want JPY", got)
	}
}
