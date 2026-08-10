package repository

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// seedRoom вставляет комнату напрямую и возвращает её hex-id.
func seedRoom(t *testing.T, db *mongo.Database, members ...api.User) string {
	t.Helper()
	room := api.Room{
		Name:     "Тестовая комната",
		Members:  &members,
		CreateAt: time.Now().UTC(),
		Currency: "RUB",
	}
	res, err := db.Collection("room").InsertOne(testCtx(t), room)
	if err != nil {
		t.Fatalf("не удалось засеять комнату: %v", err)
	}
	return res.InsertedID.(primitive.ObjectID).Hex()
}

// roomMemberIDs читает id участников комнаты напрямую из базы.
func roomMemberIDs(t *testing.T, db *mongo.Database, roomID string) []int {
	t.Helper()
	var room api.Room
	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&room); err != nil {
		t.Fatalf("не удалось прочитать комнату: %v", err)
	}
	var ids []int
	if room.Members != nil {
		for _, m := range *room.Members {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// TestJoinToRoomConcurrentSingleEntry — ключевой тест задачи: приглашения дали
// второй путь записи в users (принял приглашение + параллельно прошёл по
// ссылке), и прежняя пара «hasUserInRoom → $push» клала два снимка одного
// человека. Дубль участника означает дубль в расчёте долгов, то есть тихо
// неверные деньги.
func TestJoinToRoomConcurrentSingleEntry(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	roomID := seedRoom(t, db, api.User{ID: 1, DisplayName: "Хозяин"})

	newcomer := api.User{ID: 100, Username: "kate", DisplayName: "Катя"}

	const attempts = 10
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.JoinToRoom(testCtx(t), newcomer, roomID); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("конкурентные JoinToRoom вернули ошибки: %v", errs)
	}

	ids := roomMemberIDs(t, db, roomID)
	var count int
	for _, id := range ids {
		if id == newcomer.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("после %d конкурентных добавлений участник встречается %d раз (ожидалось 1): %v",
			attempts, count, ids)
	}
	if len(ids) != 2 {
		t.Fatalf("в комнате должно быть 2 участника, получено %d: %v", len(ids), ids)
	}
}

// TestJoinToRoomIdempotent — последовательный повтор тоже не должен плодить
// записи и не должен возвращать ошибку: эндпоинт приглашения идемпотентен.
func TestJoinToRoomIdempotent(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	roomID := seedRoom(t, db, api.User{ID: 1, DisplayName: "Хозяин"})

	newcomer := api.User{ID: 100, DisplayName: "Катя"}
	for i := 0; i < 3; i++ {
		if err := repo.JoinToRoom(testCtx(t), newcomer, roomID); err != nil {
			t.Fatalf("повтор %d вернул ошибку: %v", i+1, err)
		}
	}

	ids := roomMemberIDs(t, db, roomID)
	if len(ids) != 2 {
		t.Fatalf("повторные вызовы изменили состав комнаты: %v", ids)
	}
}

// TestJoinToRoomSanitizesSnapshot — поля личности не должны попадать во
// встроенный снимок: их там никто не обновляет и не удаляет, и при удалении
// аккаунта они пережили бы сам аккаунт.
func TestJoinToRoomSanitizesSnapshot(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	roomID := seedRoom(t, db, api.User{ID: 1, DisplayName: "Хозяин"})

	tg := 555
	newcomer := api.User{
		ID: 100, DisplayName: "Катя", Username: "kate",
		TelegramID: &tg, GoogleSub: "google-sub", AppleSub: "apple-sub",
		Email: "kate@example.com", LoginEmail: "kate@example.com",
	}
	if err := repo.JoinToRoom(testCtx(t), newcomer, roomID); err != nil {
		t.Fatalf("JoinToRoom: %v", err)
	}

	hex, err := primitive.ObjectIDFromHex(roomID)
	if err != nil {
		t.Fatalf("плохой id комнаты: %v", err)
	}
	var raw bson.M
	if err := db.Collection("room").FindOne(testCtx(t), bson.M{"_id": hex}).Decode(&raw); err != nil {
		t.Fatalf("не удалось прочитать сырой документ комнаты: %v", err)
	}

	users, ok := raw["users"].(bson.A)
	if !ok {
		t.Fatalf("users не массив: %T", raw["users"])
	}

	// Снимок обязан быть НАЙДЕН: без этой проверки тест проходил бы вхолостую,
	// если бы id раскодировался в неожиданный тип и участник молча пропускался.
	var checked bool
	for _, u := range users {
		doc, ok := u.(bson.M)
		if !ok {
			t.Fatalf("участник не документ: %T", u)
		}
		id, err := bsonIntValue(doc["_id"])
		if err != nil {
			t.Fatalf("id участника не число: %v (%T)", err, doc["_id"])
		}
		if id != newcomer.ID {
			continue
		}
		checked = true
		for _, forbidden := range []string{"telegram_id", "google_sub", "apple_sub", "email", "login_email"} {
			if _, present := doc[forbidden]; present {
				t.Fatalf("поле личности %q утекло во встроенный снимок: %+v", forbidden, doc)
			}
		}
	}
	if !checked {
		t.Fatalf("снимок добавленного участника не найден в users: %+v", users)
	}
}

// bsonIntValue приводит числовое поле bson к int: драйвер отдаёт int32 или
// int64 в зависимости от того, как значение легло в документ.
func bsonIntValue(v interface{}) (int, error) {
	switch n := v.(type) {
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("неожиданный тип %T", v)
	}
}
