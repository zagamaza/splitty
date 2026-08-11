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

// TestCreateOperationIgnoresStaleLegacyRecipients — вставка обязана требовать
// членства ровно у тех, кого расход СВЯЗЫВАЕТ, а не у всех, кто когда-либо
// попал в легаси-список recipients.
//
// Правка расхода в боте начинается с копии-черновика, и копия тащит за собой
// поле recipients целиком — бот его никогда не чистит. Когда у вставки был
// собственный, более широкий набор (плюс легаси-recipients), старый расход с
// давно вышедшим «получателем» невозможно было даже открыть на правку: черновик
// не заводился, и бот молча возвращался с пустым экраном.
func TestCreateOperationIgnoresStaleLegacyRecipients(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	member := api.User{ID: 2, DisplayName: "Сосед"}
	// в комнате его давно нет, но в legacy-recipients старого расхода он остался
	stale := api.User{ID: 3, DisplayName: "Бывший"}
	roomID := seedLeaveRoom(t, db, donor, member)

	legacy := []api.User{stale}
	draft := draftOperation(donor, donor, member)
	draft.Recipients = &legacy

	if err := repo.CreateOperation(testCtx(t), &draft, roomID); err != nil {
		t.Fatalf("черновик правки не завёлся из-за протухшего recipients: %v", err)
	}
	if got := roomOperationStatus(t, db, roomID, draft.ID); got != api.StatusDraft {
		t.Fatalf("статус записанного черновика %q", got)
	}
}

// TestCreateOperationStillRefusesWhenBoundParticipantLeft — обратная сторона:
// сузив набор до связываемых расходом людей, нельзя потерять саму защиту от
// гонки «выход × расход». Действующий расход на не-участника не записывается.
func TestCreateOperationStillRefusesWhenBoundParticipantLeft(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, leaver)

	left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
	if err != nil || !left {
		t.Fatalf("подготовка гонки не удалась: left=%v err=%v", left, err)
	}

	op := activeOperation(donor, donor, leaver)
	err = repo.CreateOperation(testCtx(t), &op, roomID)
	if !errors.Is(err, ErrParticipantLeft) {
		t.Fatalf("расход записан на не-участника: err=%v", err)
	}
}

// TestCreateOperationRefusesByLegacyRecipientsWhenSharesEmpty — легаси-список
// перестаёт быть «протухшим», когда долей нет вовсе: тогда доли синтезируются
// именно из него (api.NormalizedOperation), и такой расход людей ДЕРЖИТ.
func TestCreateOperationRefusesByLegacyRecipientsWhenSharesEmpty(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	donor := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, donor, leaver)

	left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
	if err != nil || !left {
		t.Fatalf("подготовка гонки не удалась: left=%v err=%v", left, err)
	}

	legacy := []api.User{donor, leaver}
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor: &donor, Status: api.StatusActive, CreateAt: time.Now().UTC(),
		Recipients: &legacy,
	}
	err = repo.CreateOperation(testCtx(t), &op, roomID)
	if !errors.Is(err, ErrParticipantLeft) {
		t.Fatalf("легаси-расход записан на не-участника: err=%v", err)
	}
}
