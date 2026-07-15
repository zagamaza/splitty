package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

var (
	testUser1 = api.User{ID: 1, Username: "zagir", DisplayName: "Загир"}
	testUser2 = api.User{ID: 2, Username: "almaz", DisplayName: "Алмаз"}
	testUser3 = api.User{ID: 3, Username: "guest", DisplayName: "Гость"}
)

// newTestServer собирает Server на in-memory репозиториях
func newTestServer(cfg Config, userRepo *fakeUserRepo, roomRepo *fakeRoomRepo) *Server {
	return newTestServerWithLoginCodes(cfg, userRepo, roomRepo, newFakeLoginCodeRepo())
}

// newTestServerWithLoginCodes как newTestServer, но с заранее засеянными кодами входа
func newTestServerWithLoginCodes(cfg Config, userRepo *fakeUserRepo, roomRepo *fakeRoomRepo, codeRepo *fakeLoginCodeRepo) *Server {
	if cfg.JwtSecret == "" {
		cfg.JwtSecret = "test-secret"
	}
	return NewServer(cfg, userRepo, roomRepo, codeRepo,
		service.NewRoomService(roomRepo),
		service.NewOperationService(roomRepo))
}

// newTestRoom комната с участниками 1 и 2 и ЛЕГАСИ-операцией эпохи master-2021
// (recipients без recipients_with_sum, без status): 1 заплатил 100 за обоих →
// сервер синтезирует доли [50, 50], долг 2 → 1 равен 50
func newTestRoom() *api.Room {
	operation := api.Operation{
		ID:          primitive.NewObjectID(),
		Description: "Ужин",
		Sum:         100,
		Donor:       &testUser1,
		Recipients:  &[]api.User{testUser1, testUser2},
		CreateAt:    time.Now(),
	}
	return &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Тестовая комната",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{operation},
		CreateAt:   time.Now(),
	}
}

func doRequest(t *testing.T, s *Server, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body == "" {
		reqBody = strings.NewReader("")
	} else {
		reqBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reqBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func mustToken(t *testing.T, s *Server, userId int) string {
	t.Helper()
	token, err := s.issueToken(userId)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	return token
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot parse error body %q: %v", rec.Body.String(), err)
	}
	if resp.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, wantCode)
	}
}

func parseOperation(t *testing.T, rec *httptest.ResponseRecorder) operationDto {
	t.Helper()
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}
	return op
}

// recipientSum доля получателя userId в dto операции (-1 если получателя нет)
func recipientSum(op operationDto, userId int) int {
	for _, r := range op.Recipients {
		if r.User.ID == userId {
			return r.Sum
		}
	}
	return -1
}

// (а) доступ без токена → 401
func TestRequestWithoutTokenUnauthorized(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms", "", "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")

	rec = doRequest(t, s, http.MethodGet, "/api/v1/me", "garbage-token", "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// (б) не-участник комнаты → 403; легаси-операция синтезируется канонически
func TestRoomAccessForbiddenForNonMember(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), mustToken(t, s, testUser3.ID), "")
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	// участнику комната доступна
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("member access status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room detail: %v", err)
	}
	if detail.MyBalance != 50 {
		t.Errorf("myBalance = %d, want 50", detail.MyBalance)
	}
	if detail.TotalSpent != 100 {
		t.Errorf("totalSpent = %d, want 100", detail.TotalSpent)
	}
	if detail.MySpent != 50 {
		t.Errorf("mySpent = %d, want 50 (синтезированная доля легаси-операции)", detail.MySpent)
	}
	// у легаси-операции в базе нет recipients_with_sum — API отдаёт синтезированные доли
	if len(detail.Operations) != 1 {
		t.Fatalf("operations = %d, want 1", len(detail.Operations))
	}
	op := detail.Operations[0]
	if op.SplitType != "equally" {
		t.Errorf("splitType = %q, want equally (легаси)", op.SplitType)
	}
	if recipientSum(op, 1) != 50 || recipientSum(op, 2) != 50 {
		t.Errorf("legacy recipients = %+v, want доли [50, 50]", op.Recipients)
	}
}

