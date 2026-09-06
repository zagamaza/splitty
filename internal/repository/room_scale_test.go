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

// Ревизия комнаты растёт при ЛЮБОЙ записи. Прежние сторожа — размер массива и
// максимальная версия операции — пропускали три случая разом, и каждый из них
// пересчёт шкалы затёр бы: он кладёт массив операций целиком.
func TestRoomRevisionGrowsOnEveryMutation(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	op := scaleTestOperation(20)
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, op))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	for _, tc := range []struct {
		name string
		do   func(t *testing.T)
	}{
		{"отметка о доставке пуша", func(t *testing.T) {
			if err := repo.SetNotificationSent(testCtx(t), id.Hex(), op.ID, []int{1}); err != nil {
				t.Fatalf("SetNotificationSent: %v", err)
			}
		}},
		{"архивирование расхода", func(t *testing.T) {
			if _, err := repo.DeleteOperation(testCtx(t), id.Hex(), op.ID); err != nil {
				t.Fatalf("DeleteOperation: %v", err)
			}
		}},
		{"затирание личности в снимках", func(t *testing.T) {
			if err := repo.AnonymizeUser(testCtx(t), 2, DeletedUserPlaceholder); err != nil {
				t.Fatalf("AnonymizeUser: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := readRoom(t, repo, id.Hex()).Revision
			tc.do(t)
			after := readRoom(t, repo, id.Hex()).Revision
			if after <= before {
				t.Errorf("ревизия не выросла: было %d, стало %d — пересчёт шкалы затёр бы эту запись",
					before, after)
			}
		})
	}
}

// Пересчёт отказывается, если комнату писали между чтением и записью — на том
// самом случае, который прежние сторожа пропускали: отметка о доставке пуша
// не меняет ни длину массива, ни версию операции.
func TestSetRoomScaleRefusesAfterNotificationMark(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	op := scaleTestOperation(20)
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, op))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	room := readRoom(t, repo, id.Hex())
	revision := room.Revision

	if err := repo.SetNotificationSent(testCtx(t), id.Hex(), op.ID, []int{1}); err != nil {
		t.Fatalf("SetNotificationSent: %v", err)
	}

	hex, _ := primitive.ObjectIDFromHex(id.Hex())
	res, err := db.Collection("room").UpdateOne(testCtx(t),
		bson.M{"_id": hex, "revision": versionFilter(revision)},
		bson.M{"$set": bson.M{"display_exponent": 2}})
	if err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if res.MatchedCount != 0 {
		t.Fatal("сторож по ревизии не сработал: пересчёт вернул бы отметку о доставке и пуш ушёл бы второй раз")
	}
}

// Живой путь: пересчёт под конкурентной записью отдаёт ErrRoomBusy, а не
// молча затирает.
func TestSetRoomScaleReturnsBusyOnConcurrentWrite(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	op := scaleTestOperation(20)
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0, op))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	// Вклиниваемся ровно между чтением комнаты и записью пересчёта.
	repo.beforeScaleWrite = func() {
		if err := repo.SetNotificationSent(testCtx(t), id.Hex(), op.ID, []int{1}); err != nil {
			t.Errorf("SetNotificationSent: %v", err)
		}
	}
	if _, err := repo.SetRoomScale(testCtx(t), id.Hex(), 2); !errors.Is(err, ErrRoomBusy) {
		t.Fatalf("SetRoomScale: %v, want ErrRoomBusy", err)
	}
	if api.RoomExponent(readRoom(t, repo, id.Hex())) != 0 {
		t.Error("шкала всё-таки поменялась при отказе")
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

// Обратное окно гонки: расход, посчитанный по прежней шкале, не должен лечь в
// комнату, шкалу которой успели поменять. Иначе 20 рублей превратятся в 20
// копеек — или наоборот, смотря куда двигали шкалу.
func TestCreateOperationRefusesStaleScale(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 0))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	// Вклиниваемся между чтением шкалы и записью операции: в это окно комната
	// переезжает с целых единиц на копейки.
	repo.beforeOperationWrite = func() {
		if _, err := repo.SetRoomScale(testCtx(t), id.Hex(), 2); err != nil {
			t.Errorf("SetRoomScale: %v", err)
		}
	}

	op := scaleTestOperation(20)
	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}

	room := readRoom(t, repo, id.Hex())
	if api.RoomExponent(room) != 2 {
		t.Fatalf("шкала комнаты = %d, want 2", api.RoomExponent(room))
	}
	stored := (*room.Operations)[0]
	if stored.SumMinor == nil {
		t.Fatal("у операции нет минорной суммы")
	}
	// 20 единиц валюты при шкале 2 — это 2000 минорных. Если бы запись
	// прошла по прежней шкале, здесь лежало бы 20, то есть 0,20.
	if *stored.SumMinor != 2000 {
		t.Errorf("sumMinor = %d, want 2000 — расход записан по устаревшей шкале", *stored.SumMinor)
	}
	if stored.Sum != 20 {
		t.Errorf("sum = %d, want 20", stored.Sum)
	}
}

