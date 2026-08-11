package repository

import (
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Интеграционные тесты SetRoomSeenAt против ЖИВОГО mongo — по той же причине,
// что и у SetNotificationsSeenAt: вся суть метода в условиях ФИЛЬТРА (tombstone
// и «только вперёд») плюс во вложенном ключе rooms_seen_at.<roomId>, а фейк в
// rest-тестах повторяет то же правило кодом на Go.

func roomSeen(t *testing.T, repo *MongoUserRepository, id int, roomId string) *time.Time {
	t.Helper()
	user, err := repo.FindById(testCtx(t), id)
	if err != nil {
		t.Fatalf("не удалось прочитать пользователя: %v", err)
	}
	at, ok := user.RoomsSeenAt[roomId]
	if !ok {
		return nil
	}
	return &at
}

func TestSetRoomSeenAtMovesForward(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)
	room := primitive.NewObjectID().Hex()

	first := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, first); err != nil {
		t.Fatalf("первая отметка: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, room); got == nil || !got.Equal(first) {
		t.Fatalf("отметка не записалась: %v, want %v", got, first)
	}

	second := first.Add(30 * time.Minute)
	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, second); err != nil {
		t.Fatalf("вторая отметка: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, room); got == nil || !got.Equal(second) {
		t.Fatalf("отметка не сдвинулась вперёд: %v, want %v", got, second)
	}
}

// TestSetRoomSeenAtNeverGoesBackwards — запоздавший ретрай со старым временем
// вернул бы на карточку группы уже просмотренные события.
func TestSetRoomSeenAtNeverGoesBackwards(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)
	room := primitive.NewObjectID().Hex()

	current := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, current); err != nil {
		t.Fatalf("отметка: %v", err)
	}
	// Ошибки быть не должно: для клиента это идемпотентный повтор, а не сбой.
	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, current.Add(-time.Hour)); err != nil {
		t.Fatalf("запоздавшая отметка вернула ошибку: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, room); got == nil || !got.Equal(current) {
		t.Fatalf("отметку откатили назад: %v, want %v", got, current)
	}
}

// TestSetRoomSeenAtIsPerRoom — отметки комнат независимы, и общую
// notifications_seen_at запись по комнате не трогает.
func TestSetRoomSeenAtIsPerRoom(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)
	first, second := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()

	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.SetRoomSeenAt(ctx, seenUserID, first, at); err != nil {
		t.Fatalf("отметка: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, second); got != nil {
		t.Fatalf("отметка села на соседнюю комнату: %v", got)
	}
	if got := seenAt(t, repo, seenUserID); got != nil {
		t.Fatalf("отметка по комнате сдвинула общую: %v", got)
	}
}

// TestSetRoomSeenAtSkipsTombstone — на удалённый аккаунт метод не пишет ничего
// и, главное, НЕ создаёт документ: фильтр без upsert.
func TestSetRoomSeenAtSkipsTombstone(t *testing.T) {
	deletedAt := time.Now().UTC()
	repo := seenRepo(t, api.User{
		ID: seenUserID, Username: "zagir", DisplayName: DeletedUserPlaceholder,
		DeletedAt: &deletedAt,
	})
	ctx := testCtx(t)
	room := primitive.NewObjectID().Hex()

	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, time.Now().UTC()); err != nil {
		t.Fatalf("отметка на tombstone вернула ошибку: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, room); got != nil {
		t.Fatalf("отметка записана на tombstone: %v", got)
	}
}

// TestSetRoomSeenAtRejectsNonHexRoom — точка в id увела бы $set на произвольный
// вложенный путь документа пользователя.
func TestSetRoomSeenAtRejectsNonHexRoom(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)

	if err := repo.SetRoomSeenAt(ctx, seenUserID, "display_name.x", time.Now().UTC()); err == nil {
		t.Fatal("невалидный id комнаты принят")
	}
	user, err := repo.FindById(ctx, seenUserID)
	if err != nil {
		t.Fatalf("не удалось прочитать пользователя: %v", err)
	}
	if user.DisplayName != "Загир" || len(user.RoomsSeenAt) != 0 {
		t.Fatalf("документ пострадал: %+v", user)
	}
}

// TestSoftDeleteUserDropsRoomsSeenAt — ключи rooms_seen_at это список комнат
// человека, и переживать удаление аккаунта такой след не должен.
func TestSoftDeleteUserDropsRoomsSeenAt(t *testing.T) {
	repo := seenRepo(t, api.User{ID: seenUserID, Username: "zagir", DisplayName: "Загир"})
	ctx := testCtx(t)
	room := primitive.NewObjectID().Hex()

	if err := repo.SetRoomSeenAt(ctx, seenUserID, room, time.Now().UTC()); err != nil {
		t.Fatalf("отметка: %v", err)
	}
	if err := repo.SoftDeleteUser(ctx, seenUserID); err != nil {
		t.Fatalf("удаление аккаунта: %v", err)
	}
	if got := roomSeen(t, repo, seenUserID, room); got != nil {
		t.Fatalf("отметка пережила удаление аккаунта: %v", got)
	}
}