// (в) редактирование: любой участник комнаты может, посторонний — нет
func TestUpdateForeignOperationForbidden(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), roomRepo)
	operationId := (*room.Operations)[0].ID.Hex()

	body := `{"description": "Ужин", "sum": 200, "donorId": 2, "recipientIds": [1, 2]}`
	url := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId)

	// посторонний (не участник комнаты) → 403
	rec := doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser3.ID), body)
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	// участник, но не донор операции — может (Splitwise-семантика)
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser2.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("member edit status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// донор тоже может редактировать
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("donor edit status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	// операция сохранена в модели develop: активная, с долями и типом деления
	saved := (*roomRepo.rooms[room.ID.Hex()].Operations)[0]
	if saved.Status != statusActive || saved.SplitType != splitEqually {
		t.Errorf("saved status/splitType = %q/%q, want active/equally", saved.Status, saved.SplitType)
	}
	if saved.Recipients != nil {
		t.Error("легаси-поле recipients должно обнуляться при обновлении")
	}
	if len(saved.RecipientsWithSum) != 2 || saved.RecipientsWithSum[0].Sum != 100 || saved.RecipientsWithSum[1].Sum != 100 {
		t.Errorf("saved recipientsWithSum = %+v, want доли [100, 100]", saved.RecipientsWithSum)
	}
}

// (г) погашение больше долга → 409
func TestRepaymentExceedingDebtConflict(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)
	url := fmt.Sprintf("/api/v1/rooms/%s/repayments", room.ID.Hex())
	token := mustToken(t, s, testUser2.ID)

	// долг user2 → user1 равен 50, гасим 60
	rec := doRequest(t, s, http.MethodPost, url, token, `{"debtorId": 2, "lenderId": 1, "sum": 60}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// долга user1 → user2 нет вовсе
	rec = doRequest(t, s, http.MethodPost, url, mustToken(t, s, testUser1.ID), `{"debtorId": 1, "lenderId": 2, "sum": 10}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// погашение в пределах долга проходит
	rec = doRequest(t, s, http.MethodPost, url, token, `{"debtorId": 2, "lenderId": 1, "sum": 50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid repayment status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	if !op.IsDebtRepayment || op.Sum != 50 || op.Donor.ID != 2 {
		t.Errorf("unexpected repayment operation: %+v", op)
	}
	if op.SplitType != "" {
		t.Errorf("repayment splitType = %q, want пусто", op.SplitType)
	}
	if recipientSum(op, 1) != 50 {
		t.Errorf("repayment recipients = %+v, want lender с суммой 50", op.Recipients)
	}
	// погашение сохранено как у бота develop: active + recipients_with_sum
	ops := roomOperations(roomRepo.rooms[room.ID.Hex()])
	saved := ops[len(ops)-1]
	if saved.Status != statusActive || !saved.IsDebtRepayment {
		t.Errorf("saved repayment = %+v, want active debt repayment", saved)
	}
	if len(saved.RecipientsWithSum) != 1 || saved.RecipientsWithSum[0].Sum != 50 {
		t.Errorf("saved recipientsWithSum = %+v, want [{lender, 50}]", saved.RecipientsWithSum)
	}
	// после полного погашения долгов нет
	rec = doRequest(t, s, http.MethodGet, fmt.Sprintf("/api/v1/rooms/%s/debts", room.ID.Hex()), token, "")
	var debts []debtDto
	if err := json.Unmarshal(rec.Body.Bytes(), &debts); err != nil || len(debts) != 0 {
		t.Errorf("debts after full repayment = %s, want []", rec.Body.String())
	}
}

// (г2) конкурентное погашение того же долга: компенсация удаляет переплату и отдаёт 409
func TestRepaymentConcurrentOverpayCompensated(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)

	// «конкурентный» запрос успевает вставить своё погашение 50 между нашей
	// вставкой и перепроверкой долга (mongo без транзакций)
	roomRepo.afterCreate = func(roomId string) {
		roomRepo.afterCreate = nil
		concurrent := api.Operation{
			ID:                primitive.NewObjectID(),
			Sum:               50,
			Donor:             &testUser2,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 50}},
			IsDebtRepayment:   true,
			Status:            statusActive,
			CreateAt:          time.Now(),
		}
		ops := append(roomOperations(roomRepo.rooms[roomId]), concurrent)
		roomRepo.rooms[roomId].Operations = &ops
	}

	url := fmt.Sprintf("/api/v1/rooms/%s/repayments", room.ID.Hex())
	rec := doRequest(t, s, http.MethodPost, url, mustToken(t, s, testUser2.ID), `{"debtorId": 2, "lenderId": 1, "sum": 50}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	// наша операция откатилась: осталось ровно одно погашение (конкурентное), долг не инвертирован
	var repayments int
	for _, o := range roomOperations(roomRepo.rooms[room.ID.Hex()]) {
		if o.IsDebtRepayment {
			repayments++
		}
	}
	if repayments != 1 {
		t.Fatalf("repayments in room = %d, want 1", repayments)
	}
}

// (д) happy-path создания операции: equally с каноническими долями
func TestCreateOperationHappyPath(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)

	body := `{"description": "Такси", "sum": 301, "donorId": 2, "recipientIds": [1, 2]}`
	rec := doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex()),
		mustToken(t, s, testUser2.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	op := parseOperation(t, rec)
	if op.ID == "" {
		t.Error("operation id is empty")
	}
	if op.Description != "Такси" || op.Sum != 301 || op.IsDebtRepayment {
		t.Errorf("unexpected operation: %+v", op)
	}
	if op.SplitType != "equally" {
		t.Errorf("splitType = %q, want equally", op.SplitType)
	}
	// каноническое деление: 301 на двоих → [151, 150], остаток первому получателю массива
	if op.Donor.ID != 2 || recipientSum(op, 1) != 151 || recipientSum(op, 2) != 150 {
		t.Errorf("unexpected donor/recipients: %+v", op)
	}
	if got := len(roomOperations(roomRepo.rooms[room.ID.Hex()])); got != 2 {
		t.Errorf("operations in room = %d, want 2", got)
	}
	// в базе — модель develop: active, equally, целочисленные float-доли
	saved := roomOperations(roomRepo.rooms[room.ID.Hex()])[1]
	if saved.Status != statusActive || saved.SplitType != splitEqually {
		t.Errorf("saved status/splitType = %q/%q, want active/equally", saved.Status, saved.SplitType)
	}
	if saved.Recipients != nil {
		t.Error("легаси-поле recipients не должно заполняться")
	}
	if len(saved.RecipientsWithSum) != 2 || saved.RecipientsWithSum[0].Sum != 151 || saved.RecipientsWithSum[1].Sum != 150 {
		t.Errorf("saved recipientsWithSum = %+v, want [151, 150]", saved.RecipientsWithSum)
	}

	// донор не из комнаты → 400
	rec = doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex()),
		mustToken(t, s, testUser1.ID), `{"description": "Такси", "sum": 300, "donorId": 99, "recipientIds": [1]}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// (д2) by_exact_amount: точные суммы сохраняются и отдаются как есть
func TestCreateOperationByExactAmount(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser2.ID)

	body := `{"description": "Отель", "sum": 100, "donorId": 2, "recipientSums": [{"userId": 1, "sum": 70}, {"userId": 2, "sum": 30}]}`
	rec := doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	op := parseOperation(t, rec)
	if op.SplitType != "by_exact_amount" {
		t.Errorf("splitType = %q, want by_exact_amount", op.SplitType)
	}
	if recipientSum(op, 1) != 70 || recipientSum(op, 2) != 30 {
		t.Errorf("recipients = %+v, want [70, 30]", op.Recipients)
	}
	saved := roomOperations(roomRepo.rooms[room.ID.Hex()])[1]
	if saved.SplitType != splitByExactAmount || saved.Status != statusActive {
		t.Errorf("saved status/splitType = %q/%q, want active/by_exact_amount", saved.Status, saved.SplitType)
	}

	// долги учитывают точные суммы: 1 должен 2 уже 70 − 50 = 20
	rec = doRequest(t, s, http.MethodGet, fmt.Sprintf("/api/v1/rooms/%s/debts", room.ID.Hex()), token, "")
	var debts []debtDto
	if err := json.Unmarshal(rec.Body.Bytes(), &debts); err != nil {
		t.Fatalf("cannot parse debts: %v", err)
	}
	if len(debts) != 1 || debts[0].Debtor.ID != 1 || debts[0].Lender.ID != 2 || debts[0].Sum != 20 {
		t.Errorf("debts = %+v, want [1 должен 2: 20]", debts)
	}
}

// (д3) валидация by_exact_amount и выбора режима деления
func TestCreateOperationSplitValidation(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser1.ID)

	for name, body := range map[string]string{
		"Σ recipientSums != sum": `{"description": "х", "sum": 100, "donorId": 1, "recipientSums": [{"userId": 1, "sum": 70}, {"userId": 2, "sum": 40}]}`,
		"дубль получателя":       `{"description": "х", "sum": 100, "donorId": 1, "recipientSums": [{"userId": 2, "sum": 50}, {"userId": 2, "sum": 50}]}`,
		"нулевая доля":           `{"description": "х", "sum": 100, "donorId": 1, "recipientSums": [{"userId": 1, "sum": 100}, {"userId": 2, "sum": 0}]}`,
		"не участник":            `{"description": "х", "sum": 100, "donorId": 1, "recipientSums": [{"userId": 99, "sum": 100}]}`,
		"оба режима сразу":       `{"description": "х", "sum": 100, "donorId": 1, "recipientIds": [1], "recipientSums": [{"userId": 2, "sum": 100}]}`,
		"ни одного режима":       `{"description": "х", "sum": 100, "donorId": 1}`,
	} {
		rec := doRequest(t, s, http.MethodPost, url, token, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400, body: %s", name, rec.Code, rec.Body.String())
		}
	}
}

// (е) идемпотентность создания: повтор POST с тем же clientOpId → 200 и та же
// операция (не дубль), другой ключ → новая операция, без ключа — как раньше
func TestCreateOperationIdempotent(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser1.ID)

	const key = "11111111-1111-1111-1111-111111111111"
	body := fmt.Sprintf(`{"description": "Такси", "sum": 300, "donorId": 1, "recipientIds": [1, 2], "clientOpId": %q}`, key)
	rec := doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	first := parseOperation(t, rec)
	if first.ClientOpId != key {
		t.Errorf("clientOpId = %q, want %q (клиент сопоставляет outbox по нему)", first.ClientOpId, key)
	}

	// повтор из outbox (тот же ключ) → 200 и та же операция, дубля нет
	rec = doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	second := parseOperation(t, rec)
	if second.ID != first.ID || second.ClientOpId != key {
		t.Errorf("retry вернул другую операцию %+v, want %+v", second, first)
	}
	if got := len(roomOperations(roomRepo.rooms[room.ID.Hex()])); got != 2 { // легаси + новая
		t.Errorf("operations in room = %d, want 2 (повтор не создаёт дубль)", got)
	}
	// хранимая операция несёт client_op_id
	saved := roomOperations(roomRepo.rooms[room.ID.Hex()])[1]
	if saved.ClientOpId != key {
		t.Errorf("saved client_op_id = %q, want %q", saved.ClientOpId, key)
	}

	// другой clientOpId → новая операция
	rec = doRequest(t, s, http.MethodPost, url, token, strings.Replace(body, "11111111", "22222222", 1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("other key status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	if parseOperation(t, rec).ID == first.ID {
		t.Error("операция с другим clientOpId должна быть новой")
	}
	if got := len(roomOperations(roomRepo.rooms[room.ID.Hex()])); got != 3 {
		t.Errorf("operations in room = %d, want 3", got)
	}

	// без clientOpId — как раньше: каждый POST создаёт операцию, ключа в ответе нет
	plain := `{"description": "Кофе", "sum": 100, "donorId": 1, "recipientIds": [1, 2]}`
	for i := 0; i < 2; i++ {
		rec = doRequest(t, s, http.MethodPost, url, token, plain)
		if rec.Code != http.StatusCreated {
			t.Fatalf("plain status = %d, want 201, body: %s", rec.Code, rec.Body.String())
		}
		if op := parseOperation(t, rec); op.ClientOpId != "" {
			t.Errorf("plain clientOpId = %q, want пусто", op.ClientOpId)
		}
	}
	if got := len(roomOperations(roomRepo.rooms[room.ID.Hex()])); got != 5 {
		t.Errorf("operations in room = %d, want 5 (без ключа дедупликации нет)", got)
	}
}

// (е2) невалидный clientOpId → 400 validation (и для операций, и для погашений)
func TestClientOpIdValidation(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser1.ID)

	for name, id := range map[string]string{
		"длиннее 64 символов":  strings.Repeat("a", 65),
		"недопустимые символы": "abc_123!",
		"кириллица":            "ключ-1234",
		"пробел":               "aaaa bbbb",
	} {
		body := fmt.Sprintf(`{"description": "х", "sum": 100, "donorId": 1, "recipientIds": [1], "clientOpId": %q}`, id)
		rec := doRequest(t, s, http.MethodPost, url, token, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400, body: %s", name, rec.Code, rec.Body.String())
		}
	}

	// погашение валидирует ключ так же
	rec := doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/repayments", room.ID.Hex()),
		mustToken(t, s, testUser2.ID), `{"debtorId": 2, "lenderId": 1, "sum": 50, "clientOpId": "bad key!"}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	// ровно 64 допустимых символа проходят
	body := fmt.Sprintf(`{"description": "х", "sum": 100, "donorId": 1, "recipientIds": [1], "clientOpId": %q}`,
		strings.Repeat("a", 64))
	rec = doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusCreated {
		t.Errorf("64-символьный ключ: status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
}

// (е3) конкурентная вставка того же clientOpId: атомарная проверка+вставка —
// в комнате одна операция, проигравший получает 200 с операцией победителя
func TestCreateOperationIdempotentConcurrent(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)

	const key = "33333333-3333-3333-3333-333333333333"
	var concurrentId primitive.ObjectID
	// «конкурентный» повтор успевает вставить операцию с тем же ключом между
	// ранней проверкой хендлера и атомарной вставкой (хук фейка)
	roomRepo.beforeCreateIfAbsent = func(roomId string) {
		concurrentId = primitive.NewObjectID()
		concurrent := api.Operation{
			ID:                concurrentId,
			Description:       "Такси",
			Sum:               300,
			Donor:             &testUser1,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 150}, {User: testUser2, Sum: 150}},
			SplitType:         splitEqually,
			Status:            statusActive,
			CreateAt:          time.Now(),
			ClientOpId:        key,
		}
		ops := append(roomOperations(roomRepo.rooms[roomId]), concurrent)
		roomRepo.rooms[roomId].Operations = &ops
	}

	body := fmt.Sprintf(`{"description": "Такси", "sum": 300, "donorId": 1, "recipientIds": [1, 2], "clientOpId": %q}`, key)
	rec := doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex()),
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (проигравший получает существующую), body: %s", rec.Code, rec.Body.String())
	}
	if op := parseOperation(t, rec); op.ID != concurrentId.Hex() {
		t.Errorf("вернулась операция %s, want конкурентная %s", op.ID, concurrentId.Hex())
	}
	var withKey int
	for _, o := range roomOperations(roomRepo.rooms[room.ID.Hex()]) {
		if o.ClientOpId == key {
			withKey++
		}
	}
	if withKey != 1 {
		t.Fatalf("операций с ключом = %d, want 1 (гонка не создаёт дубль)", withKey)
	}
}

