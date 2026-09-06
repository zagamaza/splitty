package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Копейки в тусе — против ЖИВОЙ mongo.
//
// Главное, что здесь проверяется: переключение признака НЕ трогает записанные
// суммы. В прежней схеме оно означало пересчёт всех денег комнаты, и половина
// найденных ревью дефектов жила именно там.

func fractionalTestRoom(t *testing.T, on bool, ops ...api.Operation) *api.Room {
	t.Helper()
	donor := api.User{ID: 1, DisplayName: "Первый"}
	other := api.User{ID: 2, DisplayName: "Второй"}
	v := on
	return &api.Room{
		Name:              "Стамбул",
		Currency:          "USD",
		FractionalAmounts: &v,
		Members:           &[]api.User{donor, other},
		Operations:        &ops,
		CreateAt:          time.Now().UTC(),
	}
}

func fractionalTestOperation(sum int) api.Operation {
	donor := api.User{ID: 1, DisplayName: "Первый"}
	other := api.User{ID: 2, DisplayName: "Второй"}
	return api.Operation{
		ID:          primitive.NewObjectID(),
		Description: "Ужин",
		Sum:         sum,
		Donor:       &donor,
		Status:      api.StatusActive,
		CreateAt:    time.Now().UTC(),
		RecipientsWithSum: []api.RecipientWithSum{
			{User: donor, Sum: float64(sum) / 2},
			{User: other, Sum: float64(sum) / 2},
		},
	}
}

func readRoom(t *testing.T, repo *MongoRoomRepository, id string) *api.Room {
	t.Helper()
	room, err := repo.FindById(testCtx(t), id)
	if err != nil {
		t.Fatalf("не удалось прочитать комнату: %v", err)
	}
	return room
}

// Деньги лежат в копейках независимо от настройки.
func TestStoredMoneyIsAlwaysKopecks(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	for _, on := range []bool{false, true} {
		id, err := repo.SaveRoom(testCtx(t), fractionalTestRoom(t, on))
		if err != nil {
			t.Fatalf("SaveRoom: %v", err)
		}
		op := fractionalTestOperation(20)
		if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
			t.Fatalf("CreateOperation: %v", err)
		}
		stored := (*readRoom(t, repo, id.Hex()).Operations)[0]
		if stored.SumMinor == nil || *stored.SumMinor != 2000 {
			t.Errorf("копейки=%v: sumMinor = %v, want 2000", on, stored.SumMinor)
		}
		if stored.Sum != 20 {
			t.Errorf("копейки=%v: sum = %d, want 20", on, stored.Sum)
		}
	}
}

// Переключение признака не меняет ни одной записанной суммы.
func TestSetRoomFractionalDoesNotTouchMoney(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), fractionalTestRoom(t, true))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	op := fractionalTestOperation(21)
	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	before := (*readRoom(t, repo, id.Hex()).Operations)[0]

	for _, on := range []bool{false, true, false} {
		if _, err := repo.SetRoomFractional(testCtx(t), id.Hex(), on); err != nil {
			t.Fatalf("SetRoomFractional(%v): %v", on, err)
		}
		after := (*readRoom(t, repo, id.Hex()).Operations)[0]
		if *after.SumMinor != *before.SumMinor {
			t.Fatalf("копейки=%v: сумма изменилась %d → %d",
				on, *before.SumMinor, *after.SumMinor)
		}
		if after.Sum != before.Sum {
			t.Fatalf("копейки=%v: старое поле изменилось %d → %d", on, before.Sum, after.Sum)
		}
	}
}

// Признак читается обратно из документа.
func TestSetRoomFractionalPersists(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), fractionalTestRoom(t, false))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	if _, err := repo.SetRoomFractional(testCtx(t), id.Hex(), true); err != nil {
		t.Fatalf("SetRoomFractional: %v", err)
	}
	if !api.RoomFractional(readRoom(t, repo, id.Hex())) {
		t.Error("признак не сохранился")
	}
}

// Переезд в валюту без дробной части гасит признак: новые суммы пойдут целыми,
// а уже записанные дробные остаются как есть.
func TestUpdateCurrencyTurnsFractionOffForCurrencyWithoutIt(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), fractionalTestRoom(t, true))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	op := fractionalTestOperation(21)
	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	before := *(*readRoom(t, repo, id.Hex()).Operations)[0].SumMinor

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "JPY"); err != nil {
		t.Fatalf("UpdateCurrency: %v", err)
	}
	room := readRoom(t, repo, id.Hex())
	if room.Currency != "JPY" {
		t.Errorf("валюта = %q, want JPY", room.Currency)
	}
	if api.RoomFractional(room) {
		t.Error("признак копеек остался включённым у валюты без дробной части")
	}
	if got := *(*room.Operations)[0].SumMinor; got != before {
		t.Errorf("сумма изменилась от смены валюты: %d, было %d", got, before)
	}
}

// Огромная сумма от бота не записывается и не превращается в ноль.
func TestCreateOperationRefusesHugeSum(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), fractionalTestRoom(t, false))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	op := fractionalTestOperation(20)
	op.Sum = 184467440737095517
	op.RecipientsWithSum = nil

	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err == nil {
		t.Fatal("огромная сумма записана")
	}
	if ops := *readRoom(t, repo, id.Hex()).Operations; len(ops) != 0 {
		t.Errorf("в комнате оказалась операция: %+v", ops)
	}
}

