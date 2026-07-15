package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// комната с тремя участниками и без операций — для создания itemized-расходов
func newItemizedRoom() *api.Room {
	return &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "AI room",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &[]api.Operation{},
	}
}

func TestCreateItemized_ServerDerivesSums(t *testing.T) {
	room := newItemizedRoom()
	rr := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), rr)

	// пицца 300 на 1 и 2 поровну; клиентский sum намеренно врёт (999)
	body := `{
	  "description":"Ужин","donorId":1,"sum":999,
	  "items":[{"name":"Пицца","price":300,"qty":1,"kind":"item",
	    "shares":[{"userId":1,"weight":1},{"userId":2,"weight":1}]}]
	}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	if op.Sum != 300 {
		t.Fatalf("сервер не пересчитал Sum из позиций: %d, want 300", op.Sum)
	}
	if recipientSum(op, 1) != 150 || recipientSum(op, 2) != 150 {
		t.Fatalf("доли неверны: %+v", op.Recipients)
	}
	// read-path: позиции вернулись
	if len(op.Items) != 1 || op.Items[0].Name != "Пицца" {
		t.Fatalf("read-path не вернул items: %+v", op.Items)
	}
	// в хранилище splitType by_exact_amount и Items сохранены
	saved := (*rr.rooms[room.ID.Hex()].Operations)[0]
	if saved.SplitType != splitByExactAmount || len(saved.Items) != 1 {
		t.Fatalf("операция сохранена без items/splitType: %+v", saved)
	}
}

func TestCreateItemized_UnknownRejected(t *testing.T) {
	room := newItemizedRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	body := `{
	  "description":"Ужин","donorId":1,"sum":300,
	  "items":[{"name":"Пиво","price":200,"kind":"item","unknown":["Саня"],
	    "shares":[{"userId":1,"weight":1}]}]
	}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (Unknown непустой)", rec.Code)
	}
}

func TestCreateItemized_ForeignUserInShareRejected(t *testing.T) {
	room := newItemizedRoom() // 1,2,3
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	body := `{
	  "description":"Ужин","donorId":1,"sum":300,
	  "items":[{"name":"Пицца","price":300,"kind":"item",
	    "shares":[{"userId":1,"weight":1},{"userId":99,"weight":1}]}]
	}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (чужой userId)", rec.Code)
	}
}

func TestCreateItemized_HugePriceRejectedFast(t *testing.T) {
	// price близок к границе int64: без ограничений величин DeriveShares ушёл бы
	// в почти бесконечную раздачу остатка. Ждём быстрый 400, а не зависание.
	room := newItemizedRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	token := mustToken(t, s, testUser1.ID)
	body := `{
	  "description":"Ужин","donorId":1,"sum":0,
	  "items":[{"name":"Атака","price":6917529027641081856,"kind":"item",
	    "shares":[{"userId":1,"weight":1},{"userId":2,"weight":1}]}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (величина вне диапазона), body %s", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("создание itemized-операции зависло на огромной цене")
	}
}

func TestCreateItemized_HugeWeightRejectedFast(t *testing.T) {
	room := newItemizedRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	body := `{
	  "description":"Ужин","donorId":1,"sum":0,
	  "items":[{"name":"Атака","price":300,"kind":"item",
	    "shares":[{"userId":1,"weight":1000000000},{"userId":2,"weight":1}]}]
	}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (вес вне диапазона), body %s", rec.Code, rec.Body.String())
	}
}

func TestCreateItemized_ZeroSumRejected(t *testing.T) {
	// позиция price:0 с пустыми shares → total 0, нет получателей: не сохраняем как активную
	room := newItemizedRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	body := `{"description":"пусто","donorId":1,"items":[{"name":"free","price":0,"kind":"item","shares":[]}]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (нулевой расход без получателей)", rec.Code)
	}
}

func TestCreatePlainStillWorks(t *testing.T) {
	// обычная операция без items — прежнее поведение, Items не появляется
	room := newItemizedRoom()
	rr := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), rr)
	body := `{"description":"Такси","donorId":1,"sum":200,"recipientIds":[1,2]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	if len(op.Items) != 0 {
		t.Fatalf("у обычной операции не должно быть items: %+v", op.Items)
	}
	saved := (*rr.rooms[room.ID.Hex()].Operations)[0]
	if saved.Items != nil {
		t.Fatalf("Items должен быть nil у обычной операции")
	}
}

func TestCreateItemized_FullReceipt(t *testing.T) {
	// полный чек: суммы должны сойтись с тем, что считает ядро (DeriveShares)
	room := newItemizedRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	body := `{
	  "description":"Ресторан","donorId":1,"sum":0,
	  "items":[
	    {"name":"Пицца","price":300,"kind":"item","shares":[{"userId":1,"weight":1},{"userId":2,"weight":1}]},
	    {"name":"Сбор","price":30,"kind":"surcharge","split":"proportional"}
	  ]
	}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	// пицца 150/150 + сбор 30 пополну 15/15 → 165/165, sum 330
	if op.Sum != 330 || recipientSum(op, 1) != 165 || recipientSum(op, 2) != 165 {
		t.Fatalf("итог неверен: sum=%d recips=%+v", op.Sum, op.Recipients)
	}
	// инвариант: сумма долей == sum
	total := 0
	for _, r := range op.Recipients {
		total += r.Sum
	}
	if total != op.Sum {
		t.Fatalf("инвариант нарушен: Σ=%d, sum=%d", total, op.Sum)
	}
}