// (е4) идемпотентное погашение: повтор с тем же clientOpId → 200 и то же
// погашение, а не 409 «долга нет» (долг уже погашен первой доставкой)
func TestRepaymentIdempotent(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)
	url := fmt.Sprintf("/api/v1/rooms/%s/repayments", room.ID.Hex())
	token := mustToken(t, s, testUser2.ID)

	body := `{"debtorId": 2, "lenderId": 1, "sum": 50, "clientOpId": "44444444-4444-4444-4444-444444444444"}`
	rec := doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	first := parseOperation(t, rec)

	// долг 2→1 полностью погашен; без идемпотентности повтор получил бы 409
	rec = doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	second := parseOperation(t, rec)
	if second.ID != first.ID || !second.IsDebtRepayment || second.Sum != 50 {
		t.Errorf("retry вернул %+v, want то же погашение %+v", second, first)
	}
	var repayments int
	for _, o := range roomOperations(roomRepo.rooms[room.ID.Hex()]) {
		if o.IsDebtRepayment {
			repayments++
		}
	}
	if repayments != 1 {
		t.Fatalf("repayments = %d, want 1 (без дубля)", repayments)
	}
}

// (д4) операции бота: float-доли equally отдаются канонически, драфты и архив скрыты
func TestBotOperationsNormalized(t *testing.T) {
	// бот develop: equally на троих с ДРОБНЫМИ float-долями 100/3 = 33.33…
	botOp := api.Operation{
		ID:          primitive.NewObjectID(),
		Description: "Из бота",
		Sum:         100,
		Donor:       &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: testUser1, Sum: 100.0 / 3},
			{User: testUser2, Sum: 100.0 / 3},
			{User: testUser3, Sum: 100.0 / 3},
		},
		Status:    statusActive,
		SplitType: splitEqually,
		CreateAt:  time.Now(),
	}
	draftOp := api.Operation{
		ID: primitive.NewObjectID(), Description: "Черновик", Sum: 500,
		Donor: &testUser1, Status: statusDraft, SplitType: splitByExactAmount, CreateAt: time.Now(),
	}
	archivedOp := api.Operation{
		ID: primitive.NewObjectID(), Description: "Старая версия", Sum: 900,
		Donor:             &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 900}},
		Status:            statusArchive, SplitType: splitEqually, CreateAt: time.Now(),
	}
	room := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Бот-комната",
		Members:    &[]api.User{testUser1, testUser2, testUser3},
		Operations: &[]api.Operation{botOp, draftOp, archivedOp},
		CreateAt:   time.Now(),
	}
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room detail: %v", err)
	}
	// драфт и архив скрыты
	if len(detail.Operations) != 1 {
		t.Fatalf("operations = %d, want 1 (draft и archive скрыты), body: %s", len(detail.Operations), rec.Body.String())
	}
	if detail.TotalSpent != 100 {
		t.Errorf("totalSpent = %d, want 100 (без драфтов и архива)", detail.TotalSpent)
	}
	// канонические целые доли вместо хранимых 33.33…: [34, 33, 33]
	op := detail.Operations[0]
	if recipientSum(op, 1) != 34 || recipientSum(op, 2) != 33 || recipientSum(op, 3) != 33 {
		t.Errorf("recipients = %+v, want канонические [34, 33, 33]", op.Recipients)
	}
	// долги по float-долям бота, округление вместо усечения: 33.33… → 33
	if got := findDebtDto(detail.Debts, 2, 1); got != 33 {
		t.Errorf("долг 2→1 = %d, want 33", got)
	}
	if got := findDebtDto(detail.Debts, 3, 1); got != 33 {
		t.Errorf("долг 3→1 = %d, want 33", got)
	}
	// mySpent донора — его каноническая доля с остатком
	if detail.MySpent != 34 {
		t.Errorf("mySpent = %d, want 34", detail.MySpent)
	}
	// драфт нельзя ни отредактировать, ни удалить через REST
	draftUrl := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), draftOp.ID.Hex())
	rec = doRequest(t, s, http.MethodPut, draftUrl, mustToken(t, s, testUser1.ID),
		`{"description": "х", "sum": 1, "donorId": 1, "recipientIds": [1]}`)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
	rec = doRequest(t, s, http.MethodDelete, draftUrl, mustToken(t, s, testUser1.ID), "")
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

