package repository

import (
	"context"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// seedSearchRoom кладёт комнату с заданными участниками и датами операций.
func seedSearchRoom(t *testing.T, repo *MongoRoomRepository, name string, createdAt time.Time, members int, opDates ...time.Time) string {
	t.Helper()

	users := make([]api.User, 0, members)
	for i := 0; i < members; i++ {
		users = append(users, api.User{ID: i + 1, DisplayName: "Участник"})
	}
	ops := make([]api.Operation, 0, len(opDates))
	for _, at := range opDates {
		donor := users[0]
		ops = append(ops, api.Operation{
			ID: primitive.NewObjectID(), Description: "Расход", Sum: 100,
			Donor: &donor, Status: api.StatusActive, CreateAt: at,
		})
	}

	res, err := repo.col.InsertOne(context.Background(), api.Room{
		Name: name, Members: &users, Operations: &ops, CreateAt: createdAt, Currency: "RUB",
	})
	if err != nil {
		t.Fatalf("не удалось засеять комнату %q: %v", name, err)
	}
	return res.InsertedID.(primitive.ObjectID).Hex()
}

func TestSearchRoomsByName(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSearchRoom(t, repo, "Стамбул 2026", now.Add(-72*time.Hour), 3, now.Add(-70*time.Hour), now.Add(-2*time.Hour))
	seedSearchRoom(t, repo, "Дача", now.Add(-48*time.Hour), 2)
	seedSearchRoom(t, repo, "стамбульский чай", now.Add(-24*time.Hour), 5)

	rooms, err := repo.SearchRooms(context.Background(), "стамбул", 10)
	if err != nil {
		t.Fatalf("поиск: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("нашлось %d комнат, ожидалось 2: %+v", len(rooms), rooms)
	}
	// Регистр не должен решать: человек ищет «стамбул», а группа названа
	// «Стамбул 2026»
	if rooms[0].Name != "стамбульский чай" || rooms[1].Name != "Стамбул 2026" {
		t.Errorf("порядок (новые сверху) нарушен: %q, %q", rooms[0].Name, rooms[1].Name)
	}

	istanbul := rooms[1]
	if istanbul.MemberCount != 3 || istanbul.OperationCount != 2 {
		t.Errorf("участников %d, операций %d — ожидалось 3 и 2", istanbul.MemberCount, istanbul.OperationCount)
	}
	if istanbul.LastOperationAt == nil || !istanbul.LastOperationAt.Equal(now.Add(-2*time.Hour)) {
		t.Errorf("последняя операция: %v", istanbul.LastOperationAt)
	}
	// Вес нужен, чтобы увидеть приближение к потолку mongo раньше, чем в него
	// упрётся запись
	if istanbul.SizeBytes <= 0 {
		t.Errorf("вес комнаты = %d", istanbul.SizeBytes)
	}
}

// Комната без операций не должна ни выпадать из выдачи, ни ломать подсчёт:
// поля operations в документе может не быть вовсе
func TestSearchRoomsHandlesEmptyRoom(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	// Комната без поля operations вовсе — так их кладёт создание группы
	id := seedRoom(t, db, api.User{ID: 1, DisplayName: "Загир"})

	rooms, err := repo.SearchRooms(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("поиск: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID.Hex() != id {
		t.Fatalf("выдача: %+v", rooms)
	}
	if rooms[0].OperationCount != 0 || rooms[0].MemberCount != 1 {
		t.Errorf("операций %d, участников %d", rooms[0].OperationCount, rooms[0].MemberCount)
	}
	if rooms[0].LastOperationAt != nil {
		t.Errorf("у комнаты без операций дата последней = %v", rooms[0].LastOperationAt)
	}
}

func TestSearchRoomsById(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	now := time.Now().UTC()
	seedSearchRoom(t, repo, "Дача", now.Add(-time.Hour), 2)
	target := seedSearchRoom(t, repo, "Стамбул", now, 2)

	rooms, err := repo.SearchRooms(context.Background(), target, 10)
	if err != nil {
		t.Fatalf("поиск: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID.Hex() != target {
		t.Fatalf("поиск по id вернул: %+v", rooms)
	}
}

// Имя комнаты пишет человек: «(» и «.*» из него не должны становиться
// синтаксисом регулярного выражения — иначе поиск то падает, то возвращает всё
func TestSearchRoomsEscapesRegex(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	now := time.Now().UTC()
	seedSearchRoom(t, repo, "Дача (лето)", now, 2)
	seedSearchRoom(t, repo, "Стамбул", now, 2)

	found, err := repo.SearchRooms(context.Background(), "(лето)", 10)
	if err != nil {
		t.Fatalf("поиск по скобкам упал: %v", err)
	}
	if len(found) != 1 || found[0].Name != "Дача (лето)" {
		t.Errorf("поиск по скобкам вернул: %+v", found)
	}

	all, err := repo.SearchRooms(context.Background(), ".*", 10)
	if err != nil {
		t.Fatalf("поиск: %v", err)
	}
	if len(all) != 0 {
		t.Errorf(".* сработала как шаблон и вернула %d комнат", len(all))
	}
}

// Лимит не отдаётся на откуп вызывающему: ответ собирается в памяти, и
// «покажи 100000» не должно означать «прочитай всю базу»
func TestSearchRoomsCapsLimit(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		seedSearchRoom(t, repo, "Комната", now.Add(time.Duration(i)*time.Minute), 1)
	}

	if rooms, err := repo.SearchRooms(context.Background(), "", 2); err != nil || len(rooms) != 2 {
		t.Fatalf("лимит 2 вернул %d комнат, err=%v", len(rooms), err)
	}
	if rooms, err := repo.SearchRooms(context.Background(), "", 0); err != nil || len(rooms) != 3 {
		t.Fatalf("лимит 0 (по умолчанию) вернул %d комнат, err=%v", len(rooms), err)
	}
	if rooms, err := repo.SearchRooms(context.Background(), "", 1_000_000); err != nil || len(rooms) != 3 {
		t.Fatalf("запредельный лимит вернул %d комнат, err=%v", len(rooms), err)
	}
}

func TestRoomSizeBytes(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	id := seedSearchRoom(t, repo, "Стамбул", time.Now().UTC(), 3, time.Now().UTC())

	size, err := repo.RoomSizeBytes(context.Background(), id)
	if err != nil {
		t.Fatalf("вес: %v", err)
	}
	if size <= 0 {
		t.Errorf("вес комнаты = %d", size)
	}
	if _, err := repo.RoomSizeBytes(context.Background(), "не-id"); err == nil {
		t.Error("кривой id принят молча")
	}
}
