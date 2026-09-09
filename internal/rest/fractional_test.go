package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"fmt"
	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	// Серверный рубильник ставится из конфига сервера (см. NewServer).
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", false)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), repo)

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
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

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

// Пока рубильник выключен, настройка копеек не записывается вовсе.
//
// В клиентах переключатель скрыт, но прямой запрос молча положил бы в документ
// «копейки включены», ответ вернул бы «выключены» — и настройка сработала бы
// сама в тот день, когда рубильник поднимут.
func TestFractionalSettingRejectedWhileFlagOff(t *testing.T) {
	room := fractionalRoom("USD", false)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/fractional", token, `{"fractional":true}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	saved := repo.rooms[room.ID.Hex()]
	if saved.FractionalAmounts != nil && *saved.FractionalAmounts {
		t.Error("настройка записалась при выключенном рубильнике — включится сама при его подъёме")
	}

	// Долларовая туса отдаётся как туса БЕЗ копеек, хотя валюта включает их
	// умолчанием: иначе клиент показал бы дробную клавиатуру, а сервер отверг бы
	// дробную сумму.
	detail := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), token, "")
	var dto roomDetailDto
	if err := json.Unmarshal(detail.Body.Bytes(), &dto); err != nil {
		t.Fatalf("не разобрал комнату: %v", err)
	}
	if dto.Fractional {
		t.Error("долларовая туса объявлена дробной при выключенном рубильнике")
	}
}