func findDebtDto(debts []debtDto, debtorId, lenderId int) int {
	for _, d := range debts {
		if d.Debtor.ID == debtorId && d.Lender.ID == lenderId {
			return d.Sum
		}
	}
	return -1
}

// (е) пагинация активности: огромный limit не переполняет int и не паникует
func TestActivityPaginationOverflow(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	token := mustToken(t, s, testUser1.ID)

	parseItems := func(rec *httptest.ResponseRecorder) []activityItemDto {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}
		var items []activityItemDto
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("cannot parse activity: %v", err)
		}
		return items
	}

	// offset+limit раньше переполнялись в отрицательный end → panic slice bounds
	rec := doRequest(t, s, http.MethodGet, "/api/v1/activity?offset=1&limit=9223372036854775807", token, "")
	if items := parseItems(rec); len(items) != 0 {
		t.Errorf("items = %d, want 0 (offset за концом списка)", len(items))
	}

	// offset за концом списка → пустой ответ
	rec = doRequest(t, s, http.MethodGet, "/api/v1/activity?offset=100&limit=10", token, "")
	if items := parseItems(rec); len(items) != 0 {
		t.Errorf("items = %d, want 0", len(items))
	}

	// в пределах списка отдаётся как раньше
	rec = doRequest(t, s, http.MethodGet, "/api/v1/activity", token, "")
	if items := parseItems(rec); len(items) != 1 {
		t.Errorf("items = %d, want 1", len(items))
	}

	// отрицательные значения по-прежнему 400
	rec = doRequest(t, s, http.MethodGet, "/api/v1/activity?offset=-1", token, "")
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// (ж) расширенная статистика дашборда: byDay/paidBy/shareBy/top/monthSpent
// на фикстуре с легаси-операцией, погашением и черновиком
func TestStatisticsDashboard(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	day := func(d int, hour int) time.Time { return time.Date(2026, 7, d, hour, 0, 0, 0, time.UTC) }

	opHotel := api.Operation{ // вне месяца и вне окна 30 дней: только totalSpent/paidBy/shareBy/top
		ID: primitive.NewObjectID(), Description: "Отель", Sum: 4000, Donor: &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 4000}},
		Status:            statusActive, SplitType: splitByExactAmount,
		CreateAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	opDinner := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 3600, Donor: &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 1800}, {User: testUser2, Sum: 1800}},
		Status:            statusActive, SplitType: splitEqually, CreateAt: day(15, 10),
	}
	opTaxi := api.Operation{
		ID: primitive.NewObjectID(), Description: "Такси", Sum: 800, Donor: &testUser2,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 500}, {User: testUser2, Sum: 300}},
		Status:            statusActive, SplitType: splitByExactAmount, CreateAt: day(14, 12),
	}
	opLegacy := api.Operation{ // легаси master-2021: без status и recipients_with_sum → доли [500, 500]
		ID: primitive.NewObjectID(), Description: "Легаси", Sum: 1000, Donor: &testUser1,
		Recipients: &[]api.User{testUser1, testUser2}, CreateAt: day(13, 12),
	}
	opCoffee := api.Operation{
		ID: primitive.NewObjectID(), Description: "Кофе", Sum: 100, Donor: &testUser2,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
		Status:            statusActive, SplitType: splitEqually, CreateAt: day(10, 12),
	}
	opGum := api.Operation{ // шестой по сумме расход — в топ-5 не входит
		ID: primitive.NewObjectID(), Description: "Жвачка", Sum: 50, Donor: &testUser2,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 50}},
		Status:            statusActive, SplitType: splitEqually, CreateAt: day(9, 12),
	}
	repayment := api.Operation{ // погашение — не трата, в статистику не входит
		ID: primitive.NewObjectID(), Sum: 500, Donor: &testUser2,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 500}},
		IsDebtRepayment:   true, Status: statusActive, CreateAt: day(15, 11),
	}
	draft := api.Operation{ // черновик бота — невидим
		ID: primitive.NewObjectID(), Description: "Черновик", Sum: 999, Donor: &testUser1,
		Status: statusDraft, CreateAt: day(15, 11),
	}
	room := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Дашборд",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{opHotel, opDinner, opTaxi, opLegacy, opCoffee, opGum, repayment, draft},
		CreateAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	s.now = func() time.Time { return now }

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex()+"/statistics", mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var stats statisticsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("cannot parse statistics %q: %v", rec.Body.String(), err)
	}

	if stats.Currency != "RUB" {
		t.Errorf("currency = %q, want RUB (валюта не выбрана)", stats.Currency)
	}
	// все расходы без погашения и черновика: 4000+3600+800+1000+100+50
	if stats.TotalSpent != 9550 {
		t.Errorf("totalSpent = %d, want 9550", stats.TotalSpent)
	}
	// только июльские расходы: без «Отеля» (май)
	if stats.MonthSpent != 5550 {
		t.Errorf("monthSpent = %d, want 5550", stats.MonthSpent)
	}
	// все расходы за всё время: погашение и черновик не считаются
	if stats.OperationCount != 6 {
		t.Errorf("operationCount = %d, want 6", stats.OperationCount)
	}

	// byMonth: ровно 6 месяцев включая текущий, нулевые присутствуют, по возрастанию
	wantByMonth := []monthlySumDto{
		{Month: "2026-02", Sum: 0},
		{Month: "2026-03", Sum: 0},
		{Month: "2026-04", Sum: 0},
		{Month: "2026-05", Sum: 4000},
		{Month: "2026-06", Sum: 0},
		{Month: "2026-07", Sum: 5550},
	}
	if len(stats.ByMonth) != len(wantByMonth) {
		t.Fatalf("byMonth = %+v, want %+v", stats.ByMonth, wantByMonth)
	}
	for i, want := range wantByMonth {
		if stats.ByMonth[i] != want {
			t.Errorf("byMonth[%d] = %+v, want %+v", i, stats.ByMonth[i], want)
		}
	}

	// byDay: только последние 30 дней, только дни с тратами, ISO-даты, по возрастанию
	wantByDay := []dailySumDto{
		{Date: "2026-07-09", Sum: 50},
		{Date: "2026-07-10", Sum: 100},
		{Date: "2026-07-13", Sum: 1000},
		{Date: "2026-07-14", Sum: 800},
		{Date: "2026-07-15", Sum: 3600},
	}
	if len(stats.ByDay) != len(wantByDay) {
		t.Fatalf("byDay = %+v, want %+v", stats.ByDay, wantByDay)
	}
	for i, want := range wantByDay {
		if stats.ByDay[i] != want {
			t.Errorf("byDay[%d] = %+v, want %+v", i, stats.ByDay[i], want)
		}
	}

	// paidByMember: u1 = 4000+3600+1000 = 8600, u2 = 800+100+50 = 950 (погашение не платёж)
	if len(stats.PaidByMember) != 2 ||
		stats.PaidByMember[0].User.ID != 1 || stats.PaidByMember[0].Sum != 8600 ||
		stats.PaidByMember[1].User.ID != 2 || stats.PaidByMember[1].Sum != 950 {
		t.Errorf("paidByMember = %+v, want [{u1 8600} {u2 950}]", stats.PaidByMember)
	}

	// shareByMember: u2 = 4000+1800+300+500+100+50 = 6750, u1 = 1800+500+500 = 2800
	// (доля легаси-операции синтезирована канонически)
	if len(stats.ShareByMember) != 2 ||
		stats.ShareByMember[0].User.ID != 2 || stats.ShareByMember[0].Sum != 6750 ||
		stats.ShareByMember[1].User.ID != 1 || stats.ShareByMember[1].Sum != 2800 {
		t.Errorf("shareByMember = %+v, want [{u2 6750} {u1 2800}]", stats.ShareByMember)
	}

	// топ-5 по сумме: Жвачка (50) не входит, погашение (500) — тоже
	wantTop := []struct {
		id   string
		sum  int
		desc string
	}{
		{opHotel.ID.Hex(), 4000, "Отель"},
		{opDinner.ID.Hex(), 3600, "Ужин"},
		{opLegacy.ID.Hex(), 1000, "Легаси"},
		{opTaxi.ID.Hex(), 800, "Такси"},
		{opCoffee.ID.Hex(), 100, "Кофе"},
	}
	if len(stats.TopOperations) != len(wantTop) {
		t.Fatalf("topOperations = %+v, want 5 операций", stats.TopOperations)
	}
	for i, want := range wantTop {
		got := stats.TopOperations[i]
		if got.ID != want.id || got.Sum != want.sum || got.Description != want.desc {
			t.Errorf("topOperations[%d] = %+v, want {%s %d %s}", i, got, want.id, want.sum, want.desc)
		}
	}
	if stats.TopOperations[0].Donor.ID != 1 || stats.TopOperations[0].CreatedAt.IsZero() {
		t.Errorf("topOperations[0] donor/createdAt = %+v, want donor u1 и непустая дата", stats.TopOperations[0])
	}

	// не участник комнаты → 403
	s2 := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), newFakeRoomRepo(room))
	rec = doRequest(t, s2, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex()+"/statistics", mustToken(t, s2, testUser3.ID), "")
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")
}

