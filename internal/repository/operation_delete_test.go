package repository

import (
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Мягкое удаление расхода.
//
// Раньше удаление вырезало операцию из документа комнаты через $pull: вернуть
// её было нечем и неоткуда. Удалить расход при этом может ЛЮБОЙ участник, а не
// только автор, — то есть чужая ошибка стоила данных навсегда.
//
// Теперь удаление ставит статус archive. Из всех чтений операция исчезает
// (api.ActiveOperations), но остаётся в документе, и восстановить её — вопрос
// одного $set.

// roomOperationCount — сколько операций физически лежит в документе комнаты.
func roomOperationCount(t *testing.T, db *mongo.Database, roomID string) int {
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
		return 0
	}
	return len(*room.Operations)
}

// TestDeleteOperationArchivesInsteadOfRemoving — головной случай: после
// удаления расход не виден в активных, но из документа не исчез.
func TestDeleteOperationArchivesInsteadOfRemoving(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	other := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, other)

	op := draftOperation(donor, donor, other)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)

	deleted, err := repo.DeleteOperation(testCtx(t), roomID, op.ID)
	if err != nil {
		t.Fatalf("удаление не прошло: %v", err)
	}
	if !deleted {
		t.Fatal("удаление сообщило, что операции не было")
	}

	if st := roomOperationStatus(t, db, roomID, op.ID); st != api.StatusArchive {
		t.Fatalf("статус операции %q, ожидался archive", st)
	}
	if n := roomOperationCount(t, db, roomID); n != 1 {
		t.Fatalf("операций в документе %d — расход вырезали, восстанавливать нечего", n)
	}

	room, err := repo.FindById(testCtx(t), roomID)
	if err != nil {
		t.Fatalf("комната не читается: %v", err)
	}
	if ops := api.ActiveOperations(room); len(ops) != 0 {
		t.Fatalf("удалённый расход остался в активных: %d", len(ops))
	}
}

// TestDeleteOperationTwiceReportsNothingToDelete — повторное удаление ничего не
// архивирует. Ответ важен: на нём REST отличает 404 от молчаливого успеха,
// когда операцию удалили конкурентно между чтением комнаты и записью.
func TestDeleteOperationTwiceReportsNothingToDelete(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	addOperation(t, db, roomID, op)

	if deleted, err := repo.DeleteOperation(testCtx(t), roomID, op.ID); err != nil || !deleted {
		t.Fatalf("первое удаление: deleted=%v err=%v", deleted, err)
	}
	deleted, err := repo.DeleteOperation(testCtx(t), roomID, op.ID)
	if err != nil {
		t.Fatalf("повторное удаление вернуло ошибку: %v", err)
	}
	if deleted {
		t.Fatal("повторное удаление отчиталось об успехе — REST ответит 204 на несуществующую операцию")
	}
}

// TestDeleteOperationMissingOperation — операции с таким id в комнате нет.
func TestDeleteOperationMissingOperation(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	deleted, err := repo.DeleteOperation(testCtx(t), roomID, primitive.NewObjectID())
	if err != nil {
		t.Fatalf("удаление несуществующей операции вернуло ошибку: %v", err)
	}
	if deleted {
		t.Fatal("удаление несуществующей операции отчиталось об успехе")
	}
}

// TestDeleteOperationLegacyWithoutStatus — у операций эпохи master-2021 поля
// status нет вовсе. Условие «не архивная» обязано их пропускать, иначе старый
// расход стало бы нельзя удалить.
func TestDeleteOperationLegacyWithoutStatus(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	addOperation(t, db, roomID, op)
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if _, err := db.Collection("room").UpdateOne(testCtx(t),
		bson.M{"_id": hex, "operations._id": op.ID},
		bson.M{"$unset": bson.M{"operations.$.status": ""}}); err != nil {
		t.Fatalf("не удалось убрать статус: %v", err)
	}

	deleted, err := repo.DeleteOperation(testCtx(t), roomID, op.ID)
	if err != nil {
		t.Fatalf("удаление легаси-операции вернуло ошибку: %v", err)
	}
	if !deleted {
		t.Fatal("легаси-операцию без статуса удалить не удалось")
	}
	if st := roomOperationStatus(t, db, roomID, op.ID); st != api.StatusArchive {
		t.Fatalf("статус легаси-операции %q, ожидался archive", st)
	}
}

// TestPurgeOperationRemovesRecord — откат вставки, которой не должно было быть
// (компенсация переплаты в погашении): следа не остаётся.
func TestPurgeOperationRemovesRecord(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	roomID := seedLeaveRoom(t, db, donor)

	op := draftOperation(donor, donor)
	op.Status = api.StatusActive
	op.IsDebtRepayment = true
	addOperation(t, db, roomID, op)

	if err := repo.PurgeOperation(testCtx(t), roomID, op.ID); err != nil {
		t.Fatalf("откат не прошёл: %v", err)
	}
	if n := roomOperationCount(t, db, roomID); n != 0 {
		t.Fatalf("операций в документе %d — откатившееся погашение осталось в истории", n)
	}
}
