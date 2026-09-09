package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"time"
)

func TestResolveAmount(t *testing.T) {
	minor := func(v int64) *int64 { return &v }

	legacyPtr := func(v int) *int { return &v }

	for _, tc := range []struct {
		name          string
		legacy        *int
		minor         *int64
		allowFraction bool
		want          int64
		wantErr       bool
	}{
		{name: "только старое поле без копеек", legacy: legacyPtr(20), want: 2000},

		{name: "только минорное", minor: minor(2080), allowFraction: true, want: 2080},
		{name: "оба поля сходятся", legacy: legacyPtr(20), minor: minor(2000), want: 2000},
		{
			name:   "оба поля сходятся у дробной суммы: 21 — проекция 20.80",
			legacy: legacyPtr(21), minor: minor(2080), allowFraction: true, want: 2080,
		},
		{
			name:   "старое поле не сходится с проекцией минорного",
			legacy: legacyPtr(20), minor: minor(2080), allowFraction: true, wantErr: true,
		},
		{
			name:  "дробь при выключенном признаке отвергается",
			minor: minor(2080), allowFraction: false, wantErr: true,
		},
		{
			name:  "целое минорное при выключенном признаке проходит",
			minor: minor(2000), allowFraction: false, want: 2000,
		},
		{name: "нет ни одного поля", wantErr: true},
		{
			name:   "ноль в старом поле — это ПРИСЛАННЫЙ ноль, а не отсутствие",
			legacy: legacyPtr(0), minor: minor(100), allowFraction: true, wantErr: true,
		},
		{
			name:   "огромное старое поле не сворачивается в маленькое дробное",
			legacy: legacyPtr(184467440737095517), wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, hErr := resolveAmount("sum", tc.legacy, tc.minor, tc.allowFraction)
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
	// С копейками наименьшая сумма — половина единицы валюты: меньше даёт в
	// старом поле ноль, то есть расход без суммы.
	if got := minAmountMinor(true); got != 50 {
		t.Errorf("с копейками: got %d, want 50", got)
	}
	// Без копеек доли всё равно кратны единице валюты.
	if got := minAmountMinor(false); got != 100 {
		t.Errorf("без копеек: got %d, want 100", got)
	}
}

// Целый расход, присланный обоими полями, — основной путь прежних и новых
// сборок.
func TestCreateOperationAcceptsBothFields(t *testing.T) {
	room := fractionalRoom("USD", true)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), repo)

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
	room := fractionalRoom("USD", true)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

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
			room := fractionalRoom("USD", true)
			s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

			rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+tc.path,
				mustToken(t, s, testUser1.ID), tc.body)
			assertErrorCode(t, rec, http.StatusBadRequest, "validation")
		})
	}
}

// Признак включён — те же дроби принимаются, и доли сходятся с суммой точно.
func TestFractionalAcceptedWhileFlagOn(t *testing.T) {
	// Серверный рубильник ставится из конфига сервера (см. NewServer).
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", true)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), repo)

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
	// Серверный рубильник ставится из конфига сервера (см. NewServer).
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", true)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

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
	room := fractionalRoom("RUB", false)
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
	// Сервер САМ достраивает минорное поле: старый клиент читает ту же тусу и
	// обязан увидеть в ней число, а не пустоту. Хранение всегда в копейках,
	// поэтому 100 единиц валюты — это 10000.
	if op.SumMinor == nil || *op.SumMinor != 10000 {
		t.Errorf("sumMinor = %v, want 10000", op.SumMinor)
	}
	if op.Sum != 100 {
		t.Errorf("sum = %d, want 100", op.Sum)
	}
}