// byMonth и operationCount: окно ровно 6 календарных месяцев включая текущий,
// операции старше окна остаются в totalSpent/operationCount, нулевые месяцы
// присутствуют, порядок по возрастанию, погашения и черновики не считаются
func TestStatisticsByMonth(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) // окно: 2026-02 .. 2026-07
	spend := func(desc string, sum int, at time.Time) api.Operation {
		return api.Operation{
			ID: primitive.NewObjectID(), Description: desc, Sum: sum, Donor: &testUser1,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: float64(sum)}},
			Status:            statusActive, SplitType: splitByExactAmount, CreateAt: at,
		}
	}
	opNov := spend("Ноябрь", 700, time.Date(2025, 11, 20, 12, 0, 0, 0, time.UTC)) // старше окна
	opJan := spend("Январь", 999, time.Date(2026, 1, 31, 23, 0, 0, 0, time.UTC))  // последний день перед окном
	opFeb := spend("Февраль", 200, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))   // первый день окна
	opApr := spend("Апрель", 300, time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC))
	opLegacy := api.Operation{ // легаси master-2021 внутри окна — нормализуется как active-расход
		ID: primitive.NewObjectID(), Description: "Легаси", Sum: 400, Donor: &testUser1,
		Recipients: &[]api.User{testUser1, testUser2},
		CreateAt:   time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
	}
	opJul := spend("Июль", 100, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	repayment := api.Operation{ // погашение в марте — не трата: март остаётся нулевым
		ID: primitive.NewObjectID(), Sum: 500, Donor: &testUser2,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 500}},
		IsDebtRepayment:   true, Status: statusActive,
		CreateAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
	}
	draft := api.Operation{ // черновик в июне — невидим: июнь остаётся нулевым
		ID: primitive.NewObjectID(), Description: "Черновик", Sum: 999, Donor: &testUser1,
		Status: statusDraft, CreateAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	room := &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Помесячная",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{opNov, opJan, opFeb, opApr, opLegacy, opJul, repayment, draft},
		CreateAt:   time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC),
	}
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	s.now = func() time.Time { return now }

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex()+"/statistics", mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var stats statisticsDto
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("cannot parse statistics %q: %v", rec.Body.String(), err)
	}

	// операции вне окна входят в общие агрегаты: 700+999+200+300+400+100
	if stats.TotalSpent != 2699 {
		t.Errorf("totalSpent = %d, want 2699", stats.TotalSpent)
	}
	if stats.OperationCount != 6 {
		t.Errorf("operationCount = %d, want 6", stats.OperationCount)
	}

	// ноябрь и январь вне окна; март/июнь нулевые, но присутствуют; ascending
	wantByMonth := []monthlySumDto{
		{Month: "2026-02", Sum: 200},
		{Month: "2026-03", Sum: 0},
		{Month: "2026-04", Sum: 300},
		{Month: "2026-05", Sum: 400},
		{Month: "2026-06", Sum: 0},
		{Month: "2026-07", Sum: 100},
	}
	if len(stats.ByMonth) != len(wantByMonth) {
		t.Fatalf("byMonth = %+v, want %+v", stats.ByMonth, wantByMonth)
	}
	for i, want := range wantByMonth {
		if stats.ByMonth[i] != want {
			t.Errorf("byMonth[%d] = %+v, want %+v", i, stats.ByMonth[i], want)
		}
	}
}

