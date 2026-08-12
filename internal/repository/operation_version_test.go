package repository

import (
	"errors"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Одновременная правка одного расхода.
//
// Правка записывала операцию целиком и безусловно. Двое открыли один расход,
// первый исправил сумму, второй — описание: побеждала последняя запись, а
// исправление первого исчезало молча. Узнать об этом было неоткуда — обоим
// показывали успех.
//
// Теперь у операции есть версия. Правка требует ту версию, которую редактор
// видел, когда открывал расход; разошлось — ErrStaleOperation, и человеку
// говорят пересобрать правку по свежим данным.

// storedOperation читает операцию прямо из базы.
func storedOperation(t *testing.T, db *mongo.Database, roomID string, opID primitive.ObjectID) api.Operation {
	t.Helper()
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	var room api.Room
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&room); err != nil {
		t.Fatalf("не удалось прочитать комнату: %v", err)
	}
	if room.Operations == nil {
		t.Fatal("в комнате нет операций")
	}
	for _, op := range *room.Operations {
		if op.ID == opID {
			return op
		}
	}
	t.Fatal("операция пропала из комнаты")
	return api.Operation{}
}

// TestUpdateOperationRejectsStaleVersion — головной случай: двое открыли один
// расход, первый уже сохранил. Второй пишет по версии, которой больше нет.
func TestUpdateOperationRejectsStaleVersion(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	other := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, other)

	op := draftOperation(donor, donor, other)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)

	// оба прочитали одну и ту же версию
	first := storedOperation(t, db, roomID, op.ID)
	second := first

	first.Description = "Ужин, а не обед"
	if err := repo.UpdateOperationIfUnchanged(testCtx(t), &first, roomID); err != nil {
		t.Fatalf("первая правка не прошла: %v", err)
	}

	second.Sum = 500
	err := repo.UpdateOperationIfUnchanged(testCtx(t), &second, roomID)
	if !errors.Is(err, ErrStaleOperation) {
		t.Fatalf("вторая правка вернула %v — чужое исправление затёрто молча", err)
	}
	if got := storedOperation(t, db, roomID, op.ID); got.Description != "Ужин, а не обед" || got.Sum == 500 {
		t.Fatalf("в базе %q на сумму %d — правка проигравшего всё-таки записалась", got.Description, got.Sum)
	}
}

// TestUpdateOperationGrowsVersion — версия обязана расти на каждой записи,
// иначе следующая правка не отличила бы «никто не писал» от «писали».
func TestUpdateOperationGrowsVersion(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)

	current := storedOperation(t, db, roomID, op.ID)
	for i := 1; i <= 3; i++ {
		current.Description = "правка"
		if err := repo.UpdateOperationIfUnchanged(testCtx(t), &current, roomID); err != nil {
			t.Fatalf("правка %d не прошла: %v", i, err)
		}
		if current.Version != i {
			t.Fatalf("после правки %d версия у вызывающего %d — клиент получит устаревшую и следующая правка упрётся в конфликт", i, current.Version)
		}
		if got := storedOperation(t, db, roomID, op.ID).Version; got != i {
			t.Fatalf("после правки %d версия в базе %d", i, got)
		}
	}
}

// TestUpdateOperationLegacyWithoutVersion — у операций эпохи master-2021 поля
// version нет вовсе. Ноль обязан находить такую запись (mongo сравнивает
// отсутствующее поле только с null), иначе старый расход стал бы неправимым.
func TestUpdateOperationLegacyWithoutVersion(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if _, err := db.Collection("room").UpdateOne(testCtx(t),
		bson.M{"_id": hex, "operations._id": op.ID},
		bson.M{"$unset": bson.M{"operations.$.version": ""}}); err != nil {
		t.Fatalf("не удалось убрать версию: %v", err)
	}

	legacy := storedOperation(t, db, roomID, op.ID)
	if legacy.Version != 0 {
		t.Fatalf("подготовка не удалась: версия %d", legacy.Version)
	}
	legacy.Description = "правка легаси"
	if err := repo.UpdateOperationIfUnchanged(testCtx(t), &legacy, roomID); err != nil {
		t.Fatalf("легаси-операция без версии стала неправимой: %v", err)
	}
	if got := storedOperation(t, db, roomID, op.ID); got.Version != 1 || got.Description != "правка легаси" {
		t.Fatalf("после правки легаси: версия %d, описание %q", got.Version, got.Description)
	}
}

// TestUpdateOperationUnconditionalStillWorks — путь бота и старых сборок:
// версию они не присылают, и требовать её нельзя — у людей на руках приложения,
// которые про неё не знают. Записывать такие правки надо, но версию растить,
// иначе конкурентная правка из приложения ничего бы не заметила.
func TestUpdateOperationUnconditionalStillWorks(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)

	// кто-то уже правил расход, версия ушла вперёд
	first := storedOperation(t, db, roomID, op.ID)
	first.Description = "из приложения"
	if err := repo.UpdateOperationIfUnchanged(testCtx(t), &first, roomID); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	// бот читает операцию непосредственно перед записью и правит безусловно
	fromBot := storedOperation(t, db, roomID, op.ID)
	fromBot.Description = "из бота"
	if err := repo.UpdateOperation(testCtx(t), &fromBot, roomID); err != nil {
		t.Fatalf("правка через путь бота отвалилась: %v", err)
	}
	got := storedOperation(t, db, roomID, op.ID)
	if got.Description != "из бота" {
		t.Fatalf("описание %q — правка бота не записалась", got.Description)
	}
	if got.Version != 2 {
		t.Fatalf("версия %d — безусловная правка её не растит, и конкурент её не заметит", got.Version)
	}
}

// TestUpdateOperationMissingIsNotFound — «операции нет» и «её изменили» это
// разные ответы человеку, поэтому и ошибки разные.
func TestUpdateOperationMissingIsNotFound(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	ghost := draftOperation(donor, donor)
	ghost.Status = api.StatusActive
	err := repo.UpdateOperationIfUnchanged(testCtx(t), &ghost, roomID)
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("правка несуществующей операции вернула %v, ожидался ErrNoDocuments", err)
	}
}