// Сборка, не умеющая копеек, не должна их стирать.
//
// Она шлёт только округлённое sum, и правка описания у расхода 20,80 записала бы
// 21: человек на другом телефоне увидел бы, что сумма изменилась сама.
func TestLegacyEditCannotEraseFraction(t *testing.T) {
	api.SetFractionalInput(true)
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", true)
	minor := int64(2080)
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 21, SumMinor: &minor,
		Donor: &testUser1, Status: statusActive, SplitType: splitEqually,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: testUser1, SumMinor: ptr64(1040)},
			{User: testUser2, SumMinor: ptr64(1040)},
		},
	}
	ops := append(*room.Operations, op)
	room.Operations = &ops
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	token := mustToken(t, s, testUser1.ID)

	body := fmt.Sprintf(
		`{"description":"Ужин с друзьями","sum":21,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec := doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// Новая сборка шлёт точную величину — правка проходит.
	body = fmt.Sprintf(
		`{"description":"Ужин с друзьями","sumMinor":2080,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("правка с точной суммой = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

// Откат рубильника не превращает уже записанные дробные расходы в неправимые.
//
// Сумму такого расхода не сохранить (дробь запрещена признаком), а без неё
// сервер отвечает 409 «обновите приложение» — человек не смог бы даже
// переименовать расход. Признак поднимается для того, что УЖЕ дробное, и новых
// дробей это не создаёт.
func TestFractionalOperationStaysEditableAfterRollback(t *testing.T) {
	// Рубильник ВЫКЛЮЧЕН: дробь записана раньше, до отката.
	room := fractionalRoom("USD", false)
	minor := int64(2080)
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 21, SumMinor: &minor,
		Donor: &testUser1, Status: statusActive, SplitType: splitEqually,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: testUser1, SumMinor: ptr64(1040)},
			{User: testUser2, SumMinor: ptr64(1040)},
		},
	}
	ops := append(*room.Operations, op)
	room.Operations = &ops
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)
	token := mustToken(t, s, testUser1.ID)

	body := fmt.Sprintf(
		`{"description":"Ужин с друзьями","sum":21,"sumMinor":2080,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec := doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("правка дробного расхода при выключенном рубильнике: %d, body: %s", rec.Code, rec.Body.String())
	}

	// А НОВУЮ дробь в той же тусе завести по-прежнему нельзя.
	body = fmt.Sprintf(
		`{"description":"Кофе","sumMinor":1050,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations", token, body)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	// И деньги того самого расхода изменить нельзя: иначе выключенный
	// рубильник не останавливал бы появление новых дробных обязательств.
	body = fmt.Sprintf(
		`{"description":"Ужин","sumMinor":3090,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// Привести расход к ЦЕЛОМУ можно — это путь «починки вперёд» после отката.
	body = fmt.Sprintf(
		`{"description":"Ужин","sum":22,"sumMinor":2200,"donorId":%d,"recipientIds":[%d,%d]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("приведение дробного расхода к целому: %d, body: %s", rec.Code, rec.Body.String())
	}
}

// Дробные ДОЛИ при опущенном рубильнике тоже заморожены, даже когда итог целый.
//
// Расход 100 с долями 50,50 + 49,50 — итог целый, а обязательства дробные:
// правка долей меняла бы их, минуя выключенный рубильник.
func TestFractionalSharesFrozenAfterRollback(t *testing.T) {
	room := fractionalRoom("USD", false)
	total := int64(10000)
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100, SumMinor: &total,
		Donor: &testUser1, Status: statusActive, SplitType: splitByExactAmount,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: testUser1, SumMinor: ptr64(5050)},
			{User: testUser2, SumMinor: ptr64(4950)},
		},
	}
	ops := append(*room.Operations, op)
	room.Operations = &ops
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	token := mustToken(t, s, testUser1.ID)

	// Те же доли — переименование проходит.
	body := fmt.Sprintf(
		`{"description":"Ужин вдвоём","sum":100,"sumMinor":10000,"donorId":%d,`+
			`"recipientSums":[{"userId":%d,"sumMinor":5050},{"userId":%d,"sumMinor":4950}]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec := doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("переименование расхода с дробными долями: %d, body: %s", rec.Code, rec.Body.String())
	}

	// Смена плательщика при тех же долях — тоже отказ: у расхода 100 пополам
	// перенос доноров переворачивает позиции обоих, это другое обязательство,
	// а не переименование.
	body = fmt.Sprintf(
		`{"description":"Ужин вдвоём","sum":100,"sumMinor":10000,"donorId":%d,`+
			`"recipientSums":[{"userId":%d,"sumMinor":5050},{"userId":%d,"sumMinor":4950}]}`,
		testUser2.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// Другие доли — отказ.
	body = fmt.Sprintf(
		`{"description":"Ужин вдвоём","sum":100,"sumMinor":10000,"donorId":%d,`+
			`"recipientSums":[{"userId":%d,"sumMinor":6050},{"userId":%d,"sumMinor":3950}]}`,
		testUser1.ID, testUser1.ID, testUser2.ID)
	rec = doRequest(t, s, http.MethodPut,
		"/api/v1/rooms/"+room.ID.Hex()+"/operations/"+op.ID.Hex(), token, body)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")
}

// Чек в тусе с копейками делится ДО КОПЕЙКИ.
//
// Раньше позиции считались целыми единицами независимо от признака: расход из
// чека на 20,80 записывался как 21, и карточка расходилась с самим чеком.
func TestFractionalReceiptSplitsToCents(t *testing.T) {
	api.SetFractionalInput(true)
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", true)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	token := mustToken(t, s, testUser1.ID)

	// Кофе 10,40 и десерт 10,40 — по одному на каждого; итог 20,80.
	body := fmt.Sprintf(`{"description":"Ужин","donorId":%d,"items":[
		{"name":"Кофе","price":10,"priceMinor":1040,"qty":1,"kind":"item","shares":[{"userId":%d,"weight":1}]},
		{"name":"Десерт","price":10,"priceMinor":1040,"qty":1,"kind":"item","shares":[{"userId":%d,"weight":1}]}
	]}`, testUser1.ID, testUser1.ID, testUser2.ID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}
	if op.SumMinor == nil || *op.SumMinor != 2080 {
		t.Fatalf("итог = %v, want 2080 — чек округлился до единицы", op.SumMinor)
	}
	if op.Sum != 21 {
		t.Errorf("округлённая проекция = %d, want 21", op.Sum)
	}
	var total int64
	for _, r := range op.Recipients {
		if r.SumMinor == nil {
			t.Fatalf("доля %d без копеек", r.User.ID)
		}
		if *r.SumMinor != 1040 {
			t.Errorf("доля %d = %d, want 1040", r.User.ID, *r.SumMinor)
		}
		total += *r.SumMinor
	}
	if total != 2080 {
		t.Errorf("сумма долей = %d, want 2080", total)
	}
}

// В тусе БЕЗ копеек дробный чек по-прежнему отвергается — рубильник накрывает
// и этот вход.
func TestFractionalReceiptRejectedInWholeRoom(t *testing.T) {
	room := fractionalRoom("RUB", false)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	body := fmt.Sprintf(`{"description":"Ужин","donorId":%d,"items":[
		{"name":"Кофе","price":10,"priceMinor":1040,"qty":1,"kind":"item","shares":[{"userId":%d,"weight":1}]}
	]}`, testUser1.ID, testUser1.ID)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}
