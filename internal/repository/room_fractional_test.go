package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
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
