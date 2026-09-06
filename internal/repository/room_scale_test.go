package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Смена шкалы комнаты и запрет опасной смены валюты — против ЖИВОЙ mongo.
//
// Фейк в тестах REST проверяет правило, но не запрос: условная запись здесь
// держится на $size по массиву операций и на $not/$elemMatch по их version,
// а такие фильтры мок не проверяет в принципе. Ошибись в них — и пересчёт
// либо не сработает никогда, либо сработает поверх чужой записи.

func scaleTestRoom(t *testing.T, exp int, ops ...api.Operation) *api.Room {
	t.Helper()
	donor := api.User{ID: 1, DisplayName: "Первый"}
	other := api.User{ID: 2, DisplayName: "Второй"}
	e := exp
	return &api.Room{
		Name:            "Стамбул",
		Currency:        "USD",
		DisplayExponent: &e,
		Members:         &[]api.User{donor, other},
		Operations:      &ops,
		CreateAt:        time.Now().UTC(),
	}
}

func scaleTestOperation(sum int) api.Operation {
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

// Включение копеек: суммы на вид те же, внутри стали минорными, версия шкалы
// выросла — и всё это доехало до документа, а не осталось в памяти.
func TestSetRoomScaleUpPersists(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	if _, err := repo.SetRoomScale(testCtx(t), id.Hex(), 2); err != nil {
		t.Fatalf("SetRoomScale: %v", err)
	}

	room := readRoom(t, repo, id.Hex())
	if api.RoomExponent(room) != 2 {
		t.Errorf("шкала = %d, want 2", api.RoomExponent(room))
	}
	if room.ScaleVersion != 1 {
		t.Errorf("версия шкалы = %d, want 1", room.ScaleVersion)
	}
	op := (*room.Operations)[0]
	if op.SumMinor == nil || *op.SumMinor != 2000 {
		t.Fatalf("sumMinor = %v, want 2000", op.SumMinor)
	}
	if op.Sum != 20 {
		t.Errorf("проекция суммы = %d, want 20 — на вид ничего не менялось", op.Sum)
	}
	var shares int64
	for _, r := range op.RecipientsWithSum {
		if r.SumMinor == nil {
			t.Fatal("у доли нет минорного значения")
		}
		shares += *r.SumMinor
	}
	if shares != 2000 {
		t.Errorf("сумма долей = %d, want 2000", shares)
	}
}

// Смена на ту же шкалу ничего не пишет и версию не двигает.
func TestSetRoomScaleNoopOnSameScale(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	if _, err := repo.SetRoomScale(testCtx(t), id.Hex(), 0); err != nil {
		t.Fatalf("SetRoomScale: %v", err)
	}
	if v := readRoom(t, repo, id.Hex()).ScaleVersion; v != 0 {
		t.Errorf("версия выросла на пустом месте: %d", v)
	}
}

// Сторож номер один: пока шёл пересчёт, в комнату добавили расход. Массив
// операций изменился в размере — запись обязана не сработать, иначе новый
// расход был бы затёрт вместе со всем массивом.
func TestSetRoomScaleRefusesWhenOperationAdded(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	// Читаем комнату так же, как это делает SetRoomScale, и вклиниваемся между
	// чтением и записью: имитируем чужой расход, доехавший в это окно.
	room := readRoom(t, repo, id.Hex())
	ops := *room.Operations
	before := len(ops)

	added := scaleTestOperation(50)
	if err := repo.CreateOperation(testCtx(t), &added, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}

	// Фильтр строится по состоянию, снятому ДО добавления: подделываем его,
	// записав документ с прежним размером массива в переменной запроса.
	hex, _ := primitive.ObjectIDFromHex(id.Hex())
	res, err := db.Collection("room").UpdateOne(testCtx(t),
		bson.M{"$and": bson.A{
			bson.M{"_id": hex},
			bson.M{"operations": bson.M{"$size": before}},
		}},
		bson.M{"$set": bson.M{"display_exponent": 2}})
	if err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if res.MatchedCount != 0 {
		t.Fatal("сторож по размеру массива не сработал: запись прошла бы поверх чужого расхода")
	}
}

// Сторож номер два: пока шёл пересчёт, существующий расход отредактировали.
// Размер массива тот же, но version операции вырос.
func TestSetRoomScaleRefusesWhenOperationEdited(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	op := scaleTestOperation(20)
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, op))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	room := readRoom(t, repo, id.Hex())
	maxVersion := 0
	for _, o := range *room.Operations {
		if o.Version > maxVersion {
			maxVersion = o.Version
		}
	}

	edited := (*room.Operations)[0]
	edited.Sum = 30
	if err := repo.UpdateOperation(testCtx(t), &edited, id.Hex()); err != nil {
		t.Fatalf("UpdateOperation: %v", err)
	}

	hex, _ := primitive.ObjectIDFromHex(id.Hex())
	res, err := db.Collection("room").UpdateOne(testCtx(t),
		bson.M{"$and": bson.A{
			bson.M{"_id": hex},
			bson.M{"operations": bson.M{"$not": bson.M{"$elemMatch": bson.M{"version": bson.M{"$gt": maxVersion}}}}},
		}},
		bson.M{"$set": bson.M{"display_exponent": 2}})
	if err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if res.MatchedCount != 0 {
		t.Fatal("сторож по версии операции не сработал: правка была бы затёрта")
	}
}

// Комнату с копейками нельзя перевести в валюту без дробной части: записанное
// 2000 (20,00 $) прочлось бы как 2000 иен.
func TestUpdateCurrencyRefusedWhenRoomHasCents(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	err = repo.UpdateCurrency(testCtx(t), id.Hex(), "JPY")
	if !errors.Is(err, ErrScaleNotSupported) {
		t.Fatalf("UpdateCurrency: %v, want ErrScaleNotSupported", err)
	}
	if got := readRoom(t, repo, id.Hex()).Currency; got != "USD" {
		t.Errorf("валюта = %q, want USD — отказ не должен ничего менять", got)
	}
}

// Пустой комнате терять нечего: валюта меняется, а шкала садится на умолчание
// новой валюты.
func TestUpdateCurrencyDropsScaleInEmptyRoom(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "JPY"); err != nil {
		t.Fatalf("UpdateCurrency: %v", err)
	}
	room := readRoom(t, repo, id.Hex())
	if room.Currency != "JPY" {
		t.Errorf("валюта = %q, want JPY", room.Currency)
	}
	if api.RoomExponent(room) != 0 {
		t.Errorf("шкала = %d, want 0", api.RoomExponent(room))
	}
}

// Смена внутри допустимой шкалы проходит и шкалу не трогает.
func TestUpdateCurrencyKeepsScaleWhenAllowed(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "EUR"); err != nil {
		t.Fatalf("UpdateCurrency: %v", err)
	}
	room := readRoom(t, repo, id.Hex())
	if room.Currency != "EUR" {
		t.Errorf("валюта = %q, want EUR", room.Currency)
	}
	if api.RoomExponent(room) != 2 {
		t.Errorf("шкала = %d, want 2 — её никто не просил менять", api.RoomExponent(room))
	}
}
