package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Гонка «выход из комнаты × активация черновика».
//
// Черновик бота живёт минутами: его создают на первом экране, а «Готово»
// нажимают потом. Всё это время он никого не держит в комнате — правило
// api.HasOperations смотрит только на активные операции, фильтр LeaveRoom тоже.
// Значит получатель успевает выйти между созданием черновика и его активацией,
// и без условия на состав долг ложится на того, кто комнату уже не видит и
// убрать себя из расхода не может.

// draftOperation — черновик бота: доли есть, статус draft.
func draftOperation(donor api.User, recipients ...api.User) api.Operation {
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor: &donor, Status: api.StatusDraft, CreateAt: time.Now().UTC(),
	}
	for _, r := range recipients {
		op.RecipientsWithSum = append(op.RecipientsWithSum,
			api.RecipientWithSum{User: r, Sum: float64(100 / len(recipients))})
	}
	return op
}

// roomOperationStatus читает статус операции прямо из базы.
func roomOperationStatus(t *testing.T, db *mongo.Database, roomID string, opID primitive.ObjectID) api.OperationStatus {
	t.Helper()
	var room api.Room
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&room); err != nil {
		t.Fatalf("не удалось прочитать комнату: %v", err)
	}
	if room.Operations == nil {
		t.Fatal("в комнате нет операций")
	}
	for _, op := range *room.Operations {
		if op.ID == opID {
			return op.Status
		}
	}
	t.Fatal("операция пропала из комнаты")
	return ""
}

// TestActivateOperationRefusesWhenRecipientLeft — головной случай: получателя
// черновика в комнате уже нет. Активировать такой расход нельзя, иначе долг
// повиснет на не-участнике.
func TestActivateOperationRefusesWhenRecipientLeft(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, leaver)

	draft := draftOperation(donor, donor, leaver)
	addOperation(t, db, roomID, draft)

	// человек вышел, пока черновик собирали: черновик его не держал
	left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
	if err != nil || !left {
		t.Fatalf("подготовка гонки не удалась: left=%v err=%v", left, err)
	}

	active := draft
	active.Status = api.StatusActive
	err = repo.ActivateOperation(testCtx(t), &active, roomID)
	if !errors.Is(err, ErrParticipantLeft) {
		t.Fatalf("активация прошла мимо состава комнаты: %v", err)
	}
	if st := roomOperationStatus(t, db, roomID, draft.ID); st != api.StatusDraft {
		t.Fatalf("операция стала %q — долг записан на не-участника", st)
	}
}

// TestActivateOperationAllowsWhenAllPresent — обратная сторона: пока состав на
// месте, активация обязана проходить, иначе бот перестал бы вносить расходы.
func TestActivateOperationAllowsWhenAllPresent(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	other := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, other)

	draft := draftOperation(donor, donor, other)
	addOperation(t, db, roomID, draft)

	active := draft
	active.Status = api.StatusActive
	if err := repo.ActivateOperation(testCtx(t), &active, roomID); err != nil {
		t.Fatalf("активация не прошла: %v", err)
	}
	if st := roomOperationStatus(t, db, roomID, draft.ID); st != api.StatusActive {
		t.Fatalf("операция осталась %q", st)
	}
}

// TestActivateOperationIgnoresStaleLegacyRecipients — бот копирует легаси-поле
// recipients при правке и никогда его не чистит, поэтому там остаются люди,
// которых расход давно не касается (см. bot.TestBotLeaveAllowsWhenLegacyRecipientsAreStale:
// из комнаты их такой список не держит). Требуя членства и по нему, условие
// запретило бы дописать правку старого расхода — то есть чинило бы гонку ценой
// неработающего редактирования.
func TestActivateOperationIgnoresStaleLegacyRecipients(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	stale := api.User{ID: 3, DisplayName: "Давно вышел"}
	roomID := seedLeaveRoom(t, db, donor)

	draft := draftOperation(donor, donor)
	draft.Recipients = &[]api.User{donor, stale}
	addOperation(t, db, roomID, draft)

	active := draft
	active.Status = api.StatusActive
	if err := repo.ActivateOperation(testCtx(t), &active, roomID); err != nil {
		t.Fatalf("протухший legacy-список запретил активацию: %v", err)
	}
	if st := roomOperationStatus(t, db, roomID, draft.ID); st != api.StatusActive {
		t.Fatalf("операция осталась %q", st)
	}
}

// TestActivateOperationMissingRoomIsNotFound — «комнаты нет» и «состав
// изменился» это разные ответы человеку, поэтому и ошибки разные.
func TestActivateOperationMissingRoomIsNotFound(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	op := draftOperation(donor, donor)
	op.Status = api.StatusActive

	err := repo.ActivateOperation(testCtx(t), &op, primitive.NewObjectID().Hex())
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("исчезнувшая комната отдана как %v", err)
	}
}