// Гонка: валюту сменили на иену, пока шёл запрос на включение копеек.
// Обработчик решает по комнате, прочитанной ДО вызова, поэтому валюту обязан
// сверить сам репозиторий — тем же запросом, что и пишет.
func TestSetRoomFractionalRefusesCurrencyWithoutFraction(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	room := fractionalTestRoom(t, false)
	room.Currency = "JPY"
	id, err := repo.SaveRoom(testCtx(t), room)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	if _, err := repo.SetRoomFractional(testCtx(t), id.Hex(), true); !errors.Is(err, ErrFractionNotSupported) {
		t.Fatalf("SetRoomFractional: %v, want ErrFractionNotSupported", err)
	}
	if api.RoomFractional(readRoom(t, repo, id.Hex())) {
		t.Error("копейки включились у валюты, где их нет")
	}
}

// Выключать можно в любой валюте: это не включение, запрещать нечего.
func TestSetRoomFractionalOffAlwaysAllowed(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	room := fractionalTestRoom(t, true)
	room.Currency = "JPY"
	id, err := repo.SaveRoom(testCtx(t), room)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	if _, err := repo.SetRoomFractional(testCtx(t), id.Hex(), false); err != nil {
		t.Fatalf("SetRoomFractional: %v", err)
	}
}

// Расход 100 на троих в тусе с копейками: доли, посчитанные обработчиком,
// доезжают до базы КАК ЕСТЬ. Прежде репозиторий выводил вектор заново по
// своему признаку, и ответ POST расходился с тем, что легло в mongo.
func TestCreateOperationKeepsHandlerShares(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	donor := api.User{ID: 1, DisplayName: "Первый"}
	other := api.User{ID: 2, DisplayName: "Второй"}
	third := api.User{ID: 3, DisplayName: "Третий"}

	room := fractionalTestRoom(t, false)
	room.Members = &[]api.User{donor, other, third}
	id, err := repo.SaveRoom(testCtx(t), room)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	// Доли посчитаны с шагом «до копейки», как это сделал бы обработчик при
	// включённом серверном признаке дробного ввода.
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		SumMinor: ptrInt64(10000), Donor: &donor,
		Status: api.StatusActive, CreateAt: time.Now().UTC(),
		SplitType: api.SplitTypeEqually,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: donor, Sum: 33.34, SumMinor: ptrInt64(3334)},
			{User: other, Sum: 33.33, SumMinor: ptrInt64(3333)},
			{User: third, Sum: 33.33, SumMinor: ptrInt64(3333)},
		},
	}
	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}

	stored := (*readRoom(t, repo, id.Hex()).Operations)[0]
	want := []int64{3334, 3333, 3333}
	for i, r := range stored.RecipientsWithSum {
		if r.SumMinor == nil || *r.SumMinor != want[i] {
			t.Errorf("доля[%d] = %v, want %d — репозиторий пересобрал вектор по-своему",
				i, r.SumMinor, want[i])
		}
	}
}

func ptrInt64(v int64) *int64 { return &v }

// ⚠️ Главный инвариант на ПРОДОВОЙ форме документа: минорных долей в базе нет
// вовсе, они выводятся при каждом чтении. Значит переключение тумблера не имеет
// права их сдвинуть.
func TestTogglingFractionKeepsSharesOnRawLegacyDocument(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	donor := api.User{ID: 1, DisplayName: "Первый"}
	other := api.User{ID: 2, DisplayName: "Второй"}
	third := api.User{ID: 3, DisplayName: "Третий"}
	// Форма бота: доли лежат как float64(total)/n, минорных полей нет.
	share := float64(100) / 3
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor: &donor, Status: api.StatusActive, CreateAt: time.Now().UTC(),
		SplitType: api.SplitTypeEqually,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: donor, Sum: share}, {User: other, Sum: share}, {User: third, Sum: share},
		},
	}
	room := fractionalTestRoom(t, true)
	room.Members = &[]api.User{donor, other, third}
	room.Operations = &[]api.Operation{op}
	id, err := repo.SaveRoom(testCtx(t), room)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	read := func() []int64 {
		t.Helper()
		out := []int64{}
		for _, r := range (*readRoom(t, repo, id.Hex()).Operations)[0].RecipientsWithSum {
			if r.SumMinor == nil {
				t.Fatal("у доли нет минорного значения")
			}
			out = append(out, *r.SumMinor)
		}
		return out
	}

	before := read()
	for _, on := range []bool{false, true, false} {
		if _, err := repo.SetRoomFractional(testCtx(t), id.Hex(), on); err != nil {
			t.Fatalf("SetRoomFractional(%v): %v", on, err)
		}
		if got := read(); !equalInt64(got, before) {
			t.Fatalf("копейки=%v: доли стали %v, были %v — долги поехали от тумблера",
				on, got, before)
		}
	}
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Старая туса без поля currency — исторический рубль, у которого копейки есть.
// Фильтр обязан её находить, иначе включение отвечает ложным отказом.
func TestSetRoomFractionalWorksForRoomWithoutCurrencyField(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	// Пишем документ СЫРЫМ bson, без поля currency вовсе — так лежат старые тусы.
	res, err := db.Collection("room").InsertOne(testCtx(t), bson.M{
		"name":       "Старая туса",
		"users":      []api.User{{ID: 1, DisplayName: "Первый"}},
		"operations": []api.Operation{},
		"create_at":  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("InsertOne: %v", err)
	}
	id := res.InsertedID.(primitive.ObjectID).Hex()

	if _, err := repo.SetRoomFractional(testCtx(t), id, true); err != nil {
		t.Fatalf("SetRoomFractional: %v — старая туса без валюты не нашлась фильтром", err)
	}
	if !api.RoomFractional(readRoom(t, repo, id)) {
		t.Error("признак не включился")
	}
}