// (з) валюта комнаты в DTO: summary/detail (пусто → RUB), activity.roomCurrency,
// friends: currency у разбивки по комнатам и totalsByCurrency без сложения валют
func TestCurrencyInDtos(t *testing.T) {
	roomRub := newTestRoom() // currency в базе пустая, долг u2 → u1 = 50
	roomUsd := &api.Room{    // долг u1 → u2 = 100
		ID:       primitive.NewObjectID(),
		Name:     "Балийская",
		Currency: "USD",
		Members:  &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{{
			ID: primitive.NewObjectID(), Description: "Серф", Sum: 200, Donor: &testUser2,
			RecipientsWithSum: []api.RecipientWithSum{{User: testUser1, Sum: 100}, {User: testUser2, Sum: 100}},
			Status:            statusActive, SplitType: splitEqually, CreateAt: time.Now(),
		}},
		CreateAt: time.Now(),
	}
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(roomRub, roomUsd))
	token := mustToken(t, s, testUser1.ID)

	// список комнат: пустая валюта отдаётся как RUB
	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms", token, "")
	var summaries []roomSummaryDto
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("cannot parse rooms: %v", err)
	}
	byId := map[string]string{}
	for _, r := range summaries {
		byId[r.ID] = r.Currency
	}
	if byId[roomRub.ID.Hex()] != "RUB" || byId[roomUsd.ID.Hex()] != "USD" {
		t.Errorf("summary currencies = %v, want RUB и USD", byId)
	}

	// деталь комнаты
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+roomUsd.ID.Hex(), token, "")
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room detail: %v", err)
	}
	if detail.Currency != "USD" {
		t.Errorf("detail currency = %q, want USD", detail.Currency)
	}

	// активность: roomCurrency у каждого элемента
	rec = doRequest(t, s, http.MethodGet, "/api/v1/activity", token, "")
	var items []activityItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("cannot parse activity: %v", err)
	}
	for _, item := range items {
		want := "RUB"
		if item.RoomId == roomUsd.ID.Hex() {
			want = "USD"
		}
		if item.RoomCurrency != want {
			t.Errorf("activity roomCurrency = %q для %s, want %q", item.RoomCurrency, item.RoomName, want)
		}
	}

	// друзья: у разбивки по комнатам валюта комнаты, итог — по каждой валюте отдельно
	rec = doRequest(t, s, http.MethodGet, "/api/v1/friends", token, "")
	var friends []friendBalanceDto
	if err := json.Unmarshal(rec.Body.Bytes(), &friends); err != nil {
		t.Fatalf("cannot parse friends: %v", err)
	}
	if len(friends) != 1 || friends[0].User.ID != 2 {
		t.Fatalf("friends = %+v, want только u2", friends)
	}
	friend := friends[0]
	roomCur := map[string]struct {
		currency string
		balance  int
	}{}
	for _, r := range friend.Rooms {
		roomCur[r.RoomId] = struct {
			currency string
			balance  int
		}{r.Currency, r.Balance}
	}
	if got := roomCur[roomRub.ID.Hex()]; got.currency != "RUB" || got.balance != 50 {
		t.Errorf("rub room = %+v, want {RUB 50}", got)
	}
	if got := roomCur[roomUsd.ID.Hex()]; got.currency != "USD" || got.balance != -100 {
		t.Errorf("usd room = %+v, want {USD -100}", got)
	}
	// суммы в разных валютах не складываются: два независимых итога
	wantTotals := []currencySumDto{{Currency: "RUB", Sum: 50}, {Currency: "USD", Sum: -100}}
	if len(friend.TotalsByCurrency) != 2 ||
		friend.TotalsByCurrency[0] != wantTotals[0] || friend.TotalsByCurrency[1] != wantTotals[1] {
		t.Errorf("totalsByCurrency = %+v, want %+v", friend.TotalsByCurrency, wantTotals)
	}
}

