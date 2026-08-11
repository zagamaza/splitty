package repository

import (
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Гонка «выход из комнаты × запись расхода».
//
// Правило «пока на человеке висят расходы, убрать его нельзя» проверяется в
// rest/боте на прочитанном снимке комнаты, а создание расхода читает комнату
// СВОИМ запросом и с членством в момент записи не связано. Между проверкой и
// $pull помещается целый чужой запрос — и человек уходил с непогашенным долгом
// в комнату, которую больше не видит. Закрывается это только условием в
// фильтре: users[] и operations[] лежат в одном документе, и mongo применяет
// условие вместе с обновлением.

// seedLeaveRoom вставляет комнату с ПУСТЫМ массивом операций: seedRoom оставляет
// поле null, а $push в null падает — конкурентный CreateOperation в бою пишет в
// массив, созданный при первой операции комнаты.
func seedLeaveRoom(t *testing.T, db *mongo.Database, members ...api.User) string {
	t.Helper()
	room := api.Room{
		Name:       "Тестовая комната",
		Members:    &members,
		Operations: &[]api.Operation{},
		CreateAt:   time.Now().UTC(),
		Currency:   "RUB",
	}
	res, err := db.Collection("room").InsertOne(testCtx(t), room)
	if err != nil {
		t.Fatalf("не удалось засеять комнату: %v", err)
	}
	return res.InsertedID.(primitive.ObjectID).Hex()
}

// addOperation дописывает операцию в комнату напрямую — так же, как это делает
// конкурентный CreateOperation.
func addOperation(t *testing.T, db *mongo.Database, roomID string, op api.Operation) {
	t.Helper()
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if _, err = db.Collection("room").UpdateOne(testCtx(t), bson.M{"_id": hex},
		bson.M{"$push": bson.M{"operations": op}}); err != nil {
		t.Fatalf("не удалось записать операцию: %v", err)
	}
}

func activeOperation(donor api.User, recipients ...api.User) api.Operation {
	op := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor: &donor, Status: api.StatusActive, CreateAt: time.Now().UTC(),
	}
	for _, r := range recipients {
		op.RecipientsWithSum = append(op.RecipientsWithSum,
			api.RecipientWithSum{User: r, Sum: float64(100 / len(recipients))})
	}
	return op
}

// TestLeaveRoomRefusesWhenOperationLandsFirst — расход, легший ПОСЛЕ проверки в
// хендлере, обязан отменить выход: иначе долг остаётся на не-участнике, и
// убрать себя из расхода он уже не может — комнаты он не видит.
func TestLeaveRoomRefusesWhenOperationLandsFirst(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, owner, leaver)

	// конкурентный POST /operations, успевший раньше нашего $pull
	addOperation(t, db, roomID, activeOperation(owner, leaver))

	left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
	if err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if left {
		t.Fatal("выход состоялся при активном расходе — долг повис на не-участнике")
	}
	if ids := roomMemberIDs(t, db, roomID); len(ids) != 2 {
		t.Fatalf("состав комнаты изменился: %v", ids)
	}
}

// TestLeaveRoomConcurrentWithOperation — та же гонка, но настоящая: выход и
// создание расхода идут одновременно. Допустимых исходов ровно два, и оба
// согласованы — «вышел без расхода» или «остался с расходом». Недопустим
// третий: расход есть, а человека в комнате нет. Закрыт он с ДВУХ сторон, и
// каждая закрывает свою половину: расход лёг раньше — его видит фильтр
// LeaveRoom, выход прошёл раньше — вставку отменяет условие на состав.
func TestLeaveRoomConcurrentWithOperation(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}

	for i := 0; i < 20; i++ {
		roomID := seedLeaveRoom(t, db, owner, leaver)
		var (
			wg        sync.WaitGroup
			left      bool
			leaveErr  error
			createErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			left, leaveErr = repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
		}()
		go func() {
			defer wg.Done()
			op := activeOperation(owner, leaver)
			createErr = repo.CreateOperation(testCtx(t), &op, roomID)
		}()
		wg.Wait()

		if leaveErr != nil {
			t.Fatalf("LeaveRoom: %v", leaveErr)
		}
		if createErr != nil && createErr != ErrParticipantLeft {
			t.Fatalf("CreateOperation: %v", createErr)
		}
		if !left {
			continue // расход успел раньше — выход отменён, это согласованный исход
		}
		var room api.Room
		hex, _ := primitive.ObjectIDFromHex(roomID)
		if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&room); err != nil {
			t.Fatalf("не удалось прочитать комнату: %v", err)
		}
		if api.HasOperations(&room, leaver.ID) {
			t.Fatalf("попытка %d: человек вышел, но расход на нём остался — долг у не-участника", i)
		}
	}
}