// Старая сборка читает только целые поля. Итог и доли в ответе обязаны
// сходиться между собой: 20,80 с долями 10,40 + 10,40 давал раньше итог 21 и
// доли 10 + 10 — единица исчезала прямо в ответе.
func TestLegacyProjectionOfFractionalSharesSumsToTotal(t *testing.T) {
	// Серверный рубильник ставится из конфига сервера (см. NewServer).
	defer api.SetFractionalInput(false)

	room := fractionalRoom("USD", true)
	repo := newFakeRoomRepo(room)
	s := newTestServer(Config{FractionalInput: true}, newFakeUserRepo(testUser1, testUser2), repo)

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
	room := fractionalRoom("RUB", false)
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
		name      string
		item      string
		flagOn    bool
		wantErr   bool
		wantMinor int64
	}{
		{"оба поля сходятся", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, false, 10000},
		{"поля не сходятся", `{"name":"Кофе","price":100,"priceMinor":20000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true, 0},
		{"минорное без старого", `{"name":"Кофе","priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true, 0},
		{"дробная цена при выключенном признаке", `{"name":"Кофе","price":101,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, false, true, 0},
		// Дробная позиция при поднятом признаке — теперь законна: чек в тусе с
		// копейками считается до копейки, а не округляется до единицы.
		{"дробная цена при включённом признаке", `{"name":"Кофе","price":101,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, false, 10050},
		// Целое обязано быть ПРОЕКЦИЕЙ минорного: 10050 округляется до 101, и
		// присланная сотня означала бы, что клиент считает не то, что записывает.
		{"целое не проекция минорного", `{"name":"Кофе","price":100,"priceMinor":10050,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, true, 0},
		{"целая цена при включённом признаке проходит", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1}]}`, true, false, 10000},
		{"дробная фикс-доля при выключенном признаке", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":1,"amount":51,"amountMinor":5050}]}`, false, true, 0},
		// Фикс-доля с копейками: остаток цены уходит второму участнику.
		{"дробная фикс-доля при включённом признаке", `{"name":"Кофе","price":100,"priceMinor":10000,"qty":1,"kind":"item","shares":[{"userId":1,"weight":0,"amount":51,"amountMinor":5050},{"userId":2,"weight":1}]}`, true, false, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			room := fractionalRoom("USD", true)
			s := newTestServer(Config{FractionalInput: tc.flagOn},
				newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

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
			if op.Items[0].PriceMinor == nil || *op.Items[0].PriceMinor != tc.wantMinor {
				t.Errorf("priceMinor = %v, want %d — минорное поле потерялось",
					op.Items[0].PriceMinor, tc.wantMinor)
			}
		})
	}
}

// Нулевая фиксированная доля законна: «этот человек за позицию не платит».
// Контракт разрешает её давно (отвергаются только отрицательные), и пара
// amount:0 + amountMinor:0 обязана проходить.
func TestZeroFixedShareIsAccepted(t *testing.T) {
	room := fractionalRoom("USD", true)
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(room))

	body := `{"description":"Чек","donorId":1,"items":[{"name":"Кофе","price":100,"priceMinor":10000,` +
		`"qty":1,"kind":"item","shares":[{"userId":1,"amount":0,"amountMinor":0},{"userId":2,"weight":1}]}]}`
	rec := doRequest(t, s, http.MethodPost, "/api/v1/rooms/"+room.ID.Hex()+"/operations",
		mustToken(t, s, testUser1.ID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
}

// Итог трат в приложении и в боте считается ОДИНАКОВО: точные величины
// складываются, округление одно в конце. Сумма округлений не равна округлению
// суммы — два расхода по 20,50 дают 41, а не 42.
func TestTotalsRoundOnceNotPerOperation(t *testing.T) {
	ops := []api.Operation{
		{Sum: 21, SumMinor: ptr64(2050), Status: statusActive},
		{Sum: 21, SumMinor: ptr64(2050), Status: statusActive},
	}

	if got := roomTotalSpent(ops); got != 41 {
		t.Errorf("итог = %d, want 41 — округлили каждый расход вместо суммы", got)
	}
	if got := roomTotalSpentMinor(ops); got != 4100 {
		t.Errorf("точный итог = %d, want 4100", got)
	}
}

// У суммы, не помещающейся в копейки, точного значения нет — итог обязан
// остаться честным, а не обнулиться.
func TestTotalsStayHonestWhenAmountDoesNotFitMinor(t *testing.T) {
	ops := []api.Operation{{Sum: 2_000_000_000, Status: statusActive}}

	if got := roomTotalSpent(ops); got != 2_000_000_000 {
		t.Errorf("итог = %d, want 2000000000 — траты комнаты пропали", got)
	}
}

// Статистика складывает ТОЧНЫЕ величины и округляет один раз.
//
// Два расхода по 20,50 — это 41, а не 42. По округлённым проекциям разъезжались
// бы и итог, и ряды графиков, и доли участников, и порядок в топе.
func TestStatisticsSumsExactAmounts(t *testing.T) {
	now := time.Now().UTC()
	donor := api.User{ID: testUser1.ID}
	other := api.User{ID: testUser2.ID}
	mk := func(minor int64) api.Operation {
		return api.Operation{
			ID: primitive.NewObjectID(), Sum: api.FromMinor(minor), SumMinor: ptr64(minor),
			Donor: &donor, Status: statusActive, CreateAt: now,
			RecipientsWithSum: []api.RecipientWithSum{
				{User: donor, SumMinor: ptr64(minor / 2)},
				{User: other, SumMinor: ptr64(minor - minor/2)},
			},
		}
	}
	spends := []api.Operation{mk(2050), mk(2050)}

	if got := roomTotalSpentMinor(spends); got != 4100 {
		t.Errorf("точный итог = %d, want 4100", got)
	}
	if got := monthSpentMinor(spends, now); got != 4100 {
		t.Errorf("итог месяца = %d, want 4100", got)
	}
	days := spentByDay(spends, now)
	if len(days) != 1 || days[0].SumMinor != 4100 || days[0].Sum != 41 {
		t.Errorf("ряд по дням = %+v, want один день 4100 копеек и 41 целыми", days)
	}
	paid := paidByMember(spends)
	if len(paid) != 1 || paid[0].SumMinor != 4100 {
		t.Errorf("заплатил = %+v, want 4100 копеек", paid)
	}
	share := shareByMember(spends)
	if len(share) != 2 {
		t.Fatalf("долей = %d, want 2", len(share))
	}
	var total int64
	for _, m := range share {
		total += m.SumMinor
	}
	if total != 4100 {
		t.Errorf("сумма долей = %d, want 4100", total)
	}
}