// (и) PUT /rooms/{roomId}/currency: успех, невалидный код, не участник
func TestUpdateCurrency(t *testing.T) {
	room := newTestRoom()
	roomRepo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2, testUser3), roomRepo)
	url := "/api/v1/rooms/" + room.ID.Hex() + "/currency"

	// участник меняет валюту → 204, значение сохранено и отдаётся в DTO
	rec := doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), `{"currency": "USD"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	if got := roomRepo.rooms[room.ID.Hex()].Currency; got != "USD" {
		t.Errorf("saved currency = %q, want USD", got)
	}
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), mustToken(t, s, testUser1.ID), "")
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room detail: %v", err)
	}
	if detail.Currency != "USD" {
		t.Errorf("detail currency = %q, want USD", detail.Currency)
	}

	// код не из справочника → 400 validation, валюта не изменилась
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), `{"currency": "BTC"}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID), `{"currency": ""}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	if got := roomRepo.rooms[room.ID.Hex()].Currency; got != "USD" {
		t.Errorf("currency after invalid codes = %q, want USD", got)
	}

	// не участник → 403 (даже с валидным кодом)
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser3.ID), `{"currency": "EUR"}`)
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	// несуществующая комната → 404
	rec = doRequest(t, s, http.MethodPut, "/api/v1/rooms/"+primitive.NewObjectID().Hex()+"/currency",
		mustToken(t, s, testUser1.ID), `{"currency": "EUR"}`)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

// (к) GET /currencies — справочник валют для пикера в стабильном порядке
func TestCurrenciesDictionary(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo())

	// как и весь API — только с токеном
	rec := doRequest(t, s, http.MethodGet, "/api/v1/currencies", "", "")
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")

	rec = doRequest(t, s, http.MethodGet, "/api/v1/currencies", mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var currencies []currencyInfoDto
	if err := json.Unmarshal(rec.Body.Bytes(), &currencies); err != nil {
		t.Fatalf("cannot parse currencies %q: %v", rec.Body.String(), err)
	}
	want := []currencyInfoDto{
		{Code: "RUB", Symbol: "₽", Flag: "🇷🇺"},
		{Code: "USD", Symbol: "$", Flag: "🇺🇸"},
		{Code: "EUR", Symbol: "€", Flag: "🇪🇺"},
		{Code: "IDR", Symbol: "Rp", Flag: "🇮🇩"},
		{Code: "KZT", Symbol: "₸", Flag: "🇰🇿"},
		{Code: "UZS", Symbol: "сум", Flag: "🇺🇿"},
	}
	if len(currencies) != len(want) {
		t.Fatalf("currencies = %+v, want %+v", currencies, want)
	}
	for i := range want {
		if currencies[i] != want[i] {
			t.Errorf("currencies[%d] = %+v, want %+v", i, currencies[i], want[i])
		}
	}
}

// Уведомления (Notifier)

// waitNotifierCall ждёт очередное уведомление — сервер шлёт их из фоновой горутины
func waitNotifierCall(t *testing.T, n *fakeNotifier) notifierCall {
	t.Helper()
	select {
	case c := <-n.calls:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("notifier was not called")
		return notifierCall{}
	}
}

// assertNoNotifierCall проверяет, что уведомлений не было: на ошибочных путях
// горутина уведомления не запускается вовсе, поэтому проверка без ожидания
func assertNoNotifierCall(t *testing.T, n *fakeNotifier) {
	t.Helper()
	select {
	case c := <-n.calls:
		t.Fatalf("unexpected notifier call: %+v", c)
	default:
	}
}

// (у1) создание операции уведомляет: комната, операция и автор — как в запросе;
// ошибка валидации уведомления не шлёт
func TestCreateOperationNotifies(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	notifier := newFakeNotifier()
	s.SetNotifier(notifier)
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser1.ID)

	rec := doRequest(t, s, http.MethodPost, url, token,
		`{"description": "Обед", "sum": 100, "donorId": 2, "recipientIds": [1, 2]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	call := waitNotifierCall(t, notifier)
	if call.event != "created" {
		t.Fatalf("event = %q, want created", call.event)
	}
	if call.roomId != room.ID.Hex() {
		t.Errorf("roomId = %q, want %q", call.roomId, room.ID.Hex())
	}
	if call.author.ID != testUser1.ID {
		t.Errorf("author.ID = %d, want %d", call.author.ID, testUser1.ID)
	}
	if call.op.Description != "Обед" || call.op.Sum != 100 || call.op.Donor == nil || call.op.Donor.ID != 2 {
		t.Errorf("unexpected operation in notification: %+v", call.op)
	}
	if len(call.op.RecipientsWithSum) != 2 {
		t.Errorf("recipients in notification = %d, want 2", len(call.op.RecipientsWithSum))
	}

	// невалидное тело (нет режима деления) → 400 и никаких уведомлений
	rec = doRequest(t, s, http.MethodPost, url, token, `{"description": "х", "sum": 100, "donorId": 1}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	assertNoNotifierCall(t, notifier)
}

// (у2) обновление операции уведомляет с old/new снапшотами; ошибка валидации
// и чужая операция уведомлений не шлют
func TestUpdateOperationNotifies(t *testing.T) {
	room := newTestRoom()
	operationId := (*room.Operations)[0].ID.Hex()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	notifier := newFakeNotifier()
	s.SetNotifier(notifier)
	url := fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId)

	rec := doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID),
		`{"description": "Ужин в баре", "sum": 200, "donorId": 1, "recipientIds": [1, 2]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	call := waitNotifierCall(t, notifier)
	if call.event != "updated" {
		t.Fatalf("event = %q, want updated", call.event)
	}
	if call.roomId != room.ID.Hex() || call.author.ID != testUser1.ID {
		t.Errorf("roomId/author = %q/%d, want %q/%d", call.roomId, call.author.ID, room.ID.Hex(), testUser1.ID)
	}
	if call.oldOp.Description != "Ужин" || call.oldOp.Sum != 100 {
		t.Errorf("oldOp = %+v, want описание 'Ужин' и сумму 100", call.oldOp)
	}
	if call.op.Description != "Ужин в баре" || call.op.Sum != 200 {
		t.Errorf("newOp = %+v, want описание 'Ужин в баре' и сумму 200", call.op)
	}

	// невалидное тело → 400 без уведомления
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser1.ID),
		`{"description": "", "sum": 200, "donorId": 1, "recipientIds": [1, 2]}`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
	assertNoNotifierCall(t, notifier)

	// не автор, но участник комнаты → 200 с уведомлением (Splitwise-семантика)
	rec = doRequest(t, s, http.MethodPut, url, mustToken(t, s, testUser2.ID),
		`{"description": "Ужин", "sum": 100, "donorId": 2, "recipientIds": [1, 2]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("member edit status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	memberCall := waitNotifierCall(t, notifier)
	if memberCall.event != "updated" || memberCall.author.ID != testUser2.ID {
		t.Errorf("event/author = %q/%d, want updated/%d", memberCall.event, memberCall.author.ID, testUser2.ID)
	}
}

// (у3) удаление операции уведомляет удалённых получателей
func TestDeleteOperationNotifies(t *testing.T) {
	room := newTestRoom()
	operationId := (*room.Operations)[0].ID
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	notifier := newFakeNotifier()
	s.SetNotifier(notifier)

	rec := doRequest(t, s, http.MethodDelete,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), operationId.Hex()),
		mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	call := waitNotifierCall(t, notifier)
	if call.event != "deleted" {
		t.Fatalf("event = %q, want deleted", call.event)
	}
	if call.op.ID != operationId || call.author.ID != testUser1.ID {
		t.Errorf("op.ID/author = %s/%d, want %s/%d", call.op.ID.Hex(), call.author.ID, operationId.Hex(), testUser1.ID)
	}
}

// (у4) погашение долга уведомляет кредитора; удаление погашения — нет
// (у бота нет такого сценария)
func TestRepaymentNotifies(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	notifier := newFakeNotifier()
	s.SetNotifier(notifier)

	// долг 2 → 1 равен 50 (см. newTestRoom)
	rec := doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/repayments", room.ID.Hex()),
		mustToken(t, s, testUser2.ID), `{"debtorId": 2, "lenderId": 1, "sum": 50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	call := waitNotifierCall(t, notifier)
	if call.event != "repayment" {
		t.Fatalf("event = %q, want repayment", call.event)
	}
	if call.author.ID != testUser2.ID || !call.op.IsDebtRepayment || call.op.Sum != 50 {
		t.Errorf("unexpected repayment notification: author %d, op %+v", call.author.ID, call.op)
	}
	if len(call.op.RecipientsWithSum) != 1 || call.op.RecipientsWithSum[0].User.ID != testUser1.ID {
		t.Errorf("lender in notification = %+v, want user 1", call.op.RecipientsWithSum)
	}

	// удаление погашения не уведомляет
	op := parseOperation(t, rec)
	rec = doRequest(t, s, http.MethodDelete,
		fmt.Sprintf("/api/v1/rooms/%s/operations/%s", room.ID.Hex(), op.ID),
		mustToken(t, s, testUser2.ID), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}
	assertNoNotifierCall(t, notifier)
}

// «Неисчислимые» комнаты (легаси-данные, на которых балансы не сходятся)

// newBrokenLegacyRoom комната с легаси-данными старых версий бота, на которых
// service.GetRoomDebts возвращает «cannot calculate debts»: донор заплатил 227,
// а доли получателей — только 100, баланс комнаты не сходится на 127
// (реальный прод-кейс, ср. комнату 677e48c1c1751966168d1364)
func newBrokenLegacyRoom() *api.Room {
	operation := api.Operation{
		ID:                primitive.NewObjectID(),
		Description:       "Легаси-расход",
		Sum:               227,
		Donor:             &testUser1,
		RecipientsWithSum: []api.RecipientWithSum{{User: testUser2, Sum: 100}},
		Status:            statusActive,
		CreateAt:          time.Now(),
	}
	return &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "Битая комната",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{operation},
		CreateAt:   time.Now(),
	}
}

// (н) неисчислимая комната не 500-ит REST: эндпоинты деградируют —
// комната видна, долги пусты, балансы нулевые, debtsUnavailable=true
func TestBrokenRoomDegradesGracefully(t *testing.T) {
	broken := newBrokenLegacyRoom()
	healthy := newTestRoom() // долг 2 → 1 равен 50
	roomRepo := newFakeRoomRepo(broken, healthy)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), roomRepo)
	token := mustToken(t, s, testUser1.ID)

	// GET /rooms → 200: битая комната остаётся в списке с myBalance=0
	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rooms status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var summaries []roomSummaryDto
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("cannot parse rooms: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("rooms = %d, want 2 (битая комната не выпадает из списка)", len(summaries))
	}
	byId := map[string]roomSummaryDto{}
	for _, r := range summaries {
		byId[r.ID] = r
	}
	brokenSummary := byId[broken.ID.Hex()]
	if brokenSummary.MyBalance != 0 || !brokenSummary.DebtsUnavailable {
		t.Errorf("broken summary = %+v, want myBalance=0 и debtsUnavailable=true", brokenSummary)
	}
	if brokenSummary.TotalSpent != 227 || brokenSummary.MemberCount != 2 {
		t.Errorf("broken summary = %+v, want totalSpent=227 и memberCount=2 (комната видна целиком)", brokenSummary)
	}
	healthySummary := byId[healthy.ID.Hex()]
	if healthySummary.MyBalance != 50 || healthySummary.DebtsUnavailable {
		t.Errorf("healthy summary = %+v, want myBalance=50 без debtsUnavailable", healthySummary)
	}

	// GET /rooms/{id} битой → 200: операции/участники/валюта видны, debts=[]
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+broken.ID.Hex(), token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("broken detail status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse broken detail: %v", err)
	}
	if len(detail.Debts) != 0 || detail.MyBalance != 0 || !detail.DebtsUnavailable {
		t.Errorf("broken detail = %+v, want debts=[], myBalance=0, debtsUnavailable=true", detail)
	}
	if len(detail.Operations) != 1 || len(detail.Members) != 2 || detail.TotalSpent != 227 || detail.Currency != "RUB" {
		t.Errorf("broken detail = %+v, want полные операции/участников/валюту", detail)
	}

	// GET /rooms/{id}/debts битой → 200 []
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+broken.ID.Hex()+"/debts", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("broken debts status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var debts []debtDto
	if err := json.Unmarshal(rec.Body.Bytes(), &debts); err != nil || len(debts) != 0 {
		t.Errorf("broken debts = %s, want []", rec.Body.String())
	}

	// omitempty: у здоровой комнаты поля debtsUnavailable в json нет
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+healthy.ID.Hex(), token, "")
	if strings.Contains(rec.Body.String(), "debtsUnavailable") {
		t.Error("healthy detail не должна содержать debtsUnavailable (omitempty)")
	}

	// GET /friends → 200: вклад битой комнаты пропущен, здоровая учтена
	rec = doRequest(t, s, http.MethodGet, "/api/v1/friends", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("friends status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var friends []friendBalanceDto
	if err := json.Unmarshal(rec.Body.Bytes(), &friends); err != nil {
		t.Fatalf("cannot parse friends: %v", err)
	}
	if len(friends) != 1 || friends[0].User.ID != 2 {
		t.Fatalf("friends = %+v, want только u2 (друг из битой комнаты не пропадает)", friends)
	}
	friend := friends[0]
	if len(friend.Rooms) != 1 || friend.Rooms[0].RoomId != healthy.ID.Hex() || friend.Rooms[0].Balance != 50 {
		t.Errorf("friend rooms = %+v, want только здоровая комната с балансом 50", friend.Rooms)
	}
	if len(friend.TotalsByCurrency) != 1 || friend.TotalsByCurrency[0] != (currencySumDto{Currency: "RUB", Sum: 50}) {
		t.Errorf("totalsByCurrency = %+v, want [{RUB 50}] (без вклада битой)", friend.TotalsByCurrency)
	}

	// GET /rooms/{id}/statistics битой → 200 (статистика от долгов не зависит)
	rec = doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+broken.ID.Hex()+"/statistics", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("broken statistics status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// POST /repayments в битой → 409 с внятным сообщением, не 500; ничего не вставлено
	rec = doRequest(t, s, http.MethodPost, fmt.Sprintf("/api/v1/rooms/%s/repayments", broken.ID.Hex()),
		mustToken(t, s, testUser2.ID), `{"debtorId": 2, "lenderId": 1, "sum": 10}`)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")
	if got := len(roomOperations(roomRepo.rooms[broken.ID.Hex()])); got != 1 {
		t.Errorf("operations in broken room = %d, want 1 (погашение не вставлено)", got)
	}
}

// (у5) идемпотентный повтор создания с тем же clientOpId уведомляет только один раз
func TestIdempotentReplayNotifiesOnce(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	notifier := newFakeNotifier()
	s.SetNotifier(notifier)
	url := fmt.Sprintf("/api/v1/rooms/%s/operations", room.ID.Hex())
	token := mustToken(t, s, testUser1.ID)
	body := `{"description": "Кофе", "sum": 100, "donorId": 1, "recipientIds": [1, 2], "clientOpId": "abc-123"}`

	rec := doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	call := waitNotifierCall(t, notifier)
	if call.event != "created" || call.op.ClientOpId != "abc-123" {
		t.Fatalf("unexpected first call: %+v", call)
	}

	// повтор из outbox → 200 и без второго уведомления
	rec = doRequest(t, s, http.MethodPost, url, token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	assertNoNotifierCall(t, notifier)
}