// Смена валюты обязана поднимать ВЕРСИЮ ШКАЛЫ, когда она меняет шкалу. Иначе
// сторож у записей операций её не видит: расход, посчитанный по прежней шкале,
// ложится в комнату с новой и толкуется в сто раз иначе.
func TestUpdateCurrencyBumpsScaleVersion(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	// Пустая долларовая комната с копейками: переезд в иены допустим, но
	// шкалу приходится опустить.
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	before := readRoom(t, repo, id.Hex()).ScaleVersion

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "JPY"); err != nil {
		t.Fatalf("UpdateCurrency: %v", err)
	}

	room := readRoom(t, repo, id.Hex())
	if api.RoomExponent(room) != 0 {
		t.Fatalf("шкала = %d, want 0", api.RoomExponent(room))
	}
	if room.ScaleVersion <= before {
		t.Errorf("версия шкалы = %d, была %d — сторож у записей операций её не увидит",
			room.ScaleVersion, before)
	}
}

// Смена валюты, не трогающая шкалу, версию шкалы не двигает.
func TestUpdateCurrencyKeepsScaleVersionWhenScaleUnchanged(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2, scaleTestOperation(20)))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}
	before := readRoom(t, repo, id.Hex()).ScaleVersion

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "EUR"); err != nil {
		t.Fatalf("UpdateCurrency: %v", err)
	}
	if got := readRoom(t, repo, id.Hex()).ScaleVersion; got != before {
		t.Errorf("версия шкалы = %d, была %d — её никто не просил менять", got, before)
	}
}

// Пересчёт проверяет шкалу против ТЕКУЩЕЙ валюты комнаты. Запрос, законный для
// рубля, не должен поставить иеновой комнате шкалу, которой у иены нет.
func TestSetRoomScaleValidatesAgainstCurrentCurrency(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	room := scaleTestRoom(t, 0)
	room.Currency = "JPY"
	id, err := repo.SaveRoom(testCtx(t), room)
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	if _, err := repo.SetRoomScale(testCtx(t), id.Hex(), 2); !errors.Is(err, ErrScaleNotSupported) {
		t.Fatalf("SetRoomScale: %v, want ErrScaleNotSupported", err)
	}
	if api.RoomExponent(readRoom(t, repo, id.Hex())) != 0 {
		t.Error("шкала всё-таки поменялась")
	}
}

// Смена валюты пишет под CAS: если комнату успели изменить между чтением и
// записью, она повторяет попытку на свежем состоянии, а не переключает шкалу
// под уже записанными деньгами.
func TestUpdateCurrencyRefusesUnderConcurrentInsert(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)

	// Пустая долларовая комната с копейками: пока она пуста, переезд в иены
	// разрешён и опускает шкалу.
	id, err := repo.SaveRoom(testCtx(t), scaleTestRoom(t, 2))
	if err != nil {
		t.Fatalf("SaveRoom: %v", err)
	}

	// Расход приезжает ДО смены валюты — теперь комната уже не пуста.
	op := scaleTestOperation(20)
	if err := repo.CreateOperation(testCtx(t), &op, id.Hex()); err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}

	if err := repo.UpdateCurrency(testCtx(t), id.Hex(), "JPY"); !errors.Is(err, ErrScaleNotSupported) {
		t.Fatalf("UpdateCurrency: %v, want ErrScaleNotSupported", err)
	}
	room := readRoom(t, repo, id.Hex())
	if room.Currency != "USD" {
		t.Errorf("валюта = %q, want USD", room.Currency)
	}
	if api.RoomExponent(room) != 2 {
		t.Errorf("шкала = %d, want 2 — её опустили под записанными деньгами", api.RoomExponent(room))
	}
}