// TestCreateOperationRefusesAfterParticipantLeft — вторая половина той же гонки:
// человек вышел, пока запрос на расход шёл. Комната прочитана хендлером раньше,
// членство по ней сходится, но записывать расход на не-участника нельзя: он
// комнату больше не видит и убрать себя из расхода не может.
func TestCreateOperationRefusesAfterParticipantLeft(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	gone := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, owner)

	op := activeOperation(owner, gone)
	if err := repo.CreateOperation(testCtx(t), &op, roomID); err != ErrParticipantLeft {
		t.Fatalf("ожидался отказ ErrParticipantLeft, получено %v", err)
	}

	idem := activeOperation(owner, gone)
	idem.ClientOpId = "outbox-1"
	created, err := repo.CreateOperationIfAbsent(testCtx(t), &idem, roomID)
	if created || err != ErrParticipantLeft {
		t.Fatalf("идемпотентная вставка: created=%v err=%v", created, err)
	}
}

// TestCreateOperationIfAbsentStaysIdempotentAfterLeave — повтор из outbox
// приходит и после чужого выхода: у него уже есть своя запись в комнате, и
// отвечать на него отказом нельзя — клиент повторял бы вечно.
func TestCreateOperationIfAbsentStaysIdempotentAfterLeave(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, owner, leaver)

	op := activeOperation(owner, leaver)
	op.ClientOpId = "outbox-1"
	created, err := repo.CreateOperationIfAbsent(testCtx(t), &op, roomID)
	if !created || err != nil {
		t.Fatalf("первая вставка: created=%v err=%v", created, err)
	}
	// участник вышел уже после расхода (в бою так не бывает — его держит
	// проверка, — но повтор обязан оставаться идемпотентным при любом составе)
	hex, _ := primitive.ObjectIDFromHex(roomID)
	if _, err = db.Collection("room").UpdateOne(testCtx(t), bson.M{"_id": hex},
		bson.M{"$pull": bson.M{"users": bson.M{"_id": leaver.ID}}}); err != nil {
		t.Fatalf("не удалось убрать участника: %v", err)
	}

	created, err = repo.CreateOperationIfAbsent(testCtx(t), &op, roomID)
	if created || err != nil {
		t.Fatalf("повтор из outbox: created=%v err=%v (ожидался идемпотентный отказ без ошибки)", created, err)
	}
}

// TestLeaveRoomIgnoresOperationsOfOthers — фильтр держит только «своего»:
// расход соседей выход не отменяет.
func TestLeaveRoomIgnoresOperationsOfOthers(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	third := api.User{ID: 3, DisplayName: "Третий"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}
	roomID := seedLeaveRoom(t, db, owner, leaver, third)
	addOperation(t, db, roomID, activeOperation(owner, third))

	left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
	if err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if !left {
		t.Fatal("чужой расход не выпустил человека из комнаты")
	}
}

// TestLeaveRoomIgnoresDraftAndArchivedOperations — фильтр обязан быть УЖЕ
// правила api.HasOperations, иначе он запрёт того, кого проверка в памяти
// выпускает: активная операция без долей понижается до драфта
// (NormalizedOperation), архивные версии в долгах не участвуют, а легаси эпохи
// master-2021 (без status) на момент чтения уже существует и отсекается в
// хендлере — в гонке её взяться неоткуда.
func TestLeaveRoomIgnoresDraftAndArchivedOperations(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	owner := api.User{ID: 1, DisplayName: "Хозяин"}
	leaver := api.User{ID: 2, DisplayName: "Гость"}

	archived := activeOperation(owner, leaver)
	archived.Status = api.StatusArchive
	draft := api.Operation{
		ID: primitive.NewObjectID(), Description: "Черновик", Sum: 100,
		Donor: &leaver, Status: api.StatusActive, CreateAt: time.Now().UTC(),
	}
	legacy := api.Operation{
		ID: primitive.NewObjectID(), Description: "Легаси", Sum: 100,
		Donor: &owner, Recipients: &[]api.User{owner, leaver}, CreateAt: time.Now().UTC(),
	}

	for _, c := range []struct {
		name string
		op   api.Operation
	}{
		{"архивная версия", archived},
		{"брошенный драфт донора", draft},
		{"легаси без status", legacy},
	} {
		t.Run(c.name, func(t *testing.T) {
			roomID := seedLeaveRoom(t, db, owner, leaver)
			addOperation(t, db, roomID, c.op)
			left, err := repo.LeaveRoom(testCtx(t), leaver.ID, roomID)
			if err != nil {
				t.Fatalf("LeaveRoom: %v", err)
			}
			if !left {
				t.Fatal("фильтр запер человека там, где проверка в памяти его выпускает")
			}
		})
	}
}
