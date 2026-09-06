package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

func TestResolveAmount(t *testing.T) {
	minor := func(v int64) *int64 { return &v }

	legacyPtr := func(v int) *int { return &v }

	for _, tc := range []struct {
		name          string
		legacy        *int
		minor         *int64
		exp           int
		allowFraction bool
		want          int64
		wantErr       bool
	}{
		{name: "только старое поле, шкала 0", legacy: legacyPtr(20), exp: 0, want: 20},
		{name: "только старое поле, шкала 2", legacy: legacyPtr(20), exp: 2, want: 2000},
		{name: "только минорное", minor: minor(2080), exp: 2, allowFraction: true, want: 2080},
		{name: "оба поля сходятся", legacy: legacyPtr(20), minor: minor(2000), exp: 2, want: 2000},
		{
			name:   "оба поля сходятся у дробной суммы: 21 — проекция 20.80",
			legacy: legacyPtr(21), minor: minor(2080), exp: 2, allowFraction: true, want: 2080,
		},
		{
			name:   "старое поле не сходится с проекцией минорного",
			legacy: legacyPtr(20), minor: minor(2080), exp: 2, allowFraction: true, wantErr: true,
		},
		{
			name:  "дробь при выключенном признаке отвергается",
			minor: minor(2080), exp: 2, allowFraction: false, wantErr: true,
		},
		{
			name:  "целое минорное при выключенном признаке проходит",
			minor: minor(2000), exp: 2, allowFraction: false, want: 2000,
		},
		{name: "нет ни одного поля", exp: 2, wantErr: true},
		{
			name:   "ноль в старом поле — это ПРИСЛАННЫЙ ноль, а не отсутствие",
			legacy: legacyPtr(0), minor: minor(100), exp: 2, allowFraction: true, wantErr: true,
		},
		{
			name:   "огромное старое поле не сворачивается в маленькое дробное",
			legacy: legacyPtr(184467440737095517), exp: 2, wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, hErr := resolveAmount("sum", tc.legacy, tc.minor, tc.exp, tc.allowFraction)
			if tc.wantErr {
				if hErr == nil {
					t.Fatalf("ожидался отказ, получено %d", got)
				}
				return
			}
			if hErr != nil {
				t.Fatalf("неожиданный отказ: %s", hErr.message)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMinAmountMinor(t *testing.T) {
	// Меньше половины единицы — и старое поле, которое читают прежние сборки,
	// оказывается нулём: расход без суммы.
	if got := minAmountMinor(0); got != 1 {
		t.Errorf("шкала 0: got %d, want 1", got)
	}
	if got := minAmountMinor(2); got != 50 {
		t.Errorf("шкала 2: got %d, want 50", got)
	}
}

// Целый расход, присланный обоими полями, — основной путь прежних и новых
// сборок.
func TestCreateOperationAcceptsBothFields(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)
	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	body := `{"description":"Ужин","sum":20,"sumMinor":2000,"donorId":1,"recipientIds":[1,2]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}
	if op.SumMinor == nil || *op.SumMinor != 2000 {
		t.Fatalf("sumMinor = %v, want 2000", op.SumMinor)
	}
	if op.Sum != 20 {
		t.Errorf("sum = %d, want 20", op.Sum)
	}
	var shares int64
	for _, r := range op.Recipients {
		if r.SumMinor == nil {
			t.Fatal("у доли нет минорного значения")
		}
		shares += *r.SumMinor
	}
	if shares != 2000 {
		t.Errorf("сумма долей = %d, want 2000", shares)
	}
}

// Расхождение полей — отказ, а не молчаливый выбор одного из них: расходятся
// они только по ошибке клиента, и угадывать за него нельзя.
func TestCreateOperationRejectsMismatchedFields(t *testing.T) {
	room := scaleRoom("USD")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	body := `{"description":"Ужин","sum":20,"sumMinor":2080,"donorId":1,"recipientIds":[1,2]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Признак дробного ввода — свойство сервера. Выключен: дробное значение не
// принимается ни на одном входе.
func TestFractionalRejectedWhileFlagOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{
			"расход", "/operations",
			`{"description":"Ужин","sumMinor":2080,"donorId":1,"recipientIds":[1,2]}`,
		},
		{
			"доли получателей", "/operations",
			`{"description":"Ужин","sumMinor":2000,"donorId":1,"recipientSums":[{"userId":1,"sumMinor":1050},{"userId":2,"sumMinor":950}]}`,
		},
		{
			"погашение", "/repayments",
			`{"debtorId":2,"lenderId":1,"sumMinor":250}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			room := scaleRoom("USD")
			s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
			setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

			rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+tc.path,
				mustToken(t, s, testUser1.ID), tc.body)
			assertErrorCode(t, rec, http.StatusBadRequest, "validation")
		})
	}
}

// Признак включён — те же дроби принимаются, и доли сходятся с суммой точно.
func TestFractionalAcceptedWhileFlagOn(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), repo)
	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	body := `{"description":"Ужин","sumMinor":2080,"donorId":1,"recipientIds":[1,2]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}
	if op.SumMinor == nil || *op.SumMinor != 2080 {
		t.Fatalf("sumMinor = %v, want 2080", op.SumMinor)
	}
	// Проекция для прежних сборок: 20,80 показывается как 21, а не как 20
	if op.Sum != 21 {
		t.Errorf("проекция суммы = %d, want 21", op.Sum)
	}
	var shares int64
	for _, r := range op.Recipients {
		shares += *r.SumMinor
	}
	if shares != 2080 {
		t.Errorf("сумма долей = %d, want 2080 — деньги разошлись", shares)
	}
}

// Сверка долей идёт в минорных единицах: в целых 10,40 + 10,40 округлились бы
// до 10 + 10 и разошлись бы с суммой 20,80 на ровном месте.
func TestExactSharesCheckedInMinorUnits(t *testing.T) {
	room := scaleRoom("USD")
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	body := `{"description":"Ужин","sumMinor":2080,"donorId":1,` +
		`"recipientSums":[{"userId":1,"sumMinor":1040},{"userId":2,"sumMinor":1040}]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	// А несходящиеся доли по-прежнему отвергаются
	bad := `{"description":"Ужин","sumMinor":2080,"donorId":1,` +
		`"recipientSums":[{"userId":1,"sumMinor":1040},{"userId":2,"sumMinor":1000}]}`
	rec = doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), bad)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Прежняя сборка шлёт только старое поле — и продолжает работать как раньше.
func TestLegacyOnlyRequestStillWorks(t *testing.T) {
	room := scaleRoom("RUB")
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	body := `{"description":"Ужин","sum":100,"donorId":1,"recipientIds":[1,2]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}
	// Сервер САМ достраивает минорное поле: старый клиент читает ту же комнату
	// и обязан увидеть в ней число, а не пустоту
	if op.SumMinor == nil || *op.SumMinor != 100 {
		t.Errorf("sumMinor = %v, want 100 (шкала 0)", op.SumMinor)
	}
	if op.Sum != 100 {
		t.Errorf("sum = %d, want 100", op.Sum)
	}
}

// Старая сборка читает только целые поля. Итог и доли в ответе обязаны
// сходиться между собой: 20,80 с долями 10,40 + 10,40 давал раньше итог 21 и
// доли 10 + 10 — единица исчезала прямо в ответе.
func TestLegacyProjectionOfFractionalSharesSumsToTotal(t *testing.T) {
	room := scaleRoom("USD")
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), repo)
	setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

	body := `{"description":"Ужин","sumMinor":2080,"donorId":1,` +
		`"recipientSums":[{"userId":1,"sumMinor":1040},{"userId":2,"sumMinor":1040}]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var op operationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
	}

	var legacy int
	for _, r := range op.Recipients {
		legacy += r.Sum
	}
	if legacy != op.Sum {
		t.Errorf("старые доли дают %d, старый итог %d — единица исчезла в ответе", legacy, op.Sum)
	}
	if op.Sum != 21 {
		t.Errorf("итог = %d, want 21", op.Sum)
	}
}

// Данные бота: равное деление лежит как float64(total)/n. Ответ REST обязан
// отдавать доли, сходящиеся с итогом, на любой шкале.
func TestBotEqualSplitProjectionSumsToTotal(t *testing.T) {
	room := scaleRoom("RUB")
	ops := *room.Operations
	ops[0].Sum = 100
	ops[0].SplitType = api.SplitTypeEqually
	ops[0].RecipientsWithSum = []api.RecipientWithSum{
		{User: testUser1, Sum: 100.0 / 3}, {User: testUser2, Sum: 100.0 / 3}, {User: testUser1, Sum: 100.0 / 3},
	}
	room.Operations = &ops
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/rooms/"+room.ID.Hex(), mustToken(t, s, testUser1.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var detail roomDetailDto
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("cannot parse room %q: %v", rec.Body.String(), err)
	}
	op := detail.Operations[0]

	var legacy int
	var minor int64
	for _, r := range op.Recipients {
		legacy += r.Sum
		if r.SumMinor != nil {
			minor += *r.SumMinor
		}
	}
	if legacy != op.Sum {
		t.Errorf("старые доли дают %d, итог %d", legacy, op.Sum)
	}
	if op.SumMinor == nil || minor != *op.SumMinor {
		t.Errorf("минорные доли дают %d, минорный итог %v", minor, op.SumMinor)
	}
}

// Минорные поля позиций больше не игнорируются молча. Пока позиции считаются
// целыми единицами, минорное принимается только вместе со старым и только
// если они сходятся — иначе контракт выглядел бы рабочим, а точное значение
// терялось бы по дороге.
func TestItemMinorFieldsAreValidated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		item    string
		flagOn  bool
		wantErr bool
	}{
		{"оба поля сходятся", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, false},
		{"поля не сходятся", `{"name":"Кофе","price":100,"priceMinor":20000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true},
		{"минорное без старого", `{"name":"Кофе","priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true},
		{"дробная цена при выключенном признаке", `{"name":"Кофе","price":100,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true},
		{"дробная цена при включённом признаке — тоже отказ, позиции считаются целыми", `{"name":"Кофе","price":100,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, true},
		// Именно этот случай проходил раньше: проекция 10050 при шкале 2 равна
		// 101, пара «сходилась», а арифметика считала по 101 и расходилась с
		// сохранённым 10050 на полтинник.
		{"проекция сходится, а единицы разные", `{"name":"Кофе","price":101,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, true},
		{"целая дробь при включённом признаке проходит", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, false},
		{"дробная фикс-доля", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1,"amount":50,"amountMinor":5050}]}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			room := scaleRoom("USD")
			s := newTestServer(Config{FractionalInput: tc.flagOn},
				newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))
			setScale(t, s, room.ID.Hex(), `{"displayExponent":2}`)

			body := `{"description":"Чек","donorId":1,"items":[` + tc.item + `]}`
			rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
				mustToken(t, s, testUser1.ID), body)
			if tc.wantErr {
				assertErrorCode(t, rec, http.StatusBadRequest, "validation")
				return
			}
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
			}
			var op operationDto
			if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
				t.Fatalf("cannot parse operation %q: %v", rec.Body.String(), err)
			}
			if len(op.Items) == 0 {
				t.Fatal("позиции не вернулись")
			}
			// Минорное поле обязано доехать до документа и вернуться в ответе
			if op.Items[0].PriceMinor == nil || *op.Items[0].PriceMinor != 10000 {
				t.Errorf("priceMinor = %v, want 10000 — минорное поле потерялось", op.Items[0].PriceMinor)
			}
		})
	}
}
